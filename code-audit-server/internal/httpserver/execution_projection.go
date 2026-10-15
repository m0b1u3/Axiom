package httpserver

import (
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/contracts/control"
	"encoding/json"
	"strconv"
)

func assignmentFromTask(task *store.Task, agent string, leaseSeconds int, credential string) (*contracts.ExecutionAssignment, error) {
	model, err := contracts.DecodeResolvedModel([]byte(task.ModelSnapshotJSON), nil)
	if err != nil {
		return nil, err
	}
	policy, err := contracts.DecodeScanPolicy([]byte(task.PolicySnapshotJSON), nil)
	if err != nil {
		return nil, err
	}
	if task.ExecutionMaxCostUSD != nil {
		policy.MaxCostUSD = contracts.Some(*task.ExecutionMaxCostUSD)
	}
	v := contracts.ExecutionAssignment{
		SchemaVersion: contracts.Version,
		Execution:     contracts.ExecutionRef{TaskID: contracts.TaskID(strconv.FormatInt(task.ID, 10)), Attempt: int64(task.Attempt)},
		Source:        contracts.SnapshotRef{SnapshotID: contracts.TaskID(strconv.FormatInt(task.SourceSnapshotID, 10)), ContentHash: contracts.Digest(task.SourceContentHash)},
		Model:         model, Policy: policy,
		Lease: contracts.Lease{AgentID: contracts.Identifier(agent), LeaseSeconds: int64(leaseSeconds)},
	}
	if credential != "" {
		v.Credential = contracts.Some(contracts.TransientCredential{EnvName: model.CredentialEnv, Value: credential})
	}
	raw, err := json.Marshal(v)
	if err != nil {
		return nil, err
	}
	checked, err := contracts.DecodeExecutionAssignment(raw, nil)
	return &checked, err
}

func legacyTask(t *store.Task) *control.LegacyTask {
	if t == nil {
		return nil
	}
	return &control.LegacyTask{
		ID:                         t.ID,
		ScanNo:                     t.ScanNo,
		SourcePath:                 t.SourcePath,
		Provider:                   t.Provider,
		Model:                      t.Model,
		BaseURL:                    t.BaseURL,
		ScanMode:                   t.ScanMode,
		IncludePaths:               t.IncludePaths,
		ScanFocus:                  t.ScanFocus,
		ValidationMode:             t.ValidationMode,
		BusinessContext:            t.BusinessContext,
		TimeoutMinutes:             t.TimeoutMinutes,
		DeepMaxDiscoveryRuns:       t.DeepMaxDiscoveryRuns,
		DeepStopAfterNoNew:         t.DeepStopAfterNoNew,
		MaxCostUSD:                 t.MaxCostUSD,
		Workers:                    t.Workers,
		ObservationContractVersion: t.ObservationContractVersion,
		Progress:                   t.Progress,
		ProgressDiagnostic:         t.ProgressDiagnostic,
		ResultContractVersion:      t.ResultContractVersion,
		InputContractVersion:       t.InputContractVersion,
		SourceContentHash:          t.SourceContentHash,
		Status:                     t.Status,
		CurrentStage:               t.CurrentStage,
		ProgressJSON:               t.ProgressJSON,
		AgentID:                    t.AgentID,
		Attempt:                    t.Attempt,
		FindingCount:               t.FindingCount,
		ErrorCode:                  t.ErrorCode,
		ErrorMessage:               t.ErrorMessage,
		OutputPath:                 t.OutputPath,
		SourceSnapshotID:           t.SourceSnapshotID,
		ModelConfigID:              t.ModelConfigID,
		ProjectID:                  t.ProjectID,
		ProjectName:                t.ProjectName,
		CreatedAt:                  t.CreatedAt,
		StartedAt:                  t.StartedAt,
		FinishedAt:                 t.FinishedAt,
		ClaimedFrom:                t.ClaimedFrom,
	}
}

func legacyLeaseRef(v control.LegacyLeaseRef) store.LeaseRef {
	return store.LeaseRef{TaskID: v.TaskID, Attempt: v.Attempt, AgentID: v.AgentID}
}
func legacyCompletion(v control.LegacyCompletion) store.Completion {
	c := store.Completion{LeaseRef: legacyLeaseRef(v.LegacyLeaseRef), Coverage: v.Coverage, ProgressJSON: v.ProgressJSON, Status: v.Status, ErrorCode: v.ErrorCode, ErrorMessage: v.ErrorMessage}
	if v.Findings != nil {
		c.Findings = make([]store.Finding, 0, len(v.Findings))
	}
	for _, f := range v.Findings {
		c.Findings = append(c.Findings, store.Finding{Normalized: f.Normalized, ID: f.ID, ScanID: f.ScanID, Fingerprint: f.Fingerprint, Title: f.Title, Severity: f.Severity, Category: f.Category, CWE: f.CWE, FilePath: f.FilePath, StartLine: f.StartLine, EndLine: f.EndLine, Description: f.Description, Evidence: f.Evidence, Remediation: f.Remediation, Confidence: f.Confidence, Status: f.Status, Raw: f.Raw})
	}
	return c
}
