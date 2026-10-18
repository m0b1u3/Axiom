package httpserver

import (
	"axiom/code-audit-server/internal/evidence"
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"strconv"
	"strings"
)

func (s *Server) findingEvidenceGraph(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	attempt, e := strconv.Atoi(r.PathValue("attempt"))
	wanted := r.URL.Query().Get("fingerprint")
	if !ok || e != nil || attempt < 1 || wanted == "" || len(wanted) > 512 {
		httputil.Error(w, 400, 40000, "证据图谱需要明确的任务、执行批次和漏洞指纹")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	archive, e := s.Store.AttemptResult(r.Context(), id, attempt)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		var invalid *contracts.ValidationError
		if errors.As(e, &invalid) {
			httputil.Error(w, 409, 40900, "归档证据合同或执行身份不匹配；原始产物仍保留")
		} else {
			httputil.Error(w, 503, 50300, "归档证据暂时无法读取，请重试")
		}
		return
	}
	if e != nil || !archive.Available {
		httputil.Error(w, 404, 40400, "本次执行尚无可读取的归档证据；原任务仍可查看")
		return
	}
	var finding *contracts.Finding
	for _, row := range archive.Findings {
		if row.Fingerprint != wanted {
			continue
		}
		if finding != nil {
			httputil.Error(w, 409, 40900, "归档包含重复漏洞指纹，无法唯一定位证据链")
			return
		}
		var value contracts.Finding
		if len(row.Normalized) > 0 {
			if json.Unmarshal(row.Normalized, &value) != nil {
				httputil.Error(w, 409, 40900, "归一化证据不可读取；原始报告仍保留")
				return
			}
		} else {
			value = contracts.Finding{Fingerprint: row.Fingerprint, Title: row.Title, Severity: contracts.Severity(row.Severity), Description: row.Description, Raw: contracts.Some(contracts.JSONValue(row.Raw))}
			if row.Evidence != "" {
				value.Evidence = contracts.Some(row.Evidence)
			}
		}
		finding = &value
	}
	if finding == nil {
		httputil.Error(w, 404, 40400, "该漏洞不属于指定执行批次，未混用其他扫描结果")
		return
	}
	task, e := s.Store.Get(r.Context(), id)
	if e != nil {
		httputil.Error(w, 404, 40400, "扫描来源不可读取")
		return
	}
	ref := contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: int64(attempt)}
	if task.GitTarget != nil {
		raw := finding.Extensions.Value["axiomGitTarget"]
		binding, err := contracts.DecodeGitDiffAssignment(raw, &ref)
		if err != nil || binding.SourceSnapshotID != contracts.TaskID(fmt.Sprint(task.SourceSnapshotID)) || binding.SourceContentHash != contracts.Digest(task.SourceContentHash) || binding.Target != *task.GitTarget {
			httputil.Error(w, 409, 40900, "Git 证据版本与冻结执行不匹配；原始报告仍可读取")
			return
		}
	}
	graph, e := evidence.Project(r.Context(), *finding, ref, task.SourceContentHash, archive.ArchiveSHA256, task.SourcePath)
	if e != nil {
		httputil.Error(w, 409, 40900, "证据合同、执行身份或原始引用不匹配；完整历史报告仍可读取")
		return
	}
	if task.GitTarget != nil {
		for i := range graph.Nodes {
			n := &graph.Nodes[i]
			if n.Kind == "code_evidence" && n.Location != nil {
				if strings.EqualFold(strings.Split(string(n.Location.Path), "/")[0], ".git") {
					n.State = "inferred"
					n.SourceCheck = "unavailable"
					n.SourceFileSHA256 = nil
					n.StateReason = "Git 元数据不属于提交的源码，未作为 HEAD 代码观察。"
				} else {
					n.StateReason = "对照冻结 HEAD " + task.GitTarget.HeadCommit + "：" + n.StateReason
				}
			}
		}
	}
	httputil.OK(w, graph)
}
