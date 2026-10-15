package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts/control"
	"errors"
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
	if strings.TrimSpace(v.AgentID) == "" || len(v.AgentID) > 128 || v.Slots < 1 || v.Slots > 64 {
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
	httputil.OK(w, map[string]any{"settings": v, "canEdit": s.admin(u), "history": history})
}
func (s *Server) saveQueueSettings(w http.ResponseWriter, r *http.Request, u *store.User) {
	if !s.admin(u) {
		httputil.Error(w, 403, 40300, "only the system administrator can change queue limits")
		return
	}
	if !s.pgOnly(w) {
		return
	}
	var v store.QueueSettings
	if !decodeAgent(w, r, &v) {
		return
	}
	if !v.Valid() || v.Revision < 1 {
		httputil.Error(w, 400, 40000, "limits must be integers between 1 and 64 with a valid revision")
		return
	}
	result, e := s.Store.SaveQueueSettings(r.Context(), v, u.Username)
	if errors.Is(e, store.ErrSettingsConflict) {
		httputil.Error(w, 409, 40904, "配置已被其他会话修改，请重新加载后保存")
		return
	}
	if e != nil {
		httputil.Error(w, 503, 50300, "queue settings save failed")
		return
	}
	httputil.OK(w, result)
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
