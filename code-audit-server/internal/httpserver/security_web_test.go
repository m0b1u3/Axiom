package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"encoding/base64"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Browser auth acceptance never starts an Agent, Scanner, SDK or model. The
// historical execution and finding below are explicitly synthetic fixtures.
func TestSecurityWebAccountsSessionsAndHistoricalRead(t *testing.T) {
	if os.Getenv("AXIOM_TEST_WEB_INPUT") != "1" {
		t.Skip("AXIOM_TEST_WEB_INPUT=1 enables real browser acceptance")
	}
	s := inputPipelineStore(t)
	root, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	dist := filepath.Join(root, "code-audit-web/dist")
	if _, e = os.Stat(filepath.Join(dist, "index.html")); e != nil {
		t.Fatal("build Web before browser acceptance")
	}
	secret := "fixture-web-session-signing-key"
	password := "fixture-auth-password"
	admin, e := s.EnsureAdministrator(t.Context(), "fixture-auth-admin", auth.HashPassword(secret, password))
	if e != nil {
		t.Fatal(e)
	}
	for _, role := range []string{"viewer", "operator"} {
		if _, e = s.CreateAccount(t.Context(), "fixture-auth-"+role, auth.HashPassword(secret, password), role); e != nil {
			t.Fatal(e)
		}
	}
	t.Setenv("AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	encrypted, e := cryptokey.Encrypt("fixture-not-a-model-key")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixture auth model (no calls)", Provider: "openai", Model: "fixture-auth-model", ApiKeyEncrypted: encrypted}); e != nil {
		t.Fatal(e)
	}
	project, e := s.CreateProject(t.Context(), "Fixture shared history", admin.ID)
	if e != nil {
		t.Fatal(e)
	}
	id, e := s.InsertPending(t.Context(), store.Task{ScanNo: "fixture-auth-historical", SourcePath: t.TempDir(), Provider: "openai", Model: "fixture-auth-model", ProjectID: project.ID, ScanMode: "standard"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Claim(t.Context(), "fixture-history-agent", time.Minute); e != nil {
		t.Fatal(e)
	}
	line := 3
	raw := json.RawMessage(`{"fixtureOnly":true,"evidence":"Synthetic session acceptance, not a real vulnerability"}`)
	if e = s.ReplaceFindings(t.Context(), id, []store.Finding{{ScanID: id, Fingerprint: "fixture-auth-finding", Title: "合成会话验收发现（Fixture）", Severity: "low", FilePath: "fixture.ts", StartLine: &line, Description: "合成历史报告，用于只读权限验收。", Raw: raw}}); e != nil {
		t.Fatal(e)
	}
	if e = s.Finish(t.Context(), id, "completed", "", "", 1); e != nil {
		t.Fatal(e)
	}
	srv := &httpserver.Server{Store: s, Secret: secret, AdminUsername: admin.Username, AgentToken: "fixture-no-agent"}
	routes := srv.Routes()
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			routes.ServeHTTP(w, r)
			return
		}
		clean := filepath.Join(dist, filepath.Clean(r.URL.Path))
		if r.URL.Path != "/" {
			if _, e := os.Stat(clean); e == nil {
				http.FileServer(http.Dir(dist)).ServeHTTP(w, r)
				return
			}
		}
		http.ServeFile(w, r, filepath.Join(dist, "index.html"))
	}))
	defer web.Close()
	command := exec.Command("node", "tests/security-web-acceptance.mjs")
	command.Dir = filepath.Join(root, "code-audit-web")
	command.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+web.URL, "AXIOM_TEST_AUTH_TASK="+securityAccountID(id))
	if out, e := command.CombinedOutput(); e != nil {
		t.Fatalf("auth browser: %v\n%s", e, out)
	} else {
		t.Log(string(out))
	}
	migrated, e := s.GetUser(t.Context(), admin.ID)
	if e != nil || !strings.HasPrefix(migrated.PasswordHash, "$argon2id$") {
		t.Fatal("browser login did not migrate old account")
	}
	if task, e := s.Get(t.Context(), id); e != nil || task.Status != "completed" || task.Attempt != 1 {
		t.Fatal("auth operations changed historical execution")
	}
}
