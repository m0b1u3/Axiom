package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"net/http"
	"os"
	"path/filepath"
)

func (s *Server) scanCheckpoints(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "invalid scan id")
		return
	}
	task, err := s.Store.Get(r.Context(), id)
	if err != nil {
		httputil.Error(w, 404, 40400, "scan not found")
		return
	}
	output, err := s.logOutputPath(r, task)
	if err != nil || output == "" {
		httputil.Error(w, 404, 40400, "scan attempt not found")
		return
	}
	serveCheckpoints(w, r, output)
}

func serveCheckpoints(w http.ResponseWriter, r *http.Request, output string) {
	file := filepath.Join(output, "checkpoints", "evidence.jsonl")
	resolved, err := filepath.EvalSymlinks(file)
	if err != nil {
		httputil.Error(w, 404, 40400, "no saved intermediate evidence")
		return
	}
	root, err := filepath.EvalSymlinks(output)
	if err != nil || resolved != filepath.Join(root, "checkpoints", "evidence.jsonl") {
		httputil.Error(w, 404, 40400, "invalid evidence path")
		return
	}
	st, err := os.Stat(resolved)
	if err != nil || !st.Mode().IsRegular() || st.Size() > 11*1024*1024 {
		httputil.Error(w, 404, 40400, "evidence unavailable")
		return
	}
	w.Header().Set("Content-Type", "application/x-ndjson")
	w.Header().Set("Content-Disposition", `attachment; filename="audit-evidence.jsonl"`)
	w.Header().Set("Cache-Control", "no-store")
	http.ServeFile(w, r, resolved)
}
