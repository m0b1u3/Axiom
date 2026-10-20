package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/contracts/control"
	"errors"
	"net/http"
)

func (s *Server) describeExecutions(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, control.ServerDescription{SDKConcurrencyVersion: 1, TaskDispatch: control.DispatchUnbounded, SDKParametersVersion: 1, SDKIdentityVersion: 1, ControlAPIs: control.ControlAPIs(), MatrixHash: control.MatrixHash})
}
func (s *Server) reportAgentRuntime(w http.ResponseWriter, r *http.Request) {
	var v control.RuntimeReport
	if !decodeAgent(w, r, &v) {
		return
	}
	raw, e := contracts.EncodeJSON(v.Runtime)
	if v.AgentID == "" || len(v.AgentID) > 128 || e != nil {
		executionError(w, 400, "invalid_input", "invalid execution node runtime")
		return
	}
	if _, e = contracts.DecodeAgentRuntime(raw, nil); e != nil {
		executionError(w, 400, "invalid_input", "invalid execution node runtime contract")
		return
	}
	if e = s.Store.ReportAgentRuntime(r.Context(), v.AgentID, v.Runtime); e != nil {
		if errors.Is(e, store.ErrAgentNotRegistered) {
			executionError(w, 409, "invalid_input", "execution node must report presence first")
			return
		}
		executionError(w, 503, "provider_unavailable", "execution node runtime unavailable")
		return
	}
	httputil.OK(w, map[string]bool{"ok": true})
}
