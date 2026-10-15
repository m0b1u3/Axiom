package httpserver

import (
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/contracts/control"
	"encoding/json"
	"fmt"
	"net/http"
	"time"
)

func validInputAgent(agent string, c contracts.InputCapabilities) bool {
	if agent == "" || len(agent) > 128 {
		return false
	}
	raw, err := json.Marshal(c)
	return err == nil && contracts.ValidateJSON(contracts.KindInputCapabilities, raw, nil) == nil
}

func (s *Server) reportInputAgent(w http.ResponseWriter, r *http.Request) {
	var v control.InputPresence
	if !decodeAgent(w, r, &v) {
		return
	}
	if !validInputAgent(v.AgentID, v.Capabilities) || v.Slots < 1 || v.Slots > 64 {
		httputil.Error(w, 400, 40000, "invalid input capabilities or agent presence")
		return
	}
	if err := s.Store.ReportAgentWithInput(r.Context(), v.AgentID, v.Slots, v.Capabilities); err != nil {
		httputil.Error(w, 503, 50300, "agent presence unavailable")
		return
	}
	httputil.OK(w, map[string]bool{"ok": true})
}

func (s *Server) claimInputExecution(w http.ResponseWriter, r *http.Request) {
	var v control.InputClaimRequest
	if !decodeAgent(w, r, &v) {
		return
	}
	if !validInputAgent(v.AgentID, v.Capabilities) {
		httputil.Error(w, 400, 40000, "invalid input capabilities")
		return
	}
	task, err := s.Store.ClaimExecutionWithInput(r.Context(), v.AgentID, s.storageClaimLimits(r.Context()), v.Capabilities)
	if err != nil {
		httputil.Error(w, 503, 50300, "queue temporarily unavailable")
		return
	}
	if task == nil {
		httputil.OK(w, control.InputLease{})
		return
	}
	leaseSeconds := int(s.QueueLimits.Lease / time.Second)
	if task.InputContractVersion == 0 {
		legacy, err := s.legacyLease(r, task, leaseSeconds)
		if err != nil {
			s.rejectInputLease(w, r, task, v.AgentID)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		httputil.OK(w, control.InputLease{Legacy: legacy})
		return
	}
	credential, err := s.taskCredential(r, task)
	if err != nil {
		s.rejectInputLease(w, r, task, v.AgentID)
		return
	}
	a, err := assignmentFromTask(task, v.AgentID, leaseSeconds, credential)
	if err != nil {
		s.rejectInputLease(w, r, task, v.AgentID)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, control.InputLease{Assignment: a, SourcePath: task.SourcePath, OutputPath: task.OutputPath, LeaseSeconds: leaseSeconds})
}

func (s *Server) inputLease(r *http.Request, t *store.Task, agent string, seconds int) (control.InputLease, error) {
	v := control.InputLease{LeaseSeconds: seconds}
	var e error
	if t.InputContractVersion == 0 {
		v.Legacy, e = s.legacyLease(r, t, seconds)
		return v, e
	}
	credential, e := s.taskCredential(r, t)
	if e != nil {
		return v, e
	}
	v.Assignment, e = assignmentFromTask(t, agent, seconds, credential)
	v.SourcePath, v.OutputPath = t.SourcePath, t.OutputPath
	return v, e
}

func (s *Server) rejectInputLease(w http.ResponseWriter, r *http.Request, t *store.Task, agent string) {
	if t.ResultContractVersion == 1 {
		_ = s.Store.CompleteContractExecution(r.Context(), contracts.ExecutionCompletion{SchemaVersion: 1, Execution: contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(t.ID)), Attempt: int64(t.Attempt)}, AgentID: contracts.Identifier(agent), ExecutionStatus: contracts.TerminalStatusFailed, Process: contracts.ProcessExit{ResultCommitted: false}, Error: contracts.Some(contracts.ContractError{Code: contracts.ErrorCodeConfigurationError, Origin: contracts.ErrorOriginServer, Message: "stored scan configuration unavailable", Retryable: false})})
	} else {
		_ = s.Store.CompleteExecution(r.Context(), store.Completion{LeaseRef: store.LeaseRef{TaskID: t.ID, Attempt: t.Attempt, AgentID: agent}, Status: "failed", ErrorCode: "configuration_error", ErrorMessage: "stored scan configuration unavailable"})
	}
	httputil.Error(w, 409, 40903, "stored scan configuration unavailable")
}

// Parameters are immutable. Credentials may rotate only while the destination,
// provider, protocol and credential binding still match the frozen connection.
// A changed/deleted configuration keeps its original encrypted credential.
func (s *Server) taskCredential(r *http.Request, t *store.Task) (string, error) {
	encrypted := t.CredentialSnapshotEncrypted
	if t.ModelSnapshotJSON == "" {
		if t.ModelConfigID == 0 {
			return "", nil
		}
		cfg, err := s.Store.GetModelConfig(r.Context(), t.ModelConfigID)
		if err != nil {
			return "", err
		}
		encrypted = cfg.ApiKeyEncrypted
	} else if t.ModelConfigID > 0 {
		original, err := contracts.DecodeResolvedModel([]byte(t.ModelSnapshotJSON), nil)
		if err != nil {
			return "", err
		}
		if cfg, err := s.Store.GetModelConfig(r.Context(), t.ModelConfigID); err == nil {
			if current, err := resolveModel(createScanRequest{}, cfg); err == nil && current.ProviderID == original.ProviderID && current.ConnectionType == original.ConnectionType && current.BaseURL == original.BaseURL && current.WireAPI == original.WireAPI && current.CredentialEnv == original.CredentialEnv {
				encrypted = cfg.ApiKeyEncrypted
			}
		}
	}
	if encrypted == "" {
		return "", nil
	}
	return cryptokey.Decrypt(encrypted)
}

func (s *Server) legacyLease(r *http.Request, t *store.Task, seconds int) (*control.LegacyLease, error) {
	lease := &control.LegacyLease{Task: legacyTask(t), LeaseSeconds: seconds}
	if t.ModelSnapshotJSON != "" {
		m, err := contracts.DecodeResolvedModel([]byte(t.ModelSnapshotJSON), nil)
		if err != nil {
			return nil, err
		}
		lease.CredentialEnv, lease.WireAPI, lease.DisplayName = m.CredentialEnv, string(m.WireAPI), string(m.ProviderID)
	} else {
		cfg := createScanRequest{Provider: t.Provider, Model: t.Model, BaseURL: t.BaseURL}
		var row *store.ModelConfigRow
		if t.ModelConfigID > 0 {
			var err error
			row, err = s.Store.GetModelConfig(r.Context(), t.ModelConfigID)
			if err != nil {
				return nil, err
			}
		}
		m, err := resolveModel(cfg, row)
		if err != nil {
			return nil, err
		}
		lease.CredentialEnv, lease.WireAPI, lease.DisplayName = m.CredentialEnv, string(m.WireAPI), string(m.ProviderID)
	}
	var err error
	lease.Credential, err = s.taskCredential(r, t)
	return lease, err
}
