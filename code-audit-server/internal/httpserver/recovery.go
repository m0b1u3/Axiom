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

func (s *Server) scanRecovery(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "invalid scan id")
		return
	}
	v, e := s.Store.RecoverySnapshot(r.Context(), id, s.QueueLimits.OutputRoot)
	if e != nil {
		httputil.Error(w, 503, 50300, "扫描恢复状态暂不可用，请刷新；当前任务与历史证据保持保留")
		return
	}
	raw, e := contracts.EncodeJSON(v)
	if e != nil || contracts.ValidateJSON(contracts.KindScanRecoverySnapshot, raw, nil) != nil {
		httputil.Error(w, 503, 50300, "扫描恢复快照校验未通过，不能据此执行恢复")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, v)
}

func (s *Server) recoverScan(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "invalid scan id")
		return
	}
	defer r.Body.Close()
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, contracts.MaxProgressBytes))
	if e != nil {
		httputil.Error(w, 400, 40000, "恢复操作超过容量限制")
		return
	}
	action, e := contracts.DecodeScanRecoveryAction(raw, nil)
	if e != nil || action.Execution.TaskID != contracts.TaskID(fmt.Sprint(id)) {
		httputil.Error(w, 400, 40000, "恢复操作或执行身份无效；此入口不支持断点续扫，重新执行需要明确确认")
		return
	}
	switch action.Action {
	case "restore_result":
		e = s.Store.RestoreCommittedResult(r.Context(), id, action, s.QueueLimits.OutputRoot, u.Username)
	case "interrupt_expired":
		e = s.Store.MarkExecutionInterrupted(r.Context(), id, action)
	case "restart":
		var accepted bool
		accepted, e = s.Store.RetryExecution(r.Context(), id, int(action.Execution.Attempt), string(action.ExpectedStatus))
		if e == nil && !accepted {
			e = store.ErrRecoveryConflict
		}
	}
	if e != nil {
		switch {
		case errors.Is(e, store.ErrBudgetUnknown), errors.Is(e, store.ErrBudgetExhausted), errors.Is(e, store.ErrBudgetPolicy):
			s.budgetError(w, e)
		case errors.Is(e, store.ErrStorageProtected):
			s.storageError(w, e)
		case errors.Is(e, store.ErrRecoveryConflict), errors.Is(e, store.ErrRecoveryUnavailable), errors.Is(e, store.ErrInvalidResult), errors.Is(e, store.ErrLeaseLost), errors.Is(e, store.ErrCompletionConflict):
			httputil.Error(w, 409, 40910, "恢复条件或结果校验未通过，请刷新恢复状态；当前记录与证据已保留，不会自动重新执行")
		default:
			httputil.Error(w, 503, 50300, "扫描恢复操作暂未完成，请刷新核对状态；不会自动重新执行")
		}
		return
	}
	s.respondTask(w, r, id)
}
