package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"crypto/sha256"
	"encoding/hex"
	"io"
	"mime"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
)

// Only paths recorded in the archived contract are downloadable. A reference
// never grants access outside that attempt, even through a replaced symlink.
func (s *Server) resultArtifact(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	attempt, e := strconv.Atoi(r.PathValue("attempt"))
	if !ok || e != nil || attempt < 1 {
		httputil.Error(w, 400, 40000, "invalid execution id")
		return
	}
	archive, e := s.Store.AttemptResult(r.Context(), id, attempt)
	if e != nil || archive.Result == nil {
		httputil.Error(w, 404, 40400, "archived result unavailable")
		return
	}
	wanted := r.URL.Query().Get("path")
	for _, artifact := range archive.Result.Artifacts {
		if string(artifact.RelativePath) != wanted {
			continue
		}
		record, e := s.Store.GetAttempt(r.Context(), id, attempt)
		if e != nil {
			httputil.Error(w, 404, 40400, "execution unavailable")
			return
		}
		root, e := os.OpenRoot(record.OutputPath)
		if e != nil {
			httputil.Error(w, 404, 40400, "artifact storage unavailable")
			return
		}
		defer root.Close()
		f, e := root.Open(filepath.FromSlash(wanted))
		if e != nil {
			httputil.Error(w, 404, 40400, "artifact is no longer retained")
			return
		}
		defer f.Close()
		info, e := f.Stat()
		if e != nil || !info.Mode().IsRegular() || info.Size() != int64(artifact.SizeBytes) {
			httputil.Error(w, 409, 40905, "artifact size differs from archived reference")
			return
		}
		// Copy once into a private snapshot. Hashing the source and then serving
		// it would permit a concurrent write between verification and response.
		snapshot, e := os.CreateTemp("", "axiom-result-download-*")
		if e != nil {
			httputil.Error(w, 503, 50300, "artifact download storage unavailable")
			return
		}
		defer os.Remove(snapshot.Name())
		defer snapshot.Close()
		hash := sha256.New()
		copied, e := io.Copy(io.MultiWriter(hash, snapshot), io.LimitReader(f, int64(artifact.SizeBytes)+1))
		if e != nil || copied != int64(artifact.SizeBytes) || hex.EncodeToString(hash.Sum(nil)) != string(artifact.SHA256) {
			httputil.Error(w, 409, 40905, "artifact checksum differs from archived reference")
			return
		}
		if _, e = snapshot.Seek(0, io.SeekStart); e != nil {
			httputil.Error(w, 500, 50000, "artifact read failed")
			return
		}
		w.Header().Set("Content-Type", "application/octet-stream")
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Content-Disposition", mime.FormatMediaType("attachment", map[string]string{"filename": filepath.Base(wanted)}))
		http.ServeContent(w, r, filepath.Base(wanted), info.ModTime(), snapshot)
		return
	}
	httputil.Error(w, 404, 40400, "artifact is not listed in the archived result")
}
