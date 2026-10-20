package store

import (
	"axiom/contracts"
	"axiom/contracts/control"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
)

var ErrLeaseLost = errors.New("execution lease is no longer owned")
var ErrCompletionConflict = errors.New("completion content conflicts with the archived execution")
var ErrInvalidResult = errors.New("invalid execution result")

type ClaimLimits struct {
	SDKConcurrencyCapable bool
	SDKParametersCapable  bool
	SDKIdentityCapable    bool
	// Deprecated: the following three values seed historical metadata only.
	Global        int
	PerModel      int
	PerProject    int
	Lease         time.Duration
	OutputRoot    string
	MaxRecoveries int
	StorageCheck  func(context.Context) string `json:"-"`
}
type LeaseRef struct {
	TaskID  int64  `json:"taskId"`
	Attempt int    `json:"attempt"`
	AgentID string `json:"agentId"`
}
type Attempt struct {
	ResultRecovery             *contracts.RecoveredResult    `json:"resultRecovery,omitempty"`
	ObservationContractVersion int                           `json:"observationContractVersion"`
	ProgressDiagnostic         *contracts.ContractError      `json:"progressDiagnostic,omitempty"`
	ProgressDiagnosticJSON     string                        `json:"-"`
	Progress                   *contracts.ProgressSnapshotV2 `json:"progress,omitempty"`
	QueuedAt                   *time.Time                    `json:"queuedAt"`
	ResultAvailable            bool                          `json:"resultAvailable"`
	FindingCount               int                           `json:"findingCount"`
	TaskID                     int64                         `json:"taskId"`
	Attempt                    int                           `json:"attempt"`
	AgentID                    string                        `json:"agentId"`
	Status                     string                        `json:"status"`
	StartedAt                  string                        `json:"startedAt"`
	FinishedAt                 string                        `json:"finishedAt"`
	OutputPath                 string                        `json:"outputPath"`
	ErrorCode                  string                        `json:"errorCode"`
	ErrorMessage               string                        `json:"errorMessage"`
	ProgressJSON               string                        `json:"progressJson"`
}
type Completion struct {
	recovery     *resultRecovery
	contract     *contracts.ExecutionCompletion
	Coverage     json.RawMessage `json:"coverage,omitempty"`
	ProgressJSON string          `json:"progressJson,omitempty"`
	LeaseRef
	Status       string    `json:"status"`
	ErrorCode    string    `json:"errorCode"`
	ErrorMessage string    `json:"errorMessage"`
	Findings     []Finding `json:"findings"`
}

func (s *Store) ClaimExecution(ctx context.Context, agent string, limits ClaimLimits) (*Task, error) {
	return s.ClaimExecutionWithInput(ctx, agent, limits, contracts.InputCapabilities{})
}

func (s *Store) ClaimExecutionWithInput(ctx context.Context, agent string, limits ClaimLimits, capabilities contracts.InputCapabilities) (*Task, error) {
	return s.ClaimExecutionWithContracts(ctx, agent, limits, capabilities, contracts.NoResultCapabilities())
}

func (s *Store) ClaimExecutionWithContracts(ctx context.Context, agent string, limits ClaimLimits, capabilities contracts.InputCapabilities, results contracts.ResultCapabilities) (*Task, error) {
	return s.ClaimExecutionWithObservations(ctx, agent, limits, capabilities, results, contracts.NoObservationCapabilities())
}
func (s *Store) ClaimExecutionWithObservations(ctx context.Context, agent string, limits ClaimLimits, capabilities contracts.InputCapabilities, results contracts.ResultCapabilities, observations contracts.ObservationCapabilities) (*Task, error) {
	if !s.db.postgres {
		return nil, errors.New("concurrent execution requires PostgreSQL")
	}
	if limits.Lease < 10*time.Second {
		return nil, errors.New("invalid queue limits")
	}
	release, e := s.storageGate(ctx, false)
	if e != nil {
		return nil, e
	}
	defer release()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	// Serialize claims and budget/identity bindings; task-count quotas are retired.
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(740919)`); err != nil {
		return nil, err
	}
	now := time.Now().UTC()
	stamp := now.Format(time.RFC3339)
	// Validation cannot report final cost. A lost lease must never replay it.
	if _, err = tx.ExecContext(ctx, `UPDATE scan_attempts a SET status='failed',finished_at=?,error_code='lease_expired',error_message='SDK validation interrupted; final cost unknown',lease_expires_at=NULL FROM scan_tasks t JOIN sdk_operations o ON o.task_id=t.id WHERE a.task_id=t.id AND a.attempt=t.attempt AND o.kind='validate' AND t.status IN ('claimed','scanning') AND (t.lease_expires_at IS NULL OR t.lease_expires_at<?)`, stamp, stamp); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE scan_tasks t SET status='failed',finished_at=?,error_code='lease_expired',error_message='SDK validation interrupted; final cost unknown',lease_expires_at=NULL FROM sdk_operations o WHERE o.task_id=t.id AND o.kind='validate' AND t.status IN ('claimed','scanning') AND (t.lease_expires_at IS NULL OR t.lease_expires_at<?)`, stamp, stamp); err != nil {
		return nil, err
	}

	if _, err = tx.ExecContext(ctx, `UPDATE scan_attempts a SET status='failed',finished_at=?,error_code='lease_expired',error_message='组件规划中断；请在 Web 创建新的操作，费用保留未知',lease_expires_at=NULL FROM scan_tasks t JOIN component_operations o ON o.task_id=t.id WHERE a.task_id=t.id AND a.attempt=t.attempt AND t.status IN ('claimed','scanning') AND (t.lease_expires_at IS NULL OR t.lease_expires_at<?)`, stamp, stamp); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE scan_tasks t SET status='failed',finished_at=?,error_code='lease_expired',error_message='组件规划中断；请在 Web 创建新的操作，费用保留未知',lease_expires_at=NULL FROM component_operations o WHERE o.task_id=t.id AND t.status IN ('claimed','scanning') AND (t.lease_expires_at IS NULL OR t.lease_expires_at<?)`, stamp, stamp); err != nil {
		return nil, err
	}
	// A total-budget execution that disappears has unknown final cost. Stop
	// automatic recovery before creating another SDK call with a fresh allowance.
	if _, err = tx.ExecContext(ctx, `UPDATE scan_attempts a SET status='failed',finished_at=?,error_code='lease_expired',error_message='final cost unknown; Web reconciliation required',lease_expires_at=NULL FROM scan_tasks t JOIN task_budgets b ON b.task_id=t.id WHERE b.total_micros IS NOT NULL AND a.task_id=t.id AND a.attempt=t.attempt AND t.status IN ('claimed','scanning') AND (t.lease_expires_at IS NULL OR t.lease_expires_at<?)`, stamp, stamp); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE scan_tasks t SET status='failed',finished_at=?,error_code='lease_expired',error_message='final cost unknown; Web reconciliation required',lease_expires_at=NULL FROM task_budgets b WHERE b.task_id=t.id AND b.total_micros IS NOT NULL AND t.status IN ('claimed','scanning') AND (t.lease_expires_at IS NULL OR t.lease_expires_at<?)`, stamp, stamp); err != nil {
		return nil, err
	}
	// A canceled run with a dead worker is finalized without starting another attempt.
	if _, err = tx.ExecContext(ctx, `UPDATE scan_attempts a SET status='canceled',finished_at=?,error_code='canceled',error_message='canceled after worker lease expired',lease_expires_at=NULL FROM scan_tasks t WHERE a.task_id=t.id AND a.attempt=t.attempt AND t.status='cancel_requested' AND (t.lease_expires_at IS NULL OR t.lease_expires_at<?)`, stamp, stamp); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE scan_tasks SET status='canceled',finished_at=?,error_code='canceled',error_message='canceled after worker lease expired',lease_expires_at=NULL WHERE status='cancel_requested' AND (lease_expires_at IS NULL OR lease_expires_at<?)`, stamp, stamp); err != nil {
		return nil, err
	}
	// Exhausted lease recovery is terminal rather than an unbounded model-cost loop.
	if _, err = tx.ExecContext(ctx, `UPDATE scan_attempts a SET status='failed',finished_at=?,error_code='lease_expired',error_message='automatic recovery limit reached' FROM scan_tasks t WHERE a.task_id=t.id AND a.attempt=t.attempt AND t.status IN ('claimed','scanning') AND (t.lease_expires_at IS NULL OR t.lease_expires_at < ?) AND t.automatic_retries >= ?`, stamp, stamp, limits.MaxRecoveries); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE scan_tasks SET status='failed',finished_at=?,error_code='lease_expired',error_message='automatic recovery limit reached',lease_expires_at=NULL WHERE status IN ('claimed','scanning') AND (lease_expires_at IS NULL OR lease_expires_at < ?) AND automatic_retries >= ?`, stamp, stamp, limits.MaxRecoveries); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE task_cost_attempts c SET state='unknown',reason='execution_interrupted',updated_at=? FROM scan_attempts a WHERE a.task_id=c.task_id AND a.attempt=c.attempt AND c.state='active' AND (a.status NOT IN ('claimed','scanning') OR a.lease_expires_at<?)`, securityTime(now), stamp); err != nil {
		return nil, err
	}
	if limits.StorageCheck != nil && limits.StorageCheck(ctx) != "" {
		return nil, tx.Commit()
	}
	var id int64
	var attempt int
	var previous string
	// A fresh runtime report can narrow admission further than the requested
	// capabilities. Old Agents have no report and retain capability-only admission.
	runtimeAPI := control.ControlAPIs()[len(control.ControlAPIs())-1]
	var runtimeJSON string
	var runtimeSeen *time.Time
	err = tx.QueryRowContext(ctx, `SELECT runtime_json,runtime_seen_at FROM execution_agents WHERE agent_id=?`, agent).Scan(&runtimeJSON, &runtimeSeen)
	if err != nil && !errors.Is(err, sql.ErrNoRows) {
		return nil, err
	}
	if runtimeJSON != "" && runtimeSeen != nil && now.Sub(*runtimeSeen) < 30*time.Second {
		runtime, e := contracts.DecodeAgentRuntime([]byte(runtimeJSON), nil)
		if e != nil {
			return nil, e
		}
		if runtime.State == contracts.AgentRuntimeStateBlocked || runtime.MatrixHash != control.MatrixHash {
			return nil, tx.Commit()
		}
		runtimeAPI = int(runtime.ControlAPI)
		limits.SDKConcurrencyCapable = limits.SDKConcurrencyCapable && runtime.ScannerDescriptionVersion != nil && contracts.DescriptionSupportsSDKConcurrency(*runtime.ScannerDescriptionVersion)
		limits.SDKParametersCapable = limits.SDKParametersCapable && runtime.ScannerDescriptionVersion != nil && contracts.DescriptionSupportsSDKParameters(*runtime.ScannerDescriptionVersion)
		limits.SDKIdentityCapable = limits.SDKIdentityCapable && runtime.ScannerDescriptionVersion != nil && contracts.DescriptionSupportsSDKIdentity(*runtime.ScannerDescriptionVersion)
	}
	if runtimeSeen == nil || now.Sub(*runtimeSeen) >= 30*time.Second {
		limits.SDKIdentityCapable = false
		limits.SDKParametersCapable = false
		limits.SDKConcurrencyCapable = false
	}
	caps := control.Capabilities{SDKConcurrency: limits.SDKConcurrencyCapable, SDKParameters: limits.SDKParametersCapable, Input: capabilities, Result: results, Observation: observations}
	filters := []string{}
	for _, p := range control.Profiles() {
		if p.ControlAPI <= runtimeAPI && caps.ReadyFor(control.Requirements{Input: p.Input, Result: p.Result, Observation: p.Observation}) {
			filters = append(filters, fmt.Sprintf("(t.input_contract_version=%d AND t.result_contract_version=%d AND t.observation_contract_version=%d)", p.Input, p.Result, p.Observation))
		}
	}
	if len(filters) == 0 {
		return nil, tx.Commit()
	}
	err = tx.QueryRowContext(ctx, `SELECT t.id,t.attempt,t.status FROM scan_tasks t
 WHERE (t.status='pending' OR (t.status IN ('claimed','scanning') AND (t.lease_expires_at IS NULL OR t.lease_expires_at<?)))
 AND (`+strings.Join(filters, " OR ")+`)
 AND (NOT EXISTS(SELECT 1 FROM task_sdk_identities b WHERE b.task_id=t.id) OR (? AND EXISTS(SELECT 1 FROM task_sdk_identities b JOIN sdk_identities i ON i.id=b.identity_id WHERE b.task_id=t.id AND i.agent_id=? AND i.state='authenticated' AND i.authenticated=TRUE AND i.cleanup_pending=FALSE AND i.generation=(b.reference_json::jsonb->>'generation')::bigint AND (`+fmt.Sprint(limits.SDKConcurrencyCapable)+` OR NOT EXISTS(SELECT 1 FROM task_sdk_identities active_binding JOIN scan_tasks active_task ON active_task.id=active_binding.task_id WHERE active_binding.identity_id=i.id AND active_task.id<>t.id AND active_task.status IN ('claimed','scanning','cancel_requested') AND active_task.lease_expires_at>=?)) AND NOT EXISTS(SELECT 1 FROM sdk_identity_operations o WHERE o.identity_id=i.id AND o.status IN ('queued','running','waiting')))))
 AND (NOT EXISTS(SELECT 1 FROM sdk_operations op WHERE op.task_id=t.id) OR `+fmt.Sprint(containsOperationJob(observations))+`)
 AND (COALESCE(t.sdk_parameters_json,'')='' OR `+fmt.Sprint(limits.SDKParametersCapable)+`)
 AND (`+fmt.Sprint(limits.SDKConcurrencyCapable)+` OR NOT `+s.needsSDKConcurrencySQL("t")+`)
 AND (NOT EXISTS(SELECT 1 FROM task_scan_inputs si WHERE si.task_id=t.id) OR ?)
 AND ((t.max_cost_usd IS NULL AND NOT EXISTS(SELECT 1 FROM task_budgets b WHERE b.task_id=t.id AND b.total_micros IS NOT NULL)) OR (t.input_contract_version IN (1,2,3,4,5,6) AND ?))
 AND NOT EXISTS(SELECT 1 FROM task_budgets b WHERE b.task_id=t.id AND b.total_micros IS NOT NULL AND (EXISTS(SELECT 1 FROM task_cost_attempts c WHERE c.task_id=t.id AND c.state='unknown') OR COALESCE((SELECT SUM(c.booked_micros) FROM task_cost_attempts c WHERE c.task_id=t.id),0)>=b.total_micros))
 ORDER BY t.queued_at,t.id LIMIT 1 FOR UPDATE OF t SKIP LOCKED`, stamp, limits.SDKIdentityCapable, agent, stamp, containsJob4(observations), capabilities.MaxCostUSD).Scan(&id, &attempt, &previous)
	if errors.Is(err, sql.ErrNoRows) {
		return nil, tx.Commit()
	}
	if err != nil {
		return nil, err
	}
	grant, err := s.checkBudgetAdmission(ctx, tx, id)
	if err != nil {
		return nil, err
	}
	if previous != "pending" {
		if _, err = tx.ExecContext(ctx, `UPDATE scan_attempts SET status='abandoned',finished_at=?,error_code='lease_expired',error_message='execution lease expired; recovered by another worker' WHERE task_id=? AND attempt=? AND status IN ('claimed','scanning')`, stamp, id, attempt); err != nil {
			return nil, err
		}
	}
	attempt++
	out := filepath.Join(limits.OutputRoot, fmt.Sprint(id), fmt.Sprintf("attempt-%d", attempt))
	until := now.Add(limits.Lease).Format(time.RFC3339)
	recoveries := 0
	if previous != "pending" {
		recoveries = 1
	}
	if _, err = tx.ExecContext(ctx, `UPDATE scan_tasks SET status='claimed',agent_id=?,attempt=?,lease_expires_at=?,heartbeat_at=?,started_at=?,finished_at=NULL,output_path=?,progress_json='',progress_diagnostic_json='',current_stage='',error_code=NULL,error_message=NULL,finding_count=0,automatic_retries=automatic_retries+?,queued_at=CASE WHEN ? THEN NOW() ELSE queued_at END WHERE id=?`, agent, attempt, until, stamp, stamp, out, recoveries, recoveries > 0, id); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO scan_attempts(task_id,attempt,agent_id,status,lease_expires_at,started_at,output_path,queued_at,result_contract_version,observation_contract_version) SELECT ?,?,?,'claimed',?,?,?,queued_at,result_contract_version,observation_contract_version FROM scan_tasks WHERE id=?`, id, attempt, agent, until, stamp, out, id); err != nil {
		return nil, err
	}
	if err = s.reserveComponentScan(ctx, tx, id, attempt, grant); err != nil {
		return nil, err
	}
	if err = s.reserveAttemptBudget(ctx, tx, id, attempt, grant); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	t, err := s.Get(ctx, id)
	if t != nil {
		t.ClaimedFrom = previous
		if grant != nil {
			limit := dollars(*grant)
			t.ExecutionMaxCostUSD = &limit
		}
	}
	if err == nil && t != nil && t.ScanInput != nil && t.ScanInput.ParentScanID.Present {
		t.ParentState, err = s.TaskParentState(ctx, id)
		if err != nil {
			return nil, err
		}
		if t.ParentState == nil {
			return nil, errors.New("frozen parent state unavailable")
		}
	}
	return t, err
}
func (s *Store) HeartbeatExecution(ctx context.Context, ref LeaseRef, lease time.Duration, stage, progress string) (string, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return "", err
	}
	defer tx.Rollback()
	now := time.Now().UTC()
	stamp := now.Format(time.RFC3339)
	until := now.Add(lease).Format(time.RFC3339)
	var status string
	err = tx.QueryRowContext(ctx, `UPDATE scan_tasks SET heartbeat_at=?,lease_expires_at=?,current_stage=?,progress_json=?,status=CASE WHEN status='claimed' THEN 'scanning' ELSE status END WHERE id=? AND agent_id=? AND attempt=? AND status IN ('claimed','scanning','cancel_requested') AND observation_contract_version=0 AND lease_expires_at>=? RETURNING status`, stamp, until, stage, progress, ref.TaskID, ref.AgentID, ref.Attempt, stamp).Scan(&status)
	if errors.Is(err, sql.ErrNoRows) {
		return "", ErrLeaseLost
	}
	if err != nil {
		return "", err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE scan_attempts SET status='scanning',lease_expires_at=?,progress_json=? WHERE task_id=? AND attempt=? AND agent_id=?`, until, progress, ref.TaskID, ref.Attempt, ref.AgentID); err != nil {
		return "", err
	}
	return status, tx.Commit()
}
func (s *Store) CompleteExecution(ctx context.Context, c Completion) error {
	if !IsTerminalStatus(c.Status) {
		return errors.New("invalid terminal status")
	}
	raw, err := completionJSON(c)
	if err != nil {
		return err
	}
	digest := sha256.Sum256(raw)
	hash := hex.EncodeToString(digest[:])
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	var owner, status, lease string
	var attempt, resultVersion, observationVersion int
	var modelSnapshot string
	err = tx.QueryRowContext(ctx, `SELECT COALESCE(agent_id,''),attempt,status,COALESCE(lease_expires_at,''),result_contract_version,observation_contract_version,COALESCE(model_snapshot_json,'') FROM scan_tasks WHERE id=? FOR UPDATE`, c.TaskID).Scan(&owner, &attempt, &status, &lease, &resultVersion, &observationVersion, &modelSnapshot)
	if err != nil {
		return err
	}
	if owner != c.AgentID || attempt != c.Attempt {
		return ErrLeaseLost
	}
	if observationVersion == 1 && c.ProgressJSON != "" {
		return ErrInvalidResult
	}
	if (resultVersion == 1) != (c.contract != nil) {
		return ErrInvalidResult
	}
	if c.contract != nil && c.contract.Result.Present {
		model, e := contracts.DecodeResolvedModel([]byte(modelSnapshot), nil)
		if e != nil || !sameJSON(model, c.contract.Result.Value.Model) {
			return ErrInvalidResult
		}
	}
	if c.recovery != nil {
		if err = s.authorizeResultRecovery(ctx, tx, c, status, lease); err != nil {
			if errors.Is(err, errRecoveryReplayed) {
				return tx.Commit()
			}
			return err
		}
	} else if IsTerminalStatus(status) {
		var existing string
		if err = tx.QueryRowContext(ctx, `SELECT COALESCE(completion_hash,'') FROM scan_attempts WHERE task_id=? AND attempt=? AND agent_id=?`, c.TaskID, c.Attempt, c.AgentID).Scan(&existing); err != nil {
			return err
		}
		if existing == hash {
			return tx.Commit()
		}
		return ErrCompletionConflict
	}
	now := time.Now().UTC().Format(time.RFC3339)
	if c.recovery == nil && lease < now {
		return ErrLeaseLost
	}
	if status == "cancel_requested" {
		c.Status = "canceled"
		if c.contract == nil {
			c.Findings = nil
		}
		c.ErrorCode = "canceled"
		c.ErrorMessage = "canceled by user"
		if c.contract != nil {
			c.contract.ExecutionStatus = contracts.TerminalStatusCanceled
			c.contract.Error = contracts.Some(contracts.ContractError{Code: contracts.ErrorCodeCanceled, Origin: contracts.ErrorOriginServer, Message: c.ErrorMessage, Retryable: false})
		}
	}
	raw, err = completionJSON(c)
	if err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `DELETE FROM findings WHERE scan_id=?`, c.TaskID); err != nil {
		return err
	}
	stmt, err := tx.PrepareContext(ctx, `INSERT INTO findings(scan_id,fingerprint,title,severity,category,cwe,file_path,start_line,end_line,description,evidence,remediation,confidence,raw,normalized_json,created_at) VALUES(?,?,?,?,?,?,?,?,?,?,?,?,?,?,?,?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, f := range c.Findings {
		cwe, _ := json.Marshal(f.CWE)
		if _, err = stmt.ExecContext(ctx, c.TaskID, f.Fingerprint, f.Title, f.Severity, nullIfEmpty(f.Category), string(cwe), nullIfEmpty(f.FilePath), f.StartLine, f.EndLine, f.Description, nullIfEmpty(f.Evidence), nullIfEmpty(f.Remediation), f.Confidence, nullIfEmpty(string(f.Raw)), nullIfEmpty(string(f.Normalized)), now); err != nil {
			return err
		}
	}
	if c.contract != nil && c.contract.Result.Present && c.contract.Result.Value.Extensions.Present {
		if data := c.contract.Result.Value.Extensions.Value["componentScan"]; data != nil {
			v, e := contracts.DecodeComponentScanResult(data, &c.contract.Execution)
			if e != nil {
				return ErrInvalidResult
			}
			if e = s.recordComponentSnapshot(ctx, tx, c.TaskID, int64(c.Attempt), &v.Snapshot); e != nil {
				return e
			}
		}
	}
	parentStatus, e := s.componentParentStatus(ctx, tx, c)
	if e != nil {
		return e
	}
	if _, err = tx.ExecContext(ctx, `UPDATE scan_tasks SET status=?,error_code=?,error_message=?,finding_count=?,finished_at=?,lease_expires_at=NULL,progress_json=CASE WHEN ?<>'' THEN ? ELSE progress_json END WHERE id=?`, parentStatus, nullIfEmpty(c.ErrorCode), nullIfEmpty(c.ErrorMessage), len(c.Findings), now, c.ProgressJSON, c.ProgressJSON, c.TaskID); err != nil {
		return err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE scan_attempts SET status=?,error_code=?,error_message=?,finished_at=?,lease_expires_at=NULL,result_json=?,completion_hash=?,progress_json=CASE WHEN ?<>'' THEN ? ELSE progress_json END WHERE task_id=? AND attempt=? AND agent_id=?`, c.Status, c.ErrorCode, c.ErrorMessage, now, string(raw), hash, c.ProgressJSON, c.ProgressJSON, c.TaskID, c.Attempt, c.AgentID); err != nil {
		return err
	}
	if c.Status == "completed" && len(c.Findings) > 0 {
		if _, err = tx.ExecContext(ctx, `UPDATE model_configs SET compatibility_status='supported',updated_at=? WHERE id=(SELECT model_config_id FROM scan_tasks WHERE id=?) AND compatibility_status='experimental'`, now, c.TaskID); err != nil {
			return err
		}
	}

	if err = s.recordFinalCost(ctx, tx, c); err != nil {
		return err
	}
	if err = s.indexNativeResult(ctx, tx, c); err != nil {
		return err
	}
	if c.recovery != nil {
		if err = saveResultRecovery(ctx, tx, c); err != nil {
			return err
		}
	}
	return tx.Commit()
}
func (s *Store) ListAttempts(ctx context.Context, id int64) ([]Attempt, error) {
	if !s.db.postgres {
		return []Attempt{}, nil
	}
	rows, err := s.db.QueryContext(ctx, `SELECT progress_diagnostic_json,observation_contract_version,task_id,attempt,agent_id,status,started_at,COALESCE(finished_at,''),output_path,error_code,error_message,progress_json,queued_at,result_json IS NOT NULL,CASE WHEN result_contract_version=1 AND jsonb_typeof(result_json::jsonb->'result'->'findings')='array' THEN jsonb_array_length(result_json::jsonb->'result'->'findings') WHEN jsonb_typeof(result_json::jsonb->'findings')='array' THEN jsonb_array_length(result_json::jsonb->'findings') ELSE 0 END,COALESCE((SELECT document_json FROM scan_result_recoveries r WHERE r.task_id=scan_attempts.task_id AND r.attempt=scan_attempts.attempt),'') FROM scan_attempts WHERE task_id=? ORDER BY attempt DESC`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Attempt{}
	for rows.Next() {
		var a Attempt
		var recoveryJSON string
		if err = rows.Scan(&a.ProgressDiagnosticJSON, &a.ObservationContractVersion, &a.TaskID, &a.Attempt, &a.AgentID, &a.Status, &a.StartedAt, &a.FinishedAt, &a.OutputPath, &a.ErrorCode, &a.ErrorMessage, &a.ProgressJSON, &a.QueuedAt, &a.ResultAvailable, &a.FindingCount, &recoveryJSON); err != nil {
			return nil, err
		}
		if a.ObservationContractVersion == 1 && a.ProgressJSON != "" {
			expected := contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(a.TaskID)), Attempt: int64(a.Attempt)}
			if p, e := contracts.DecodeProgressSnapshotV2([]byte(a.ProgressJSON), &expected); e == nil {
				a.Progress = &p
			}
		}
		if a.ProgressDiagnosticJSON != "" {
			var d contracts.ContractError
			if json.Unmarshal([]byte(a.ProgressDiagnosticJSON), &d) == nil {
				a.ProgressDiagnostic = &d
			}
		}
		if recoveryJSON != "" {
			var record contracts.RecoveredResult
			if err = json.Unmarshal([]byte(recoveryJSON), &record); err != nil {
				return nil, err
			}
			a.ResultRecovery = &record
		}
		out = append(out, a)
	}
	return out, rows.Err()
}
func (s *Store) GetAttempt(ctx context.Context, id int64, attempt int) (*Attempt, error) {
	all, err := s.ListAttempts(ctx, id)
	if err != nil {
		return nil, err
	}
	for _, a := range all {
		if a.Attempt == attempt {
			return &a, nil
		}
	}
	return nil, sql.ErrNoRows
}

func containsJob4(v contracts.ObservationCapabilities) bool {
	for _, n := range v.JobVersions {
		if n == contracts.ScanInputJobVersion {
			return true
		}
	}
	return false
}
