package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"errors"
	"net/http"
	"os"
	"strconv"
)

// This Web catalog groups saved evidence by original execution. It never
// promotes candidates to findings or sums them as unique vulnerabilities.
type interimExecutionSummary struct {
	store.InterimExecution
	State       string                        `json:"state"`
	Reason      string                        `json:"reason"`
	Observation *contracts.InterimObservation `json:"observation"`
}

func (s *Server) interimExecutionCatalog(w http.ResponseWriter, r *http.Request, _ *store.User) {
	w.Header().Set("Cache-Control", "no-store")
	q := r.URL.Query()
	p, _, e := catalogPageQuery(q, []string{})
	project, pe := catalogID(q, "projectId")
	status := q.Get("status")
	if e != nil || pe != nil || p.Order != "" || !catalogValue(status, "", "claimed", "scanning", "cancel_requested", "completed", "partial", "failed", "canceled", "timeout") {
		httputil.Error(w, 400, 40000, "中间发现执行目录的分页或筛选无效")
		return
	}
	rows, e := s.Store.ListInterimExecutionsPage(r.Context(), store.InterimExecutionFilter{Query: q.Get("q"), Status: status, ProjectID: project}, p)
	if e != nil {
		httputil.Error(w, 500, 50000, "中间发现执行目录读取失败，请重试；历史报告保持可读")
		return
	}
	out := store.CatalogPage[interimExecutionSummary]{Items: []interimExecutionSummary{}, Total: rows.Total, Page: rows.Page, PageSize: rows.PageSize}
	for _, v := range rows.Items {
		if r.Context().Err() != nil {
			return
		}
		row := interimExecutionSummary{InterimExecution: v, State: "unavailable", Reason: "尚无可展示的中间快照；不代表没有漏洞。"}
		root, err := os.OpenRoot(v.OutputPath)
		if err == nil {
			execution := contracts.ExecutionRef{TaskID: contracts.TaskID(strconv.FormatInt(v.TaskID, 10)), Attempt: int64(v.Attempt)}
			snapshot, readErr := checkedInterim(root, execution, v.SourceContentHash)
			root.Close()
			if readErr == nil {
				row.State = string(snapshot.State)
				row.Reason = "候选可能重复或被后续推翻，尚未最终汇总或独立验证。"
				row.Observation = &contracts.InterimObservation{SchemaVersion: 1, Execution: execution, SourceContentHash: snapshot.SourceContentHash, SDKVersion: snapshot.SDKVersion, Revision: snapshot.Revision, ObservedAt: snapshot.ObservedAt, State: contracts.InterimObservationState(snapshot.State), DocumentCount: int64(len(snapshot.Documents)), CandidateRecords: int64(len(snapshot.Candidates))}
			} else if !errors.Is(readErr, os.ErrNotExist) {
				row.State = "rejected"
				row.Reason = "索引身份或完整性校验失败；请查看原执行的证据诊断。"
			}
		} else if !errors.Is(err, os.ErrNotExist) {
			row.State = "rejected"
			row.Reason = "中间证据目录不可读取；请查看原执行的证据诊断。"
		}
		out.Items = append(out.Items, row)
	}
	httputil.OK(w, out)
}
