package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"bufio"
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"net/http"
	"os"
	"strconv"
)

// The archive reference, execution, every sequence and the entire journal hash
// are checked before returning a bounded page. Unknown extensions stay readable
// through the historical core/archive routes.
func (s *Server) sdkSessions(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	attempt, e := strconv.Atoi(r.PathValue("attempt"))
	after, ae := strconv.ParseInt(r.URL.Query().Get("after"), 10, 64)
	if r.URL.Query().Get("after") == "" {
		ae = nil
		after = 0
	}
	if !ok || e != nil || attempt < 1 || ae != nil || after < 0 {
		httputil.Error(w, 400, 40000, "会话事件执行编号或游标无效")
		return
	}
	archive, e := s.Store.AttemptResult(r.Context(), id, attempt)
	if e != nil || archive.Result == nil {
		s.uncommittedSDKSessions(w, r, id, attempt, after)
		return
	}
	raw, ok := archive.Result.Extensions.Value["sdkSession"]
	if !ok {
		httputil.Error(w, 404, 40400, "此历史执行未保存完整原生会话事件")
		return
	}
	summary, e := contracts.DecodeSDKSessionSummary(raw, &archive.Result.Execution)
	if e != nil || summary.Journal == nil {
		httputil.Error(w, 409, 40900, "会话事件扩展或日志引用不可用，原始归档仍可访问")
		return
	}
	ref := *summary.Journal
	found := false
	for _, a := range archive.Result.Artifacts {
		if a == ref {
			found = true
		}
	}
	if !found || ref.RelativePath != "logs/sdk-session-events.jsonl" || ref.SizeBytes > 64*1024*1024 {
		httputil.Error(w, 409, 40900, "会话事件日志引用不符合本次归档")
		return
	}
	record, e := s.Store.GetAttempt(r.Context(), id, attempt)
	if e != nil {
		httputil.Error(w, 404, 40400, "执行不可用")
		return
	}
	root, e := os.OpenRoot(record.OutputPath)
	if e != nil {
		httputil.Error(w, 404, 40400, "会话日志存储不可用")
		return
	}
	defer root.Close()
	file, e := root.Open(string(ref.RelativePath))
	if e != nil {
		httputil.Error(w, 404, 40400, "会话日志未保留")
		return
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() != int64(ref.SizeBytes) {
		httputil.Error(w, 409, 40900, "会话日志大小与归档不一致")
		return
	}
	hash := sha256.New()
	lines := bufio.NewScanner(io.TeeReader(io.LimitReader(file, int64(ref.SizeBytes)+1), hash))
	lines.Buffer(make([]byte, 65536), 8*1024*1024+1)
	items := []contracts.SDKSessionEvent{}
	var count, next int64
	bytes := 0
	pageFull := false
	for lines.Scan() {
		count++
		item, e := contracts.DecodeSDKSessionEvent(lines.Bytes(), &archive.Result.Execution)
		if e != nil || item.Sequence != count {
			httputil.Error(w, 409, 40900, "会话日志身份、顺序或内容无效")
			return
		}
		if count > after && !pageFull {
			if len(items) < 50 && (bytes+len(lines.Bytes()) <= 8*1024*1024 || len(items) == 0) {
				items = append(items, item)
				next = count
				bytes += len(lines.Bytes())
			} else {
				pageFull = true
			}
		}
	}
	if lines.Err() != nil || count != int64(summary.PersistedCount) || hex.EncodeToString(hash.Sum(nil)) != string(ref.SHA256) {
		httputil.Error(w, 409, 40900, "会话日志完整性核验失败，请保留原始产物核查")
		return
	}
	if len(items) == 0 {
		next = after
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, struct {
		Summary contracts.SDKSessionSummary `json:"summary"`
		Items   []contracts.SDKSessionEvent `json:"items"`
		Next    int64                       `json:"next"`
		HasMore bool                        `json:"hasMore"`
	}{summary, items, next, next < count})
}

func (s *Server) uncommittedSDKSessions(w http.ResponseWriter, r *http.Request, id int64, attempt int, after int64) {
	record, e := s.Store.GetAttempt(r.Context(), id, attempt)
	if e != nil || record.ObservationContractVersion != 1 {
		httputil.Error(w, 404, 40400, "本次执行没有原生会话日志")
		return
	}
	root, e := os.OpenRoot(record.OutputPath)
	if e != nil {
		httputil.Error(w, 404, 40400, "执行产物尚不可用")
		return
	}
	defer root.Close()
	file, e := root.Open("logs/sdk-session-events.jsonl")
	if e != nil {
		httputil.Error(w, 404, 40400, "SDK 尚未提供可读取的原生会话事件")
		return
	}
	defer file.Close()
	info, e := file.Stat()
	if e != nil || !info.Mode().IsRegular() || info.Size() > 64*1024*1024 {
		httputil.Error(w, 409, 40900, "未提交会话日志不符合存储边界")
		return
	}
	// Read a fixed-size snapshot. A concurrently appended event belongs to the next read.
	raw, e := io.ReadAll(io.LimitReader(file, info.Size()))
	if e != nil {
		httputil.Error(w, 503, 50300, "未提交会话日志读取失败")
		return
	}
	execution := contracts.ExecutionRef{TaskID: contracts.TaskID(strconv.FormatInt(id, 10)), Attempt: int64(attempt)}
	lines := bytes.Split(raw, []byte{'\n'})
	items := []contracts.SDKSessionEvent{}
	var count, next int64
	size := 0
	full := false
	tail := len(lines[len(lines)-1]) > 0
	for _, line := range lines[:len(lines)-1] {
		count++
		item, e := contracts.DecodeSDKSessionEvent(line, &execution)
		if e != nil || item.Sequence != count {
			httputil.Error(w, 409, 40900, "未提交会话日志的身份、顺序或内容无效")
			return
		}
		if count > after && !full {
			if len(items) < 50 && (size+len(line) <= 8*1024*1024 || len(items) == 0) {
				items = append(items, item)
				next = count
				size += len(line)
			} else {
				full = true
			}
		}
	}
	reason := "执行尚未提交完整 SDK 结果；只展示已保存事件，总事件数与完整性未知。"
	if tail {
		reason += "日志尾部存在未完成记录，原文仍保留。"
	}
	w.Header().Set("Cache-Control", "no-store")
	if r.URL.Query().Get("download") == "1" {
		hash := sha256.Sum256(raw)
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Content-Disposition", `attachment; filename="sdk-session-events.partial.jsonl"`)
		w.Header().Set("X-Axiom-Artifact-Status", "uncommitted")
		w.Header().Set("X-Axiom-SHA256", hex.EncodeToString(hash[:]))
		w.Write(raw)
		return
	}
	if len(items) == 0 {
		next = after
	}
	httputil.OK(w, struct {
		Execution     contracts.ExecutionRef       `json:"execution"`
		Summary       *contracts.SDKSessionSummary `json:"summary"`
		Items         []contracts.SDKSessionEvent  `json:"items"`
		Next          int64                        `json:"next"`
		HasMore       bool                         `json:"hasMore"`
		Uncommitted   bool                         `json:"uncommitted"`
		CapturedCount int64                        `json:"capturedCount"`
		Reason        string                       `json:"reason"`
	}{execution, nil, items, next, next < count, true, count, reason})
}
