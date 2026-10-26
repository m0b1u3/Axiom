package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

// Exact deduplication is deliberately conservative; similar model prose is not
// evidence that two candidates share a root cause. Manual grouping is explicit.
func interimCandidateKey(c contracts.InterimCandidate) string {
	summary := ""
	if c.Summary != nil {
		summary = *c.Summary
	}
	severity := ""
	if c.ReportedSeverity != nil {
		severity = *c.ReportedSeverity
	}
	evidence := append([]string{}, c.Evidence...)
	sort.Strings(evidence)
	raw, _ := json.Marshal([]any{strings.TrimSpace(c.Title), strings.TrimSpace(summary), severity, evidence, c.Truncated, c.OmittedEvidence})
	return contracts.InterimReviewDigest(raw)
}
func checkedInterimReviewDocument(root *os.Root, ref contracts.InterimDocumentRef, execution contracts.ExecutionRef, source string) ([]byte, error) {
	raw, e := interimBytes(root, string(ref.Artifact.RelativePath), 4*1024*1024)
	if e != nil {
		return nil, e
	}
	if int64(len(raw)) != int64(ref.Artifact.SizeBytes) || contracts.InterimReviewDigest(raw) != string(ref.Artifact.SHA256) {
		return nil, fmt.Errorf("证据副本哈希已变化")
	}
	doc, e := contracts.DecodeInterimDocument(raw, &execution)
	if e != nil || string(doc.SourceContentHash) != source || doc.SourcePath != ref.SourcePath || doc.SourceSHA256 != ref.SourceSHA256 || string(doc.Kind) != string(ref.Kind) || doc.ObservedAt != ref.ObservedAt {
		return nil, fmt.Errorf("证据执行或来源绑定不匹配")
	}
	return raw, nil
}
func (s *Server) freezeInterimReview(r *http.Request, source *store.Task, request contracts.InterimReviewRequest) (contracts.InterimReviewEnvelope, error) {
	input := contracts.InterimReviewInput{SchemaVersion: 1, SourceContentHash: contracts.Digest(source.SourceContentHash), Grouping: contracts.InterimReviewInputGrouping(request.Grouping), GroupingReason: strings.TrimSpace(request.GroupingReason), Members: []contracts.InterimReviewMember{}, Documents: []contracts.InterimReviewEvidence{}}
	snapshots := map[int64]contracts.InterimSnapshot{}
	roots := map[int64]*os.Root{}
	defer func() {
		for _, v := range roots {
			v.Close()
		}
	}()
	seen, documents, keys := map[string]bool{}, map[string]bool{}, map[string]bool{}
	frozenBytes := 0
	for _, selection := range request.Members {
		if selection.Execution.TaskID != contracts.TaskID(fmt.Sprint(source.ID)) {
			return contracts.InterimReviewEnvelope{}, fmt.Errorf("候选不属于所选来源任务")
		}
		a := selection.Execution.Attempt
		if _, ok := snapshots[a]; !ok {
			attempt, e := s.Store.GetAttempt(r.Context(), source.ID, int(a))
			if e != nil || !store.IsTerminalStatus(attempt.Status) {
				return contracts.InterimReviewEnvelope{}, fmt.Errorf("请使用已结束的原执行，避免冻结尚在变化的候选")
			}
			root, e := os.OpenRoot(attempt.OutputPath)
			if e != nil {
				return contracts.InterimReviewEnvelope{}, fmt.Errorf("原执行的已保存证据暂不可读取")
			}
			roots[a] = root
			v, e := checkedInterim(root, selection.Execution, source.SourceContentHash)
			if e != nil {
				return contracts.InterimReviewEnvelope{}, fmt.Errorf("原执行索引未通过身份或完整性检查")
			}
			snapshots[a] = v
		}
		snapshot := snapshots[a]
		var candidate *contracts.InterimCandidate
		for _, c := range snapshot.Candidates {
			if c.ID == selection.CandidateID && c.Artifact == selection.ArtifactSHA256 {
				v := c
				candidate = &v
				break
			}
		}
		if candidate == nil {
			return contracts.InterimReviewEnvelope{}, fmt.Errorf("所选候选版本已变化，请刷新后重新选择")
		}
		memberKey := fmt.Sprint(a, ":", candidate.ID, ":", candidate.Artifact)
		if seen[memberKey] {
			return contracts.InterimReviewEnvelope{}, fmt.Errorf("同一候选版本被重复选择")
		}
		seen[memberKey] = true
		input.Members = append(input.Members, contracts.InterimReviewMember{Execution: selection.Execution, Candidate: *candidate})
		keys[interimCandidateKey(*candidate)] = true
		docKey := fmt.Sprint(a, ":", candidate.Artifact)
		if !documents[docKey] {
			for _, ref := range snapshot.Documents {
				if ref.Artifact.SHA256 != candidate.Artifact {
					continue
				}
				raw, e := checkedInterimReviewDocument(roots[a], ref, selection.Execution, source.SourceContentHash)
				if e != nil {
					return contracts.InterimReviewEnvelope{}, e
				}
				frozenBytes += len(raw)
				if frozenBytes > 8*1024*1024 {
					return contracts.InterimReviewEnvelope{}, fmt.Errorf("分组证据超出单次复核存储容量，请减少选择；原证据未被裁剪")
				}
				input.Documents = append(input.Documents, contracts.InterimReviewEvidence{Execution: selection.Execution, Artifact: ref.Artifact, DocumentJSON: string(raw)})
				documents[docKey] = true
				break
			}
		}
	}
	if request.Grouping == "exact" && len(keys) != 1 {
		return contracts.InterimReviewEnvelope{}, fmt.Errorf("选中的记录不属于同一精确重复组；分别复核，或明确选择手动合并并说明依据")
	}
	if request.Grouping == "manual" && input.GroupingReason == "" {
		return contracts.InterimReviewEnvelope{}, fmt.Errorf("手动合并须说明它们描述同一问题的依据")
	}
	sorted := []string{}
	for k := range keys {
		sorted = append(sorted, k)
	}
	sort.Strings(sorted)
	identity := strings.Join(sorted, "\x00")
	if request.Grouping == "manual" && len(keys) > 1 {
		identity += "\x00manual\x00" + input.GroupingReason
	}
	input.GroupKey = contracts.Digest(contracts.InterimReviewDigest([]byte("interim-review-v1\x00" + source.SourceContentHash + "\x00" + identity)))
	// Stable member/document order makes a lost-response retry byte-identical.
	sort.Slice(input.Members, func(i, j int) bool {
		a, b := input.Members[i], input.Members[j]
		return fmt.Sprint(a.Execution, ":", a.Candidate.ID) < fmt.Sprint(b.Execution, ":", b.Candidate.ID)
	})
	sort.Slice(input.Documents, func(i, j int) bool {
		a, b := input.Documents[i], input.Documents[j]
		return fmt.Sprint(a.Execution, ":", a.Artifact.SHA256) < fmt.Sprint(b.Execution, ":", b.Artifact.SHA256)
	})
	raw, e := contracts.EncodeJSON(input)
	if e != nil || len(raw) > 8*1024*1024 {
		return contracts.InterimReviewEnvelope{}, fmt.Errorf("分组证据超出单次复核输入的存储容量，请减少选择；原证据未被裁剪")
	}
	envelope := contracts.InterimReviewEnvelope{SchemaVersion: 1, InputJSON: string(raw), InputSHA256: contracts.Digest(contracts.InterimReviewDigest(raw))}
	_, e = contracts.ReadInterimReview(envelope, source.SourceContentHash)
	return envelope, e
}
func candidateValidationTask(source *store.Task, originAttempt int, envelope contracts.InterimReviewEnvelope, request string, retry bool) store.Task {
	t := *source
	t.ID = 0
	t.InterimReviewGroupID = 0
	t.ComponentScan = nil
	t.ComponentPlanning = nil
	t.DeepResume = nil
	t.ParentState = nil
	t.ParentExecution = nil
	origin := contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(source.ID)), Attempt: int64(originAttempt)}
	id := "1"
	raw, _ := contracts.EncodeJSON(envelope)
	finding := contracts.JSONObject{"axiomInterimReview": raw}
	t.SDKOperation = &contracts.SDKOperation{SchemaVersion: 1, Execution: contracts.ExecutionRef{TaskID: "1", Attempt: 1}, Kind: "validate", Origin: &origin, FindingID: &id, Finding: &finding, AcknowledgeUnknownCost: true}
	t.SDKOperationOrigin = source.ID
	policy := contracts.ScanPolicy{Mode: "standard", TimeBudget: contracts.TimeBudget{ScanSeconds: int64(source.TimeoutMinutes) * 60}, IncludePaths: []contracts.RelativePath{}, Focus: []contracts.Focus{}, Validation: contracts.ValidationPolicy{Mode: "default"}}
	p, _ := contracts.EncodeJSON(policy)
	t.PolicySnapshotJSON = string(p)
	t.ScanMode = "standard"
	t.ValidationMode = "default"
	t.BusinessContext = ""
	t.IncludePaths = "[]"
	t.ScanFocus = "[]"
	t.Workers = nil
	t.MaxCostUSD = nil
	t.MaxTotalCostUSD = nil
	t.DeepMaxDiscoveryRuns = 0
	t.DeepStopAfterNoNew = 0
	t.ScanInput = &contracts.ScanInput{SchemaVersion: 1, Knowledge: []contracts.KnowledgeDocument{}}
	if t.SDKParameters != nil {
		v := *t.SDKParameters
		v.ComponentInput = contracts.Optional[contracts.ComponentSDKInput]{}
		t.SDKParameters = &v
	}
	t.ScanNo = "SDK-review-" + time.Now().UTC().Format("20060102150405.000000000")
	t.InputContractVersion = contracts.SDKOperationInputVersion
	if t.GitTarget != nil {
		t.InputContractVersion = contracts.GitDiffInputVersion
	}
	t.BudgetAccounting = true
	t.InterimReviewStart = &store.InterimReviewStart{Envelope: envelope, RequestID: request, Retry: retry}
	return t
}
func reviewJSON(w http.ResponseWriter, r *http.Request, out any, limit int64) bool {
	defer r.Body.Close()
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, limit))
	decoder.DisallowUnknownFields()
	var extra any
	if decoder.Decode(out) != nil || decoder.Decode(&extra) != io.EOF {
		httputil.Error(w, 400, 40000, "候选复核请求格式无效或超出输入容量，请重新选择")
		return false
	}
	return true
}
func (s *Server) createInterimReview(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "任务编号无效")
		return
	}
	var body contracts.InterimReviewRequest
	if !reviewJSON(w, r, &body, 64*1024) {
		return
	}
	raw, _ := contracts.EncodeJSON(body)
	if contracts.ValidateJSON(contracts.KindInterimReviewRequest, raw, nil) != nil {
		httputil.Error(w, 400, 40000, "请选择候选版本，并明确确认独立复核的未知费用")
		return
	}
	source, e := s.Store.Get(r.Context(), id)
	if e != nil || source.SDKOperation != nil || source.ComponentPlanning != nil || source.ComponentScan != nil || source.ScanMode != "standard" || source.SourceSnapshotID < 1 {
		httputil.Error(w, 409, 40900, "独立候选复核需要 Standard 执行及冻结源码")
		return
	}
	envelope, e := s.freezeInterimReview(r, source, body)
	if e != nil {
		httputil.Error(w, 409, 40900, e.Error())
		return
	}
	next, e := s.Store.InsertPending(r.Context(), candidateValidationTask(source, int(body.Members[0].Execution.Attempt), envelope, body.RequestID, false))
	if e != nil {
		httputil.Error(w, 409, 40900, "候选复核未创建："+e.Error())
		return
	}
	task, e := s.Store.Get(r.Context(), next)
	if e != nil {
		httputil.Error(w, 503, 50300, "复核已创建，请重读复核历史")
		return
	}
	httputil.OK(w, map[string]any{"taskId": next, "groupId": task.InterimReviewGroupID})
}
func (s *Server) interimReviews(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	attempt, e := strconv.Atoi(r.PathValue("attempt"))
	if !ok || e != nil || attempt < 1 {
		httputil.Error(w, 400, 40000, "原执行编号无效")
		return
	}
	ids, e := s.Store.InterimReviewGroups(r.Context(), id, attempt)
	if e != nil {
		httputil.Error(w, 503, 50300, "候选复核历史暂不可读取")
		return
	}
	httputil.OK(w, ids)
}
func (s *Server) interimReviewView(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "group")
	if !ok {
		httputil.Error(w, 400, 40000, "复核组编号无效")
		return
	}
	v, e := s.Store.InterimReview(r.Context(), id)
	if e != nil {
		httputil.Error(w, 404, 40400, "复核组不存在或证据无法核验")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, v)
}
func (s *Server) retryInterimReview(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "group")
	var body struct {
		RequestID   string `json:"requestId"`
		Acknowledge bool   `json:"acknowledgeUnknownCost"`
	}
	if !ok || !reviewJSON(w, r, &body, 4096) {
		return
	}
	if ok, _ := regexp.MatchString(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`, body.RequestID); !ok || !body.Acknowledge {
		httputil.Error(w, 400, 40000, "重新复核需要独立请求编号与未知费用确认")
		return
	}
	envelope, origin, attempt, e := s.Store.InterimReviewRetryInput(r.Context(), id)
	if e != nil {
		httputil.Error(w, 404, 40400, "原冻结复核输入不可用")
		return
	}
	source, e := s.Store.Get(r.Context(), origin)
	if e != nil {
		httputil.Error(w, 409, 40900, "原冻结源码与模型配置不可用")
		return
	}
	next, e := s.Store.InsertPending(r.Context(), candidateValidationTask(source, attempt, envelope, body.RequestID, true))
	if e != nil {
		httputil.Error(w, 409, 40900, "重新复核未创建："+e.Error())
		return
	}
	httputil.OK(w, map[string]any{"taskId": next, "groupId": id})
}
func (s *Server) archiveInterimReview(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, ok := pathID(r, "group")
	var body store.InterimArchiveRequest
	if !ok || !reviewJSON(w, r, &body, 256*1024) {
		return
	}
	finding, e := s.Store.ArchiveInterimReview(r.Context(), id, u.ID, body)
	if e != nil {
		httputil.Error(w, 409, 40900, "候选未归档："+e.Error())
		return
	}
	httputil.OK(w, finding)
}
