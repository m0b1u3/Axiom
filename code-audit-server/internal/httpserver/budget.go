package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"database/sql"
	"errors"
	"net/http"
	"strconv"
)

func (s *Server) taskBudget(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "任务 ID 无效")
		return
	}
	v, e := s.Store.BudgetSnapshot(r.Context(), id)
	if e != nil {
		if errors.Is(e, sql.ErrNoRows) {
			httputil.Error(w, 404, 40400, "任务不存在")
		} else {
			httputil.Error(w, 503, 50300, "任务预算暂不可用，请重试")
		}
		return
	}
	if raw, e := contracts.EncodeJSON(v); e != nil || contracts.ValidateJSON(contracts.KindTaskBudgetSnapshot, raw, nil) != nil {
		httputil.Error(w, 503, 50300, "任务费用快照无效，请重新加载")
		return
	}
	httputil.OK(w, v)
}
func (s *Server) updateTaskBudget(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "任务 ID 无效")
		return
	}
	if task, e := s.Store.Get(r.Context(), id); e == nil && (task.SDKOperation != nil || task.ComponentPlanning != nil) {
		httputil.Error(w, 409, 40900, "此 SDK 操作不支持任务费用上限；费用可单独核对")
		return
	}
	var body struct {
		MaxTotalCostUSD float64 `json:"maxTotalCostUsd"`
		Revision        int64   `json:"revision"`
	}
	if !smallJSON(w, r, &body) {
		return
	}
	if e := s.Store.UpdateTotalBudget(r.Context(), id, body.MaxTotalCostUSD, body.Revision); e != nil {
		s.budgetError(w, e)
		return
	}
	s.taskBudget(w, r, u)
}
func (s *Server) reconcileTaskCost(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, ok := pathID(r, "id")
	index, e := strconv.Atoi(r.PathValue("attempt"))
	if !ok || e != nil || index < 1 {
		httputil.Error(w, 400, 40000, "执行身份无效")
		return
	}
	var body struct {
		EstimatedUSD *float64 `json:"estimatedUsd"`
		Note         string   `json:"note"`
		Revision     int64    `json:"revision"`
	}
	if !smallJSON(w, r, &body) {
		return
	}
	if body.EstimatedUSD == nil {
		httputil.Error(w, 400, 40000, "请提供本次执行的人工估算费用和核对依据")
		return
	}
	if e = s.Store.ReconcileAttemptCost(r.Context(), id, index, *body.EstimatedUSD, body.Note, u.Username, body.Revision); e != nil {
		s.budgetError(w, e)
		return
	}
	s.taskBudget(w, r, u)
}
func (s *Server) budgetError(w http.ResponseWriter, e error) {
	status, code, message := 503, 50300, "预算记录暂不可用，请重试"
	if errors.Is(e, store.ErrBudgetPolicy) || errors.Is(e, store.ErrCostBelowRecorded) || errors.Is(e, store.ErrBudgetBelowRecorded) {
		status, code, message = 400, 40000, e.Error()
	}
	if errors.Is(e, store.ErrBudgetConflict) || errors.Is(e, store.ErrBudgetUnknown) || errors.Is(e, store.ErrBudgetExhausted) {
		status, code, message = 409, 40905, e.Error()
	}
	if errors.Is(e, sql.ErrNoRows) {
		status, code, message = 404, 40400, "任务或执行不存在"
	}
	httputil.Error(w, status, code, message)
}
