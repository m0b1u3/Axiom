package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"sync/atomic"
	"testing"
)

func TestStructuredDiagnosticAuthenticationAndBusinessIsolation(t *testing.T) {
	st, server, web, secret := securityFixture(t)
	scratch := t.TempDir()
	server.QueueLimits.OutputRoot = filepath.Join(scratch, "scans")
	for _, role := range []string{"viewer", "operator"} {
		if _, e := st.CreateAccount(t.Context(), role, auth.HashPassword(secret, "fixture-password"), role); e != nil {
			t.Fatal(e)
		}
	}
	var providerCalls atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerCalls.Add(1)
		http.Error(w, "must never be contacted", 502)
	}))
	defer provider.Close()
	model, e := st.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixtures / Mock diagnostic trap", Provider: "custom", Model: "fixtures-only-model", BaseURL: provider.URL, WireAPI: "responses", ApiKeyEncrypted: "fixture-ciphertext-must-not-be-decrypted"})
	if e != nil {
		t.Fatal(e)
	}
	history, e := st.InsertPending(t.Context(), store.Task{ScanNo: "fixture-protected-history", SourcePath: "/frozen/fixture", SourceContentHash: strings.Repeat("1", 64), Provider: "custom", Model: model.Model, ModelConfigID: model.ID, ScanMode: "standard"})
	if e != nil {
		t.Fatal(e)
	}
	before, e := st.Get(t.Context(), history)
	if e != nil {
		t.Fatal(e)
	}
	marker := filepath.Join(scratch, "historical-report.json")
	if e = os.WriteFile(marker, []byte(`{"fixture":"protected historical artifact"}`), 0600); e != nil {
		t.Fatal(e)
	}
	request := map[string]any{"schemaVersion": 1, "mode": "offline", "catalogSha256": contracts.StructuredDiagnosticCatalogHash, "protocol": "responses", "scenario": "title_only"}
	endpoint := web.URL + "/api/v1/diagnostics/structured-output"
	if code, _ := getJSON(t, endpoint, ""); code != 401 {
		t.Fatal("unauthenticated preview allowed")
	}
	for _, role := range []string{"viewer", "operator"} {
		token := login(t, web.URL, role, "fixture-password")
		if code, _ := getJSON(t, endpoint, token); code != 200 {
			t.Fatal("read preview", role, code)
		}
		if code, _ := doJSON(t, endpoint+"/records", token, request); code != 403 {
			t.Fatal("non-admin receipt write", role, code)
		}
		if code, _ := getJSON(t, endpoint+"/records", token); code != 403 {
			t.Fatal("non-admin history", role, code)
		}
	}
	token := login(t, web.URL, "admin", "fixture-admin-password")
	if code, raw := doJSON(t, endpoint+"/records", token, request); code != 201 {
		t.Fatal(code, raw)
	}
	if code, raw := getJSON(t, endpoint+"/records", token); code != 200 || strings.Contains(string(raw), "fixture-ciphertext") || strings.Contains(string(raw), provider.URL) || strings.Contains(string(raw), "bodyJson") {
		t.Fatal("receipt leaked private input")
	}
	after, e := st.Get(t.Context(), history)
	if e != nil || !reflect.DeepEqual(before, after) {
		t.Fatal("business history changed", e)
	}
	rows, e := st.ListModelConfigs(t.Context())
	if e != nil || len(rows) != 1 || rows[0].ApiKeyEncrypted != model.ApiKeyEncrypted {
		t.Fatal("model credential changed", e)
	}
	if providerCalls.Load() != 0 {
		t.Fatal("offline diagnostic contacted provider")
	}
	raw, _ := os.ReadFile(marker)
	if string(raw) != `{"fixture":"protected historical artifact"}` {
		t.Fatal("report changed")
	}
}

// Opt-in only: a real disposable Go Server/SQLite + built Web, no Agent/SDK.
func TestStructuredDiagnosticBrowserAcceptance(t *testing.T) {
	if os.Getenv("AXIOM_TEST_STRUCTURED_WEB") != "1" {
		t.Skip("opt-in isolated browser acceptance")
	}
	st, server, _, secret := securityFixture(t)
	scratch := t.TempDir()
	server.QueueLimits.OutputRoot = filepath.Join(scratch, "scans")
	if _, e := st.CreateAccount(t.Context(), "viewer", auth.HashPassword(secret, "fixture-password"), "viewer"); e != nil {
		t.Fatal(e)
	}
	var upstream atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		upstream.Add(1)
		http.Error(w, "offline fixture must not call provider", 502)
	}))
	defer provider.Close()
	model, e := st.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixtures / Mock upstream trap", Provider: "custom", Model: "fixtures-only", BaseURL: provider.URL, ApiKeyEncrypted: "fixtures-only-encrypted-value"})
	if e != nil {
		t.Fatal(e)
	}
	root, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	dist := filepath.Join(root, "code-audit-web/dist")
	if _, e = os.Stat(filepath.Join(dist, "index.html")); e != nil {
		t.Fatal("build Web first")
	}
	routes := server.Routes()
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			routes.ServeHTTP(w, r)
			return
		}
		name := filepath.Join(dist, filepath.Clean(r.URL.Path))
		if info, e := os.Stat(name); e == nil && !info.IsDir() {
			http.FileServer(http.Dir(dist)).ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dist, "index.html"))
	}))
	defer web.Close()
	command := exec.CommandContext(t.Context(), "node", "tests/structured-output-web-acceptance.mjs")
	command.Dir = filepath.Join(root, "code-audit-web")
	command.Env = append(os.Environ(), "AXIOM_TEST_STRUCTURED_URL="+web.URL)
	if out, e := command.CombinedOutput(); e != nil {
		t.Fatalf("browser acceptance: %v\n%s", e, out)
	} else {
		t.Log(string(out))
	}
	if upstream.Load() != 0 {
		t.Fatal("provider contacted")
	}
	rows, e := st.ListModelConfigs(t.Context())
	if e != nil || len(rows) != 1 || rows[0].ApiKeyEncrypted != model.ApiKeyEncrypted {
		t.Fatal("credential changed")
	}
	t.Logf("fixtureOnly=true upstreamRequests=%d credentialPreserved=true", upstream.Load())
}
