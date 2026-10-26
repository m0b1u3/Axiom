package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"errors"
	"fmt"
	"io"
	"net/http"
)

func (s *Server) deepResumeSnapshot(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "invalid scan id")
		return
	}
	v, e := s.Store.DeepResumeSnapshot(r.Context(), id)
	if e != nil || v.Execution.Attempt < 1 {
		httputil.Error(w, 404, 40400, "此历史执行没有 Deep 续扫入口，原报告仍可读取")
		return
	}
	raw, e := contracts.EncodeJSON(v)
	if e != nil || contracts.ValidateJSON(contracts.KindDeepResumeSnapshot, raw, nil) != nil {
		httputil.Error(w, 503, 50300, "Deep 恢复状态校验未通过")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, v)
}
func (s *Server) deepResumeCheck(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "invalid scan id")
		return
	}
	defer r.Body.Close()
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 16384))
	if e != nil {
		httputil.Error(w, 400, 40000, "检查请求无效")
		return
	}
	v, e := contracts.DecodeExecutionRef(raw, nil)
	if e != nil || v.TaskID != contracts.TaskID(fmt.Sprint(id)) {
		httputil.Error(w, 400, 40000, "检查执行身份无效")
		return
	}
	_, e = s.Store.RequestDeepResumeCheck(r.Context(), id, v)
	if e != nil {
		httputil.Error(w, 409, 40910, "Deep 检查条件已变化；请刷新后核验，不会启动模型")
		return
	}
	s.deepResumeSnapshot(w, r, nil)
}
func (s *Server) resumeDeep(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "invalid scan id")
		return
	}
	defer r.Body.Close()
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 16384))
	if e != nil {
		httputil.Error(w, 400, 40000, "续扫请求无效")
		return
	}
	v, e := contracts.DecodeDeepResumeAction(raw, nil)
	if e != nil || v.Execution.TaskID != contracts.TaskID(fmt.Sprint(id)) {
		httputil.Error(w, 400, 40000, "续扫须绑定原执行、检查证明并确认新增费用")
		return
	}
	e = s.Store.ResumeDeepScan(r.Context(), id, v)
	if e != nil {
		if errors.Is(e, store.ErrBudgetUnknown) || errors.Is(e, store.ErrBudgetExhausted) {
			s.budgetError(w, e)
			return
		}
		httputil.Error(w, 409, 40910, "Deep 续扫条件、费用或检查证明已变化；未自动重新扫描，请重新核验")
		return
	}
	s.respondTask(w, r, id)
}
func (s *Server) claimDeepResumeCheck(w http.ResponseWriter, r *http.Request) {
	var v struct {
		AgentID string `json:"agentId"`
		Version int    `json:"version"`
	}
	if !decodeAgent(w, r, &v) {
		return
	}
	if v.Version != 1 {
		executionError(w, 400, "invalid_input", "unsupported Deep inspection version")
		return
	}
	claim, e := s.Store.ClaimDeepResumeCheck(r.Context(), v.AgentID)
	if e != nil {
		executionError(w, 503, "provider_unavailable", "Deep inspection queue unavailable")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, claim)
}
func (s *Server) reportDeepResumeCheck(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 128*1024))
	if e != nil {
		executionError(w, 400, "invalid_input", "invalid Deep inspection")
		return
	}
	v, e := contracts.DecodeDeepResumeProbeReport(raw, nil)
	if e != nil || s.Store.ReportDeepResumeCheck(r.Context(), v) != nil {
		executionError(w, 409, "lease_expired", "Deep inspection lease or identity changed")
		return
	}
	httputil.OK(w, map[string]bool{"ok": true})
}
