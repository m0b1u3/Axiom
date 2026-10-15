package httpserver

import (
	"axiom/code-audit-server/internal/store"
	"net/http/httptest"
	"path/filepath"
	"testing"
)

func TestReadinessTracksDatabaseAvailability(t *testing.T) {
	s, e := store.Open(filepath.Join(t.TempDir(), "health.db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	routes := (&Server{Store: s}).Routes()
	check := func(path string, status int) {
		t.Helper()
		response := httptest.NewRecorder()
		routes.ServeHTTP(response, httptest.NewRequest("GET", path, nil))
		if response.Code != status || response.Header().Get("Cache-Control") != "no-store" {
			t.Fatal(path, response.Code, response.Body.String())
		}
	}
	check("/healthz", 200)
	check("/readyz", 200)
	s.Close()
	check("/healthz", 200)
	check("/readyz", 503)
}
