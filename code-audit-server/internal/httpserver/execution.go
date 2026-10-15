package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts/control"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

func (s *Server) agentOnly(next http.HandlerFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		token := strings.TrimPrefix(r.Header.Get("Authorization"), "Bearer ")
		if s.AgentToken == "" || subtle.ConstantTimeCompare([]byte(token), []byte(s.AgentToken)) != 1 {
			httputil.Error(w, 401, 40101, "invalid agent credentials")
			return
		}
		next(w, r)
	}
}
func decodeAgent(w http.ResponseWriter, r *http.Request, v any) bool {
	r.Body = http.MaxBytesReader(w, r.Body, 32<<20)
	d := json.NewDecoder(r.Body)
	d.DisallowUnknownFields()
	if d.Decode(v) != nil || d.Decode(new(any)) != io.EOF {
		httputil.Error(w, 400, 40000, "invalid execution payload")
		return false
	}
	return true
}
func (s *Server) claimExecution(w http.ResponseWriter, r *http.Request) {
	var body control.ClaimRequest
	if !decodeAgent(w, r, &body) {
		return
	}
	if len(body.AgentID) == 0 || len(body.AgentID) > 128 {
		httputil.Error(w, 400, 40000, "invalid agent id")
		return
	}
	task, err := s.Store.ClaimExecution(r.Context(), body.AgentID, s.storageClaimLimits(r.Context()))
	if err != nil {
		httputil.Error(w, 503, 50300, "queue temporarily unavailable")
		return
	}
	if task == nil {
		httputil.OK(w, control.LegacyLease{})
		return
	}
	lease, err := s.legacyLease(r, task, int(s.QueueLimits.Lease/time.Second))
	if err != nil {
		s.rejectInputLease(w, r, task, body.AgentID)
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, lease)
}
func (s *Server) heartbeatExecution(w http.ResponseWriter, r *http.Request) {
	var body control.LegacyHeartbeat
	if !decodeAgent(w, r, &body) {
		return
	}
	if len(body.ProgressJSON) > 1<<20 {
		httputil.Error(w, 400, 40000, "progress payload too large")
		return
	}
	status, e := s.Store.HeartbeatExecution(r.Context(), legacyLeaseRef(body.LegacyLeaseRef), s.QueueLimits.Lease, body.Stage, body.ProgressJSON)
	if e != nil {
		if errors.Is(e, store.ErrLeaseLost) {
			httputil.Error(w, 409, 40901, "execution lease lost")
		} else {
			httputil.Error(w, 503, 50300, "heartbeat unavailable")
		}
		return
	}
	httputil.OK(w, control.HeartbeatResult{Status: status})
}
func (s *Server) completeExecution(w http.ResponseWriter, r *http.Request) {
	var body control.LegacyCompletion
	if !decodeAgent(w, r, &body) {
		return
	}
	if !store.IsTerminalStatus(body.Status) {
		httputil.Error(w, 400, 40000, "invalid completion status")
		return
	}
	if e := s.Store.CompleteExecution(r.Context(), legacyCompletion(body)); e != nil {
		if errors.Is(e, store.ErrLeaseLost) {
			httputil.Error(w, 409, 40901, "execution lease lost")
		} else if errors.Is(e, store.ErrCompletionConflict) {
			httputil.Error(w, 409, 40902, "completion_conflict")
		} else if errors.Is(e, store.ErrInvalidResult) {
			executionError(w, 400, "invalid_result", "task requires the negotiated result completion contract")
		} else {
			httputil.Error(w, 503, 50300, "completion unavailable")
		}
		return
	}
	httputil.OK(w, map[string]bool{"ok": true})
}
func (s *Server) scanAttempts(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "invalid scan id")
		return
	}
	if _, e := s.Store.Get(r.Context(), id); e != nil {
		httputil.Error(w, 404, 40400, "scan not found")
		return
	}
	items, e := s.Store.ListAttempts(r.Context(), id)
	if e != nil {
		httputil.Error(w, 500, 50000, "attempts unavailable")
		return
	}
	httputil.OK(w, items)
}
func (s *Server) logOutputPath(r *http.Request, task *store.Task) (string, error) {
	v := r.URL.Query().Get("attempt")
	if v == "" {
		return task.OutputPath, nil
	}
	n, e := strconv.Atoi(v)
	if e != nil || n < 1 {
		return "", errors.New("invalid attempt")
	}
	a, e := s.Store.GetAttempt(r.Context(), task.ID, n)
	if e != nil {
		return "", e
	}
	return a.OutputPath, nil
}
