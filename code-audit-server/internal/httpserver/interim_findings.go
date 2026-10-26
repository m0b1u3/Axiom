package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"net/http"
	"os"
	"regexp"
	"strconv"
	"strings"
)

const interimIndexPath = "checkpoints/interim/index.json"

var interimDigest = regexp.MustCompile(`^[a-f0-9]{64}$`)

// Server reads only the portable Scanner projection. SDK files, SQLite and
// model-generated checkpoint formats never cross this module boundary.
func interimBytes(root *os.Root, relative string, limit int64) ([]byte, error) {
	at := ""
	for _, p := range strings.Split(relative, "/") {
		if p == "" || p == "." || p == ".." || strings.Contains(p, `\`) {
			return nil, fmt.Errorf("invalid evidence path")
		}
		if at != "" {
			at += "/"
		}
		at += p
		v, e := root.Lstat(at)
		if e != nil {
			return nil, e
		}
		if v.Mode()&os.ModeSymlink != 0 {
			return nil, fmt.Errorf("evidence link rejected")
		}
	}
	f, e := root.Open(relative)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	v, e := f.Stat()
	if e != nil {
		return nil, e
	}
	if !v.Mode().IsRegular() || v.Size() > limit {
		return nil, fmt.Errorf("evidence exceeds read boundary")
	}
	raw, e := io.ReadAll(io.LimitReader(f, limit+1))
	if e != nil || int64(len(raw)) > limit {
		return nil, fmt.Errorf("evidence read incomplete")
	}
	return raw, nil
}
func checkedInterim(root *os.Root, execution contracts.ExecutionRef, source string) (contracts.InterimSnapshot, error) {
	raw, e := interimBytes(root, interimIndexPath, 8*1024*1024)
	if e != nil {
		return contracts.InterimSnapshot{}, e
	}
	v, e := contracts.DecodeInterimSnapshot(raw, &execution)
	if e != nil {
		return v, e
	}
	if string(v.SourceContentHash) != source {
		return v, fmt.Errorf("evidence source mismatch")
	}
	refs := map[contracts.Digest]contracts.InterimDocumentRef{}
	latest := map[string]contracts.Digest{}
	for _, d := range v.Documents {
		a := d.Artifact
		if a.Kind != "engine_raw" || string(a.RelativePath) != "checkpoints/interim/objects/"+string(a.SHA256)+".json" || a.SizeBytes > 4*1024*1024 {
			return v, fmt.Errorf("evidence reference mismatch")
		}
		if _, ok := refs[a.SHA256]; ok {
			return v, fmt.Errorf("duplicate evidence reference")
		}
		refs[a.SHA256] = d
		latest[string(d.SourcePath)] = a.SHA256
	}
	ids := map[contracts.Digest]bool{}
	for _, c := range v.Candidates {
		d, ok := refs[c.Artifact]
		if !ok || latest[string(d.SourcePath)] != c.Artifact || ids[c.ID] {
			return v, fmt.Errorf("candidate provenance mismatch")
		}
		digest := sha256.Sum256([]byte(string(d.SourcePath) + "\x00" + c.Pointer))
		if string(c.ID) != hex.EncodeToString(digest[:]) {
			return v, fmt.Errorf("candidate identity mismatch")
		}
		ids[c.ID] = true
	}
	return v, nil
}
func interimQuery(r *http.Request, key string, fallback, maximum int) (int, error) {
	v := r.URL.Query().Get(key)
	if v == "" {
		return fallback, nil
	}
	n, e := strconv.Atoi(v)
	if e != nil || n < 1 || n > maximum {
		return 0, fmt.Errorf("invalid %s", key)
	}
	return n, nil
}
func (s *Server) interimContext(w http.ResponseWriter, r *http.Request) (*store.Task, *store.Attempt, contracts.ExecutionRef, bool) {
	id, ok := pathID(r, "id")
	attempt, e := strconv.Atoi(r.PathValue("attempt"))
	if !ok || e != nil || attempt < 1 {
		httputil.Error(w, 400, 40000, "中间发现执行编号无效")
		return nil, nil, contracts.ExecutionRef{}, false
	}
	task, e := s.Store.Get(r.Context(), id)
	if e != nil {
		httputil.Error(w, 404, 40400, "扫描任务不存在")
		return nil, nil, contracts.ExecutionRef{}, false
	}
	a, e := s.Store.GetAttempt(r.Context(), id, attempt)
	if e != nil {
		httputil.Error(w, 404, 40400, "本次执行不存在，历史任务仍可读取")
		return nil, nil, contracts.ExecutionRef{}, false
	}
	if task.ScanMode != "standard" || task.SDKOperation != nil || task.ComponentPlanning != nil || task.ComponentScan != nil || !interimDigest.MatchString(task.SourceContentHash) {
		httputil.Error(w, 404, 40400, "此历史执行不属于 Standard 中间发现合同")
		return nil, nil, contracts.ExecutionRef{}, false
	}
	return task, a, contracts.ExecutionRef{TaskID: contracts.TaskID(strconv.FormatInt(id, 10)), Attempt: int64(attempt)}, true
}
func (s *Server) interimFindings(w http.ResponseWriter, r *http.Request, _ *store.User) {
	t, a, execution, ok := s.interimContext(w, r)
	if !ok {
		return
	}
	page, e := interimQuery(r, "page", 1, 1000000)
	size, se := interimQuery(r, "pageSize", 20, 100)
	docPage, de := interimQuery(r, "documentPage", 1, 1000000)
	q := r.URL.Query().Get("search")
	filter := r.URL.Query().Get("artifact")
	if e != nil || se != nil || de != nil || len(q) > 1024 || (filter != "" && !interimDigest.MatchString(filter)) {
		httputil.Error(w, 400, 40000, "中间发现分页或筛选无效")
		return
	}
	out := contracts.InterimPage{SchemaVersion: 1, Execution: execution, SourceContentHash: contracts.Digest(t.SourceContentHash), SDKVersion: "0.5.0", State: "unavailable", Reason: "本次执行尚无可展示的中间发现快照；不能据此认定没有漏洞。原始会话与历史报告保持可读。", Items: []contracts.InterimCandidate{}, Documents: []contracts.InterimDocumentRef{}, Issues: []string{}, Page: int64(page), PageSize: int64(size), DocumentPage: int64(docPage)}
	root, e := os.OpenRoot(a.OutputPath)
	if e != nil {
		w.Header().Set("Cache-Control", "no-store")
		httputil.OK(w, out)
		return
	}
	defer root.Close()
	v, e := checkedInterim(root, execution, t.SourceContentHash)
	if errors.Is(e, os.ErrNotExist) {
		httputil.OK(w, out)
		return
	}
	if e != nil {
		httputil.Error(w, 409, 40900, "中间发现索引身份或完整性检查失败；原始产物及最终归档未被替换")
		return
	}
	out.State = contracts.InterimPageState(v.State)
	out.Revision = v.Revision
	out.ObservedAt = &v.ObservedAt
	out.Reason = "候选记录来自已保存的模型检查点，尚未最终汇总或独立验证；可能重复、被撤回或与最终结果不同。"
	out.Issues = v.Issues
	severity, duplicates := r.URL.Query().Get("severity"), r.URL.Query().Get("duplicates")
	if !catalogValue(severity, "", "critical", "high", "medium", "low", "info") || !catalogValue(duplicates, "", "collapse", "only") {
		httputil.Error(w, 400, 40000, "候选严重度或精确去重筛选无效")
		return
	}
	counts := map[string]int{}
	for _, c := range v.Candidates {
		counts[interimCandidateKey(c)]++
	}
	seen := map[string]bool{}
	q = strings.ToLower(q)
	items := []contracts.InterimCandidate{}
	for _, c := range v.Candidates {
		if filter != "" && string(c.Artifact) != filter {
			continue
		}
		hay := c.Title + " " + strings.Join(c.Evidence, " ")
		if c.Summary != nil {
			hay += " " + *c.Summary
		}
		if !strings.Contains(strings.ToLower(hay), q) {
			continue
		}
		if severity != "" && (c.ReportedSeverity == nil || strings.ToLower(*c.ReportedSeverity) != severity) {
			continue
		}
		key := interimCandidateKey(c)
		if duplicates == "only" && counts[key] < 2 {
			continue
		}
		if duplicates == "collapse" && seen[key] {
			continue
		}
		seen[key] = true
		items = append(items, c)
	}
	if duplicates != "" {
		out.Reason += " 精确重复仅依据相同标题、摘要、初评严重度和证据引用；不会自动合并相似问题。"
	}
	out.Total = int64(len(items))
	start := (page - 1) * size
	if start < len(items) {
		end := min(start+size, len(items))
		out.Items = items[start:end]
	}
	out.DocumentTotal = int64(len(v.Documents))
	start = (docPage - 1) * 50
	if start < len(v.Documents) {
		end := min(start+50, len(v.Documents))
		out.Documents = v.Documents[start:end]
	}
	raw, e := contracts.EncodeJSON(out)
	if e != nil || contracts.ValidateJSON(contracts.KindInterimPage, raw, &execution) != nil {
		httputil.Error(w, 503, 50300, "中间发现分页未通过合同校验")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, out)
}
func (s *Server) interimEvidence(w http.ResponseWriter, r *http.Request, _ *store.User) {
	t, a, execution, ok := s.interimContext(w, r)
	if !ok {
		return
	}
	digest := r.PathValue("artifact")
	if !interimDigest.MatchString(digest) {
		httputil.Error(w, 400, 40000, "中间证据哈希无效")
		return
	}
	root, e := os.OpenRoot(a.OutputPath)
	if e != nil {
		httputil.Error(w, 404, 40400, "中间证据存储不可用")
		return
	}
	defer root.Close()
	v, e := checkedInterim(root, execution, t.SourceContentHash)
	if e != nil {
		httputil.Error(w, 409, 40900, "中间证据索引无法核验")
		return
	}
	for _, ref := range v.Documents {
		if string(ref.Artifact.SHA256) != digest {
			continue
		}
		raw, e := interimBytes(root, string(ref.Artifact.RelativePath), 4*1024*1024)
		if e != nil {
			httputil.Error(w, 409, 40900, "中间证据文件缺失或不符合读取边界")
			return
		}
		hash := sha256.Sum256(raw)
		if int64(len(raw)) != int64(ref.Artifact.SizeBytes) || hex.EncodeToString(hash[:]) != digest {
			httputil.Error(w, 409, 40900, "中间证据哈希已变化，已拒绝展示；历史结果保持保留")
			return
		}
		doc, e := contracts.DecodeInterimDocument(raw, &execution)
		if e != nil || string(doc.SourceContentHash) != t.SourceContentHash || doc.SourcePath != ref.SourcePath || doc.SourceSHA256 != ref.SourceSHA256 || string(doc.Kind) != string(ref.Kind) || doc.ObservedAt != ref.ObservedAt {
			httputil.Error(w, 409, 40900, "中间证据来源绑定不匹配")
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		if r.URL.Query().Get("download") == "1" {
			w.Header().Set("Content-Type", "application/json")
			w.Header().Set("Content-Disposition", `attachment; filename="interim-evidence-`+digest+`.json"`)
			w.Header().Set("Content-Length", strconv.Itoa(len(raw)))
			w.WriteHeader(http.StatusOK)
			_, _ = w.Write(raw)
			return
		}
		httputil.OK(w, doc)
		return
	}
	httputil.Error(w, 404, 40400, "所选证据不属于本次执行，未访问其他产物")
}
