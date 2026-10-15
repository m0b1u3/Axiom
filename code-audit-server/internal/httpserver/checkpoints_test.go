package httpserver

import (
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

func TestCheckpointDownload(t *testing.T) {
	dir := t.TempDir()
	root := filepath.Join(dir, "checkpoints")
	os.Mkdir(root, 0700)
	file := filepath.Join(root, "evidence.jsonl")
	request := httptest.NewRequest("GET", "/api/v1/scans/13/checkpoints", nil)
	missing := httptest.NewRecorder()
	serveCheckpoints(missing, request, dir)
	if missing.Code != 404 {
		t.Fatalf("missing: %d", missing.Code)
	}
	if err := os.WriteFile(file, []byte("{\"notValidated\":true}\n"), 0600); err != nil {
		t.Fatal(err)
	}
	valid := httptest.NewRecorder()
	serveCheckpoints(valid, request, dir)
	if valid.Code != 200 || valid.Header().Get("Cache-Control") != "no-store" || valid.Body.String() != "{\"notValidated\":true}\n" {
		t.Fatalf("invalid download: %d %s", valid.Code, valid.Body.String())
	}
	os.Remove(file)
	outside := filepath.Join(t.TempDir(), "secret")
	os.WriteFile(outside, []byte("private"), 0600)
	if err := os.Symlink(outside, file); err != nil {
		t.Fatal(err)
	}
	blocked := httptest.NewRecorder()
	serveCheckpoints(blocked, request, dir)
	if blocked.Code != 404 {
		t.Fatalf("symlink escaped: %d", blocked.Code)
	}
}
