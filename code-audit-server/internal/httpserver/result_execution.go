package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/contracts/control"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"time"
)

func validResultCapabilities(c contracts.ResultCapabilities) bool {
	raw, e := json.Marshal(c)
	return e == nil && contracts.ValidateJSON(contracts.KindResultCapabilities, raw, nil) == nil
}

func (s *Server) reportContractAgent(w http.ResponseWriter, r *http.Request) {
	var v control.ContractPresence
	if !decodeAgent(w, r, &v) {
		return
	}
	if !validInputAgent(v.AgentID, v.InputCapabilities) || !validResultCapabilities(v.ResultCapabilities) || v.Slots < 1 || v.Slots > 64 {
		executionError(w, 400, "invalid_input", "invalid execution capabilities")
		return
	}
	if e := s.Store.ReportAgentWithContracts(r.Context(), v.AgentID, v.Slots, v.InputCapabilities, v.ResultCapabilities); e != nil {
		executionError(w, 503, "provider_unavailable", "agent presence unavailable")
		return
	}
	httputil.OK(w, map[string]bool{"ok": true})
}

func (s *Server) claimContractExecution(w http.ResponseWriter, r *http.Request) {
	var v control.ContractClaimRequest
	if !decodeAgent(w, r, &v) {
		return
	}
	if !validInputAgent(v.AgentID, v.InputCapabilities) || !validResultCapabilities(v.ResultCapabilities) {
		executionError(w, 400, "invalid_input", "invalid execution capabilities")
		return
	}
	t, e := s.Store.ClaimExecutionWithContracts(r.Context(), v.AgentID, s.storageClaimLimits(r.Context()), v.InputCapabilities, v.ResultCapabilities)
	if e != nil {
		executionError(w, 503, "provider_unavailable", "queue temporarily unavailable")
		return
	}
	if t == nil {
		httputil.OK(w, control.ContractLease{})
		return
	}
	seconds := int(s.QueueLimits.Lease / time.Second)
	lease, e := s.inputLease(r, t, v.AgentID, seconds)
	if e != nil {
		s.rejectInputLease(w, r, t, v.AgentID)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, control.ContractLease{InputLease: lease, ResultContractVersion: t.ResultContractVersion})
}

func (s *Server) completeContractExecution(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, contracts.MaxDocumentBytes))
	if e != nil {
		executionError(w, 413, "invalid_result", "completion exceeds document byte limit")
		return
	}
	var header struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if json.Unmarshal(raw, &header) == nil && header.SchemaVersion != 1 {
		executionError(w, 400, "protocol_version_unsupported", "unsupported completion contract version")
		return
	}
	v, e := contracts.DecodeExecutionCompletion(raw, nil)
	if e != nil {
		executionError(w, 400, "invalid_result", "completion contract or execution identity is invalid")
		return
	}
	if e = s.Store.CompleteContractExecution(r.Context(), v); e != nil {
		switch {
		case errors.Is(e, store.ErrLeaseLost):
			executionError(w, 409, "lease_expired", "execution lease lost")
		case errors.Is(e, store.ErrCompletionConflict):
			executionError(w, 409, "completion_conflict", "completion differs from the archived execution")
		case errors.Is(e, store.ErrInvalidResult):
			executionError(w, 400, "invalid_result", "result contract or frozen model does not match this execution")
		default:
			executionError(w, 503, "provider_unavailable", "completion temporarily unavailable")
		}
		return
	}
	httputil.OK(w, map[string]bool{"ok": true})
}

func executionError(w http.ResponseWriter, status int, code contracts.ErrorCode, message string) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.Header().Set("Cache-Control", "no-store")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(map[string]any{"code": status * 100, "message": message, "data": map[string]any{"error": contracts.ContractError{Code: code, Origin: contracts.ErrorOriginServer, Message: message, Retryable: status >= 500}}})
}
