package worker

import (
	"axiom/code-audit-agent/internal/config"
	"axiom/code-audit-agent/internal/scanner"
	"axiom/code-audit-agent/pkg/execution"
	"axiom/contracts"
	"axiom/contracts/control"
	"context"
	"encoding/json"
	"errors"
	"log"
	"os"
	"sync"
	"sync/atomic"
	"time"
)

type Worker struct{ Cfg config.Config }

func (w *Worker) Run(ctx context.Context) error {
	if w.Cfg.AgentToken == "" {
		return errors.New("AXIOM_AGENT_TOKEN is required")
	}
	if w.Cfg.HealthFile != "" {
		if err := os.Remove(w.Cfg.HealthFile); err != nil && !os.IsNotExist(err) {
			return err
		}
	}
	count := w.Cfg.Concurrency
	if w.Cfg.Once {
		count = 1
	}
	if count < 1 {
		count = 1
	}
	client := execution.NewClient(w.Cfg.ServerURL, w.Cfg.AgentToken)
	capabilities, probeErr := scanner.ProbeContracts(ctx, w.Cfg.ScannerDir, w.Cfg.NodePath)
	if probeErr != nil {
		capabilities.Input = contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{}, JobVersions: []contracts.SchemaVersion{}}
		capabilities.Result = contracts.NoResultCapabilities()
		capabilities.Observation = contracts.NoObservationCapabilities()
	}
	client.InputCapabilities = &capabilities.Input
	client.ResultCapabilities = &capabilities.Result
	client.ObservationCapabilities = &capabilities.Observation
	if err := client.Negotiate(ctx, w.Cfg.AgentID, count); err != nil {
		return err
	}
	runtime := contracts.AgentRuntime{SchemaVersion: 1, AgentBuild: execution.AgentBuild, ControlAPI: int64(client.ControlAPI), State: contracts.AgentRuntimeStateReady, MatrixHash: control.MatrixHash}
	if capabilities.Build != "" {
		runtime.ScannerBuild = &capabilities.Build
	}
	if capabilities.DescriptionVersion > 0 && capabilities.DescriptionVersion <= 1000 {
		version := int64(capabilities.DescriptionVersion)
		runtime.ScannerDescriptionVersion = &version
	}
	if probeErr != nil {
		runtime.State = contracts.AgentRuntimeStateBlocked
		runtime.Diagnostic = &contracts.ContractError{Code: contracts.ErrorCodeProtocolVersionUnsupported, Origin: contracts.ErrorOriginAgent, Message: "Scanner 能力描述缺失、无效或版本不受支持；模型尚未启动。请升级配套 Scanner 后重启执行节点。", Retryable: false}
	}
	presenceCtx, stopPresence := context.WithCancel(ctx)
	defer stopPresence()
	report := func() {
		e := client.Presence(presenceCtx, w.Cfg.AgentID, count)
		if e == nil {
			e = client.ReportRuntime(presenceCtx, w.Cfg.AgentID, runtime)
		}
		if e != nil && presenceCtx.Err() == nil {
			log.Printf("agent presence: %v", e)
		} else if e == nil && w.Cfg.HealthFile != "" {
			if e = writeHealth(w.Cfg.HealthFile); e != nil {
				log.Printf("agent health: %v", e)
			}
		}
	}
	report()
	go func() {
		ticker := time.NewTicker(10 * time.Second)
		defer ticker.Stop()
		for {
			select {
			case <-presenceCtx.Done():
				return
			case <-ticker.C:
				report()
			}
		}
	}()
	if probeErr != nil {
		if w.Cfg.Once {
			return probeErr
		}
		log.Print(runtime.Diagnostic.Message)
		<-ctx.Done()
		return ctx.Err()
	}
	var wg sync.WaitGroup
	for i := 0; i < count; i++ {
		wg.Add(1)
		go func(slot int) { defer wg.Done(); w.loop(ctx, client, slot) }(i)
	}
	wg.Wait()
	return ctx.Err()
}
func (w *Worker) loop(ctx context.Context, c *execution.Client, slot int) {
	for ctx.Err() == nil {
		lease, e := c.Claim(ctx, w.Cfg.AgentID)
		if e != nil {
			log.Printf("slot=%d claim: %v", slot, e)
		} else if lease.HasTask() {
			log.Printf("task=%s attempt=%d slot=%d start", lease.Execution.TaskID, lease.Execution.Attempt, slot)
			if e = w.execute(ctx, c, lease); e != nil {
				log.Printf("task=%s attempt=%d: %v", lease.Execution.TaskID, lease.Execution.Attempt, e)
			}
		}
		if w.Cfg.Once {
			return
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(w.Cfg.PollInterval):
		}
	}
}
func (w *Worker) execute(ctx context.Context, c *execution.Client, lease *execution.Lease) error {
	task := lease
	ref := control.LegacyLeaseRef{TaskID: task.NumericTaskID(), Attempt: int(task.Execution.Attempt), AgentID: w.Cfg.AgentID}
	duration := time.Duration(scanner.HardTimeoutMinutes(legacyMode(task), timeoutMinutes(task))) * time.Minute
	if lease.Assignment != nil {
		budget := lease.Assignment.Policy.TimeBudget
		duration = time.Duration(budget.ScanSeconds+budget.FinalizationGraceSeconds) * time.Second
	}
	runCtx, cancel := context.WithTimeout(ctx, duration)
	defer cancel()
	hbCtx, stopHB := context.WithCancel(ctx)
	defer stopHB()
	var lost, canceled, completionStarted atomic.Bool
	hbDone := make(chan struct{})
	// Renew through completion retries too. Losing the lease terminates this process group.
	go func() {
		defer close(hbDone)
		lastOK := time.Now()
		leaseDuration := time.Duration(lease.LeaseSeconds) * time.Second
		interval, reserve := renewalTiming(leaseDuration)
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-hbCtx.Done():
				return
			case <-ticker.C:
				// A blocked HTTP call must not outlive the safe renewal window.
				renewCtx, stopRenew := context.WithDeadline(hbCtx, lastOK.Add(leaseDuration-reserve))
				status, e := sendHeartbeat(renewCtx, c, task, ref)
				stopRenew()

				if e == nil {
					lastOK = time.Now()
					if status == "cancel_requested" {
						canceled.Store(true)
						cancel()
					}
				}
				if e != nil {
					log.Printf("task=%d attempt=%d heartbeat: %v", task.NumericTaskID(), int(task.Execution.Attempt), e)
				}
				if errors.Is(e, execution.ErrLeaseLost) || errors.Is(e, execution.ErrUnauthorized) || time.Since(lastOK) >= leaseDuration-reserve {
					lost.Store(true)
					cancel()
					return
				}
			}
		}
	}()
	completion := control.LegacyCompletion{LegacyLeaseRef: ref, Status: "failed"}
	var full *contracts.ExecutionCompletion
	if task.Legacy != nil && task.Legacy.Task.ClaimedFrom == "cancel_requested" {
		completion.Status = "canceled"
		completion.ErrorCode = "canceled"
		completion.ErrorMessage = "canceled before scan"
	} else {
		var result *scanner.Result
		var err error
		if lease.Assignment != nil {
			a := lease.Assignment
			job, e := scanner.JobFromAssignment(*a, task.SourcePath, task.OutputPath)
			if e == nil && !a.Credential.Present && os.Getenv("AXIOM_SCAN_MOCK") != "1" {
				e = errors.New("selected model has no credential; configure its API Key in the Web model settings")
			}
			if e != nil {
				completion.ErrorCode = "configuration_error"
				err = e
			} else {
				overrides := map[string]string{}
				if a.Credential.Present {
					overrides[a.Credential.Value.EnvName] = a.Credential.Value.Value
				}
				result, err = scanner.Run(runCtx, scanner.Request{ObservationContractVersion: task.ObservationContractVersion, ResultContractVersion: task.ResultContractVersion, Job: job, TaskID: task.NumericTaskID(), Attempt: int(task.Execution.Attempt), OutputPath: task.OutputPath, Provider: string(a.Model.ProviderID), ScannerDir: w.Cfg.ScannerDir, NodePath: w.Cfg.NodePath, EnvOverrides: overrides, Mock: os.Getenv("AXIOM_SCAN_MOCK") == "1"})
			}
		} else {
			result, err = w.runLegacy(runCtx, task.Legacy)
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if lost.Load() {
			return execution.ErrLeaseLost
		}
		if task.ResultContractVersion == 1 {
			full = buildCompletion(task.Execution, w.Cfg.AgentID, result, err, canceled.Load(), errors.Is(runCtx.Err(), context.DeadlineExceeded), completion.ErrorCode)
		} else {
			switch {
			case canceled.Load():
				completion.Status = "canceled"
				completion.ErrorCode = "canceled"
				completion.ErrorMessage = "canceled by user"
			case errors.Is(runCtx.Err(), context.DeadlineExceeded):
				completion.Status = "timeout"
				completion.ErrorCode = "timeout"
				completion.ErrorMessage = "scan exceeded configured time limit"
			case err != nil:
				if completion.ErrorCode == "" {
					completion.ErrorCode = "scanner_error"
				}
				completion.ErrorMessage = err.Error()
			case result != nil:
				completion.Coverage = result.Coverage
				completion.Status = result.Status
				if !legacyTerminalStatus(completion.Status) {
					completion.Status = "failed"
					completion.ErrorCode = "invalid_result"
				}
				if result.Error != nil {
					completion.ErrorCode = result.Error.Code
					completion.ErrorMessage = result.Error.Message
				}
				for _, f := range result.Findings {
					completion.Findings = append(completion.Findings, control.LegacyFinding{ScanID: task.NumericTaskID(), Fingerprint: f.Fingerprint, Title: f.Title, Severity: f.Severity, Category: f.Category, CWE: f.CWE, FilePath: f.FilePath, StartLine: f.StartLine, EndLine: f.EndLine, Description: f.Description, Evidence: f.Evidence, Remediation: f.Remediation, Confidence: f.Confidence, Raw: f.Raw})
				}
			}
		}
	}
	if task.ResultContractVersion == 1 && full == nil {
		full = buildCompletion(task.Execution, w.Cfg.AgentID, nil, errors.New(completion.ErrorMessage), completion.Status == "canceled", false, completion.ErrorCode)
	}
	if full != nil {
		completion.Status = string(full.ExecutionStatus)
	}
	if full == nil && (completion.Status == "failed" || completion.Status == "timeout" || completion.Status == "partial") {
		completion.ErrorCode = classifyLegacyFailure(completion.ErrorCode, completion.ErrorMessage)
	}
	if task.ObservationContractVersion == 0 {
		if p, e := scanner.ReadProgress(task.OutputPath); e == nil {
			b, _ := json.Marshal(p)
			completion.ProgressJSON = string(b)
		}
	}
	finalProgressAccepted := task.ObservationContractVersion == 0
	for retry := 0; retry < 6; retry++ {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if lost.Load() && !completionStarted.Load() {
			return execution.ErrLeaseLost
		}
		var err error
		if !finalProgressAccepted {
			_, err = sendHeartbeat(ctx, c, task, ref)
			finalProgressAccepted = err == nil
		}
		if err == nil && full != nil {
			completionStarted.Store(true)
			err = c.CompleteContract(ctx, *full)
		} else if err == nil {
			completionStarted.Store(true)
			err = c.Complete(ctx, completion)
		}
		if err == nil {
			stopHB()
			<-hbDone
			log.Printf("task=%d attempt=%d status=%s findings=%d", task.NumericTaskID(), int(task.Execution.Attempt), completion.Status, completionFindingCount(full, completion))
			return nil
		}
		var protocol *execution.ProtocolError
		if errors.Is(err, execution.ErrLeaseLost) || errors.Is(err, execution.ErrCompletionConflict) || errors.Is(err, execution.ErrUnauthorized) || (errors.As(err, &protocol) && !protocol.Retryable) {
			return err
		}
		log.Printf("task=%d completion retry=%d: %v", task.NumericTaskID(), retry+1, err)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(time.Duration(retry+1) * time.Second):
		}
	}
	return errors.New("completion unavailable; lease will expire for recovery")
}

// Keep normal 60-second leases unchanged, and support the Server's shorter
// valid leases without a negative cutoff or overlapping recovered executions.
func renewalTiming(lease time.Duration) (interval, reserve time.Duration) {
	interval = min(5*time.Second, lease/3)
	reserve = min(15*time.Second, lease/3)
	return interval, reserve
}
func timeoutMinutes(task *execution.Lease) int {
	if task.Legacy == nil || task.Legacy.Task.TimeoutMinutes <= 0 {
		return 60
	}
	return task.Legacy.Task.TimeoutMinutes
}
func legacyMode(task *execution.Lease) string {
	if task.Legacy == nil {
		return "standard"
	}
	return task.Legacy.Task.ScanMode
}
