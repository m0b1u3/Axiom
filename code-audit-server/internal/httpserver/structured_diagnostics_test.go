package httpserver

import (
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"encoding/json"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

func structuredTestServer(t *testing.T) *Server {
	t.Helper()
	return &Server{QueueLimits: store.ClaimLimits{OutputRoot: filepath.Join(t.TempDir(), "scans")}}
}
func structuredCreate(t *testing.T, s *Server, body string) *httptest.ResponseRecorder {
	t.Helper()
	w := httptest.NewRecorder()
	s.createStructuredDiagnostic(w, httptest.NewRequest("POST", "/", strings.NewReader(body)), nil)
	return w
}
func structuredRequest(t *testing.T) string {
	t.Helper()
	raw, e := contracts.EncodeJSON(contracts.StructuredDiagnosticRequest{SchemaVersion: 1, Mode: "offline", CatalogSHA256: contracts.StructuredDiagnosticCatalogHash, Protocol: "responses", Scenario: "title_only"})
	if e != nil {
		t.Fatal(e)
	}
	return string(raw)
}
func TestStructuredDiagnosticOfflineNoStoreAndConcurrentReceipts(t *testing.T) {
	s := structuredTestServer(t)
	base := filepath.Dir(s.QueueLimits.OutputRoot)
	for _, name := range []string{"database", "historical-report", "sdk-credentials"} {
		if e := os.WriteFile(filepath.Join(base, name), []byte("protected-fixture-only"), 0600); e != nil {
			t.Fatal(e)
		}
	}
	body := structuredRequest(t)
	var wg sync.WaitGroup
	for i := 0; i < 12; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if w := structuredCreate(t, s, body); w.Code != 201 {
				t.Errorf("offline create: %d %s", w.Code, w.Body.String())
			}
		}()
	}
	wg.Wait()
	root, e := s.structuredRecordRoot(false)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	entries, e := os.ReadDir(root.Name())
	if e != nil || len(entries) != 12 {
		t.Fatalf("receipts lost: %v %d", e, len(entries))
	}
	for _, entry := range entries {
		if strings.HasSuffix(entry.Name(), ".tmp") {
			t.Fatal("unfinished receipt")
		}
		info, _ := entry.Info()
		if info.Mode().Perm() != 0600 {
			t.Fatal("receipt mode")
		}
		v, e := readStructuredRecord(root, entry.Name())
		if e != nil || v.CredentialLoaded || v.UpstreamContacted || v.ProviderCompatibility != "not_tested" {
			t.Fatalf("bad receipt: %v", e)
		}
	}
	for _, name := range []string{"database", "historical-report", "sdk-credentials"} {
		raw, _ := os.ReadFile(filepath.Join(base, name))
		if string(raw) != "protected-fixture-only" {
			t.Fatal("protected file changed")
		}
	}
	for _, page := range []struct {
		url    string
		length int
	}{{"/?page=1&pageSize=5", 5}, {"/?page=3&pageSize=5", 2}, {"/?page=999999&pageSize=5", 0}} {
		w := httptest.NewRecorder()
		s.listStructuredDiagnostics(w, httptest.NewRequest("GET", page.url, nil), nil)
		var result struct {
			Data contracts.StructuredDiagnosticPage
		}
		if e := json.Unmarshal(w.Body.Bytes(), &result); e != nil || w.Code != 200 || len(result.Data.Items) != page.length || result.Data.Total != 12 {
			t.Fatalf("bad page: %d %s", w.Code, w.Body.String())
		}
	}
}
func TestStructuredDiagnosticRejectsRealInputsAndStalePreview(t *testing.T) {
	s := structuredTestServer(t)
	body := structuredRequest(t)
	for _, raw := range []string{strings.Replace(body, `"offline"`, `"live"`, 1), strings.Replace(body, `"title_only"`, `"custom"`, 1), strings.TrimSuffix(body, "}") + `,"apiKey":"fixture-secret"}`, strings.TrimSuffix(body, "}") + `,"baseUrl":"http://must-never-fetch.invalid"}`, body + body, strings.Repeat("x", 4097)} {
		w := structuredCreate(t, s, raw)
		if w.Code != 400 && w.Code != 413 {
			t.Fatalf("accepted input %d", w.Code)
		}
		if strings.Contains(w.Body.String(), "fixture-secret") || strings.Contains(w.Body.String(), "must-never-fetch") {
			t.Fatal("error reflected input")
		}
	}
	if w := structuredCreate(t, s, strings.Replace(body, contracts.StructuredDiagnosticCatalogHash, strings.Repeat("a", 64), 1)); w.Code != 409 {
		t.Fatal("stale preview accepted")
	}
	if root, _ := s.structuredRecordRoot(false); root != nil {
		root.Close()
		t.Fatal("invalid request wrote storage")
	}
	for _, q := range []string{"page=0", "page=-1", "page=1.5", "pageSize=51", "pageSize=0"} {
		w := httptest.NewRecorder()
		s.listStructuredDiagnostics(w, httptest.NewRequest("GET", "/?"+q, nil), nil)
		if w.Code != 400 {
			t.Fatal(q, w.Code)
		}
	}
}
func TestStructuredDiagnosticStorageFailuresDoNotMasqueradeAsEmptyOrComplete(t *testing.T) {
	s := structuredTestServer(t)
	w := httptest.NewRecorder()
	s.listStructuredDiagnostics(w, httptest.NewRequest("GET", "/", nil), nil)
	if w.Code != 200 || !strings.Contains(w.Body.String(), `"items":[]`) {
		t.Fatal("fresh list")
	}
	if w = structuredCreate(t, s, structuredRequest(t)); w.Code != 201 {
		t.Fatal(w.Code, w.Body.String())
	}
	root, e := s.structuredRecordRoot(false)
	if e != nil {
		t.Fatal(e)
	}
	defer root.Close()
	entries, _ := os.ReadDir(root.Name())
	name := entries[0].Name()
	raw, e := os.ReadFile(filepath.Join(root.Name(), name))
	if e != nil {
		t.Fatal(e)
	}
	// Valid JSON and shape, but changed facts: must fail the fixed fixture binding.
	changed := strings.Replace(string(raw), `"bodyState":"title_only"`, `"bodyState":"body_present"`, 1)
	if e = os.WriteFile(filepath.Join(root.Name(), name), []byte(changed), 0600); e != nil {
		t.Fatal(e)
	}
	w = httptest.NewRecorder()
	s.listStructuredDiagnostics(w, httptest.NewRequest("GET", "/", nil), nil)
	if w.Code != 503 {
		t.Fatal("tampered receipt accepted")
	}
	if _, e = os.Stat(filepath.Join(root.Name(), name)); e != nil {
		t.Fatal("corrupt evidence removed")
	}
	if e = os.WriteFile(filepath.Join(root.Name(), name), []byte("{"), 0600); e != nil {
		t.Fatal(e)
	}
	w = httptest.NewRecorder()
	s.listStructuredDiagnostics(w, httptest.NewRequest("GET", "/", nil), nil)
	if w.Code != 503 {
		t.Fatal("corrupt receipt interpreted as empty")
	}
	blocked := structuredTestServer(t)
	if e = os.WriteFile(filepath.Join(filepath.Dir(blocked.QueueLimits.OutputRoot), "diagnostics"), []byte("blocked"), 0600); e != nil {
		t.Fatal(e)
	}
	if w = structuredCreate(t, blocked, structuredRequest(t)); w.Code != 503 {
		t.Fatal("storage failure hidden")
	}
	noStorage := &Server{}
	if w = structuredCreate(t, noStorage, structuredRequest(t)); w.Code != 503 {
		t.Fatal("unconfigured storage hidden")
	}
}
func TestStructuredDiagnosticRefusesSymlinkDirectories(t *testing.T) {
	s := structuredTestServer(t)
	if e := os.Symlink(t.TempDir(), filepath.Join(filepath.Dir(s.QueueLimits.OutputRoot), "diagnostics")); e != nil {
		t.Skip(e)
	}
	if w := structuredCreate(t, s, structuredRequest(t)); w.Code != 503 {
		t.Fatal("symlink storage allowed")
	}
}

func TestStructuredDiagnosticReceiptsSurviveServerReopen(t *testing.T) {
	first := structuredTestServer(t)
	created := structuredCreate(t, first, structuredRequest(t))
	if created.Code != 201 {
		t.Fatal(created.Code, created.Body.String())
	}
	var envelope struct {
		Data contracts.StructuredDiagnosticRecord
	}
	if e := json.Unmarshal(created.Body.Bytes(), &envelope); e != nil {
		t.Fatal(e)
	}
	next := &Server{QueueLimits: first.QueueLimits}
	page := httptest.NewRecorder()
	next.listStructuredDiagnostics(page, httptest.NewRequest("GET", "/", nil), nil)
	var listed struct {
		Data contracts.StructuredDiagnosticPage
	}
	if e := json.Unmarshal(page.Body.Bytes(), &listed); e != nil || page.Code != 200 || len(listed.Data.Items) != 1 {
		t.Fatal(page.Code, page.Body.String())
	}
	before, _ := contracts.EncodeJSON(envelope.Data)
	after, _ := contracts.EncodeJSON(listed.Data.Items[0])
	if string(before) != string(after) {
		t.Fatal("reopening changed the original receipt")
	}
}
