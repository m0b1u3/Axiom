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

func validObservationAgent(v control.ObservationClaimRequest) bool {
	raw, e := contracts.EncodeJSON(v.ObservationCapabilities)
	return (v.SDKDeepResumeVersion == 0 || v.SDKDeepResumeVersion == 1) && (v.SDKConcurrencyVersion == 0 || v.SDKConcurrencyVersion == 1) && (v.SDKParametersVersion == 0 || v.SDKParametersVersion == 1) && e == nil && contracts.ValidateJSON(contracts.KindObservationCapabilities, raw, nil) == nil && validInputAgent(v.AgentID, v.InputCapabilities) && validResultCapabilities(v.ResultCapabilities)
}
func (s *Server) reportObservationAgent(w http.ResponseWriter, r *http.Request) {
	var v control.ObservationPresence
	if !decodeAgent(w, r, &v) {
		return
	}
	if !validObservationAgent(v.ObservationClaimRequest) || !control.ValidDispatchPresence(v.DispatchMode, v.Slots) {
		executionError(w, 400, "invalid_input", "invalid execution observation capabilities")
		return
	}
	if e := s.Store.ReportAgentWithDispatch(r.Context(), v.AgentID, v.DispatchMode, v.Slots, v.InputCapabilities, v.ResultCapabilities, v.ObservationCapabilities); e != nil {
		executionError(w, 503, "provider_unavailable", "agent presence unavailable")
		return
	}
	httputil.OK(w, map[string]bool{"ok": true})
}
func (s *Server) claimObservationExecution(w http.ResponseWriter, r *http.Request) {
	var v control.ObservationClaimRequest
	if !decodeAgent(w, r, &v) {
		return
	}
	if !validObservationAgent(v) {
		executionError(w, 400, "invalid_input", "invalid execution observation capabilities")
		return
	}
	limits := s.storageClaimLimits(r.Context())
	limits.SDKDeepResumeCapable = v.SDKDeepResumeVersion == 1
	limits.SDKIdentityCapable = v.SDKIdentityVersion == 1
	limits.SDKParametersCapable = v.SDKParametersVersion == 1
	limits.SDKConcurrencyCapable = v.SDKConcurrencyVersion == 1
	t, e := s.Store.ClaimExecutionWithObservations(r.Context(), v.AgentID, limits, v.InputCapabilities, v.ResultCapabilities, v.ObservationCapabilities)
	if e != nil {
		executionError(w, 503, "provider_unavailable", "queue temporarily unavailable")
		return
	}
	if t == nil {
		httputil.OK(w, control.ObservationLease{})
		return
	}
	seconds := int(s.QueueLimits.Lease / time.Second)
	lease, e := s.inputLease(r, t, v.AgentID, seconds)
	if e != nil {
		s.rejectInputLease(w, r, t, v.AgentID)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	out := control.ObservationLease{InputContractVersion: t.InputContractVersion, ContractLease: control.ContractLease{InputLease: lease, ResultContractVersion: t.ResultContractVersion}, ObservationContractVersion: t.ObservationContractVersion}
	if t.ScanInput != nil {
		out.ScanInput = &contracts.ScanInputAssignment{SchemaVersion: contracts.ScanInputVersion, Execution: lease.Assignment.Execution, Input: *t.ScanInput, ParentState: contracts.Optional[contracts.SDKParentState]{Present: t.ParentState != nil}}
		if t.ParentState != nil {
			out.ScanInput.ParentState = contracts.Some(*t.ParentState)
		}
	}
	if t.GitTarget != nil {
		out.GitDiff = &contracts.GitDiffAssignment{SchemaVersion: 1, Execution: lease.Assignment.Execution, SourceSnapshotID: lease.Assignment.Source.SnapshotID, SourceContentHash: lease.Assignment.Source.ContentHash, Target: *t.GitTarget, EvidenceRevision: "head"}
	}
	if t.SDKIdentity != nil {
		out.SDKIdentity = &contracts.SDKTaskIdentity{SchemaVersion: 1, Execution: lease.Assignment.Execution, Identity: *t.SDKIdentity}
	}
	if t.SDKParameters != nil {
		out.SDKParametersVersion = 1
		out.SDKParameters = &contracts.SDKParametersAssignment{SchemaVersion: 1, Execution: lease.Assignment.Execution, Parameters: *t.SDKParameters}
	}
	out.DeepResume = t.DeepResume
	if v.SDKDeepResumeVersion == 1 && t.ScanMode == "deep" && t.DeepResume == nil {
		out.DeepResumeContext = &contracts.DeepResumeContext{SchemaVersion: 1, Execution: lease.Assignment.Execution, AgentID: v.AgentID, ConfigurationHash: store.DeepConfigurationHash(t)}
	}
	out.ComponentScan = t.ComponentScan
	out.ComponentPlanning = t.ComponentPlanning
	out.SDKOperation = t.SDKOperation
	httputil.OK(w, out)
}
func (s *Server) heartbeatObservationExecution(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, contracts.MaxProgressBytes))
	if e != nil {
		executionError(w, 413, "invalid_input", "heartbeat exceeds progress byte limit")
		return
	}
	var header struct {
		SchemaVersion int `json:"schemaVersion"`
	}
	if json.Unmarshal(raw, &header) == nil && header.SchemaVersion != 1 {
		executionError(w, 400, "protocol_version_unsupported", "unsupported heartbeat contract version")
		return
	}
	v, e := contracts.DecodeExecutionHeartbeat(raw, nil)
	if e != nil {
		executionError(w, 400, "invalid_input", "progress contract or execution identity is invalid")
		return
	}
	out, e := s.Store.HeartbeatContractExecution(r.Context(), v, s.QueueLimits.Lease)
	if e != nil {
		switch {
		case errors.Is(e, store.ErrLeaseLost):
			executionError(w, 409, "lease_expired", "execution lease lost")
		case errors.Is(e, store.ErrInvalidProgress):
			executionError(w, 400, "invalid_input", "progress format differs from this execution")
		default:
			executionError(w, 503, "provider_unavailable", "heartbeat unavailable")
		}
		return
	}
	httputil.OK(w, out)
}
