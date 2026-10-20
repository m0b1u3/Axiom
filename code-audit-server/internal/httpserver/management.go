package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/contracts/control"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) admin(u *store.User) bool { return u.Role == "admin" && u.Active }
func (s *Server) pgOnly(w http.ResponseWriter) bool {
	if !s.Store.IsPostgres() {
		httputil.Error(w, 503, 50300, "task management requires PostgreSQL")
		return false
	}
	return true
}
func (s *Server) reportAgent(w http.ResponseWriter, r *http.Request) {
	var v control.Presence
	if !decodeAgent(w, r, &v) {
		return
	}
	if strings.TrimSpace(v.AgentID) == "" || len(v.AgentID) > 128 || v.Slots < 1 || int64(v.Slots) > contracts.SDKMaxSafeInteger {
		httputil.Error(w, 400, 40000, "invalid agent presence")
		return
	}
	if e := s.Store.ReportAgent(r.Context(), v.AgentID, v.Slots); e != nil {
		httputil.Error(w, 503, 50300, "agent presence unavailable")
		return
	}
	httputil.OK(w, map[string]bool{"ok": true})
}
func (s *Server) queueOverview(w http.ResponseWriter, r *http.Request, u *store.User) {
	if !s.pgOnly(w) {
		return
	}
	var id int64
	if raw := r.URL.Query().Get("taskId"); raw != "" {
		var e error
		id, e = strconv.ParseInt(raw, 10, 64)
		if e != nil || id < 1 {
			httputil.Error(w, 400, 40000, "invalid task id")
			return
		}
	}
	v, e := s.Store.QueueOverview(r.Context(), s.storageClaimLimits(r.Context()), id)
	if e != nil {
		httputil.Error(w, 503, 50300, "queue overview unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, v)
}
func (s *Server) queueSettings(w http.ResponseWriter, r *http.Request, u *store.User) {
	if !s.pgOnly(w) {
		return
	}
	v, e := s.Store.QueueSettings(r.Context(), s.QueueLimits)
	if e != nil {
		httputil.Error(w, 503, 50300, "queue settings unavailable")
		return
	}
	history, e := s.Store.QueueSettingHistory(r.Context())
	if e != nil {
		httputil.Error(w, 503, 50300, "settings history unavailable")
		return
	}
	httputil.OK(w, map[string]any{"dispatchMode": control.DispatchUnbounded, "settings": v, "canEdit": false, "history": history})
}
func (s *Server) saveQueueSettings(w http.ResponseWriter, r *http.Request, u *store.User) {
	if !s.admin(u) {
		httputil.Error(w, 403, 40300, "only the system administrator can change queue limits")
		return
	}
	if !s.pgOnly(w) {
		return
	}
	httputil.Error(w, http.StatusGone, 41000, "平台并发配额已停用；旧配置仅保留历史读取，不再参与任务调度。")
}
func (s *Server) attemptResult(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, ok := pathID(r, "id")
	n, e := strconv.Atoi(r.PathValue("attempt"))
	if !ok || e != nil || n < 1 {
		httputil.Error(w, 400, 40000, "invalid execution id")
		return
	}
	result, e := s.Store.AttemptResult(r.Context(), id, n)
	if e != nil {
		httputil.Error(w, 404, 40400, "execution result unavailable")
		return
	}
	httputil.OK(w, result)
}
