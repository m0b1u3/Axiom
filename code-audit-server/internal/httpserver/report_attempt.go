package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"net/http"
	"strconv"
)

// The default remains the mutable current projection. An explicit attempt uses
// its immutable archive, including that round's selected component scope.
func (s *Server) reportInput(w http.ResponseWriter, r *http.Request, id int64) (*store.Task, []store.Finding, bool) {
	task, err := s.Store.Get(r.Context(), id)
	if err != nil {
		httputil.Error(w, 404, 40002, "scan not found")
		return nil, nil, false
	}
	if (task.SDKOperation != nil && (task.InterimReviewGroupID == 0 || task.FindingCount == 0)) || task.ComponentPlanning != nil {
		httputil.Error(w, 409, 40900, "SDK 独立操作没有扫描报告，请查看其原始操作报告")
		return nil, nil, false
	}
	values, explicit := r.URL.Query()["attempt"]
	if !explicit {
		items, err := s.Store.ListFindings(r.Context(), id)
		if err != nil {
			httputil.Error(w, 500, 50000, "report findings could not be read")
			return nil, nil, false
		}
		return task, items, true
	}
	if len(values) != 1 {
		httputil.Error(w, 400, 40000, "invalid report attempt")
		return nil, nil, false
	}
	attempt, err := strconv.Atoi(values[0])
	if err != nil || attempt < 1 || strconv.Itoa(attempt) != values[0] {
		httputil.Error(w, 400, 40000, "invalid report attempt")
		return nil, nil, false
	}
	a, err := s.Store.GetAttempt(r.Context(), id, attempt)
	if err != nil {
		httputil.Error(w, 404, 40002, "report attempt not found")
		return nil, nil, false
	}
	archive, err := s.Store.AttemptResult(r.Context(), id, attempt)
	if err != nil || !archive.Available {
		httputil.Error(w, 409, 40900, "所选执行没有可读取的归档报告；不会回退到当前执行")
		return nil, nil, false
	}
	task.Attempt, task.Status, task.StartedAt, task.FinishedAt = a.Attempt, a.Status, a.StartedAt, a.FinishedAt
	task.FindingCount, task.ErrorCode, task.ErrorMessage = len(archive.Findings), a.ErrorCode, a.ErrorMessage
	if task.ComponentScan != nil {
		history, err := s.Store.ComponentAttempts(r.Context(), id)
		if err != nil {
			httputil.Error(w, 500, 50000, "component report scope could not be read")
			return nil, nil, false
		}
		task.ComponentScan = nil
		for _, h := range history {
			if int(h.Request.Execution.Attempt) == attempt {
				r := h.Request
				task.ComponentScan = &r
				break
			}
		}
		if task.ComponentScan == nil {
			httputil.Error(w, 409, 40900, "所选执行缺少组件范围证据；不会使用新一轮组件范围")
			return nil, nil, false
		}
	}
	return task, archive.Findings, true
}
