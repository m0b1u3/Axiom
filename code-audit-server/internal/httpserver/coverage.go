package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"encoding/json"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

// Existing finalized artifacts are read only as a fallback for pre-upgrade attempts.
func (s *Server) scanCoverage(w http.ResponseWriter, r *http.Request, _ *store.User) {
	w.Header().Set("Cache-Control", "no-store")
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "invalid scan id")
		return
	}
	task, e := s.Store.Get(r.Context(), id)
	if e != nil {
		httputil.Error(w, 404, 40400, "scan not found")
		return
	}
	attempt := task.Attempt
	if value := r.URL.Query().Get("attempt"); value != "" {
		attempt, e = strconv.Atoi(value)
		if e != nil || attempt < 1 {
			httputil.Error(w, 400, 40000, "invalid attempt")
			return
		}
	}
	reply := map[string]any{"available": false, "coverage": nil, "attempt": attempt, "source": "unavailable", "reason": "本次执行尚未生成覆盖报告"}
	if attempt < 1 {
		httputil.OK(w, reply)
		return
	}
	attempts, e := s.Store.ListAttempts(r.Context(), id)
	if e != nil {
		httputil.Error(w, 503, 50300, "execution history unavailable")
		return
	}
	var selected *store.Attempt
	for i := range attempts {
		if attempts[i].Attempt == attempt {
			selected = &attempts[i]
			break
		}
	}
	if selected == nil {
		httputil.Error(w, 404, 40400, "execution not found")
		return
	}
	reply["status"] = selected.Status
	if !store.IsTerminalStatus(selected.Status) {
		httputil.OK(w, reply)
		return
	}
	result, e := s.Store.AttemptResult(r.Context(), id, attempt)
	if e != nil {
		httputil.Error(w, 503, 50300, "coverage archive unavailable")
		return
	}
	raw := result.Coverage
	source := "archive"
	// Old normalization kept completeness only: prefer the original full report if retained.
	var doc map[string]any
	_ = json.Unmarshal(raw, &doc)
	if len(doc) <= 1 && selected.OutputPath != "" {
		if original, err := readCoverageArtifact(filepath.Join(selected.OutputPath, "raw", "codex-security", "coverage.json")); err == nil {
			raw = original
			source = "legacy_artifact"
		}
	}
	doc = nil
	if json.Unmarshal(raw, &doc) == nil && doc != nil {
		reply["available"] = true
		reply["coverage"] = doc
		reply["source"] = source
		reply["reason"] = ""
	} else {
		reply["reason"] = "该执行未保留可用覆盖报告，无法确认完整性"
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, reply)
}
func readCoverageArtifact(name string) (json.RawMessage, error) {
	f, e := os.Open(name)
	if e != nil {
		return nil, e
	}
	defer f.Close()
	b, e := io.ReadAll(io.LimitReader(f, 16<<20+1))
	if e != nil {
		return nil, e
	}
	if len(b) > 16<<20 {
		return nil, io.ErrShortBuffer
	}
	var doc map[string]any
	e = json.Unmarshal(b, &doc)
	if e != nil || doc == nil {
		return nil, os.ErrInvalid
	}
	return b, nil
}
