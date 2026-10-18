package httpserver_test

import (
	"archive/zip"
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/snapshot"
	"axiom/code-audit-server/internal/store"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

// This is a UI/HTTP acceptance fixture. No Agent, Scanner, SDK, user key or
// model process is started. Provider buttons talk only to the synthetic server.
func TestFrontendPageActionsAndRecovery(t *testing.T) {
	if os.Getenv("AXIOM_TEST_WEB_INPUT") != "1" {
		t.Skip("AXIOM_TEST_WEB_INPUT=1 enables browser acceptance")
	}
	s := inputPipelineStore(t)
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	dist := filepath.Join(root, "code-audit-web/dist")
	if _, err := os.Stat(filepath.Join(dist, "index.html")); err != nil {
		t.Fatal("build Web before browser acceptance")
	}
	scratch := t.TempDir()
	source := filepath.Join(scratch, "snapshots")
	output := filepath.Join(scratch, "scans")
	for _, dir := range []string{source, output} {
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
	}
	secret, password := "fixture-frontend-secret", "fixture-frontend-password"
	admin, err := s.EnsureAdministrator(t.Context(), "fixture-frontend-admin", auth.HashPassword(secret, password))
	if err != nil {
		t.Fatal(err)
	}
	if _, err := s.CreateAccount(t.Context(), "fixture-frontend-viewer", auth.HashPassword(secret, password), "viewer"); err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProject(t.Context(), "Fixture 前端完整性项目", admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	empty, err := s.CreateProject(t.Context(), "Fixture 无资产项目", admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	var providerRequests atomic.Int64
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providerRequests.Add(1)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/models":
			fmt.Fprint(w, `{"data":[{"id":"fixture-ui-model"}]}`)
		case "/responses", "/chat/completions":
			fmt.Fprint(w, `{"fixtureOnly":true,"output":[]}`)
		default:
			http.Error(w, "fixture provider does not implement this endpoint", 404)
		}
	}))
	defer provider.Close()
	t.Setenv("AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	encrypted, err := cryptokey.Encrypt("fixture-ui-never-valid-provider-key")
	if err != nil {
		t.Fatal(err)
	}
	model, err := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixture UI 模型（无真实调用）", Provider: "custom", Model: "fixture-ui-model", BaseURL: provider.URL, WireAPI: "responses", ApiKeyEncrypted: encrypted})
	if err != nil {
		t.Fatal(err)
	}
	if err := s.SetModelConfigEnabled(t.Context(), model.ID, true); err != nil {
		t.Fatal(err)
	}
	var historyID int64
	for n := 1; n <= 2; n++ {
		id, err := s.InsertPending(t.Context(), store.Task{ScanNo: fmt.Sprintf("fixture-ui-history-%d", n), SourcePath: "/synthetic/frontend/source", Provider: "custom", Model: model.Model, ModelConfigID: model.ID, ProjectID: project.ID, ScanMode: "standard"})
		if err != nil {
			t.Fatal(err)
		}
		claimed, err := s.ClaimExecution(t.Context(), "fixture-history-record-only", store.ClaimLimits{Global: 2, PerModel: 2, PerProject: 2, Lease: time.Minute, OutputRoot: output})
		if err != nil || claimed == nil || claimed.ID != id {
			t.Fatal(err)
		}
		line := 3
		findings := []store.Finding{{ScanID: id, Fingerprint: "fixture-ui-finding", Title: "Fixture 合成证据：界面验收", Severity: "low", FilePath: "fixture.ts", StartLine: &line, CWE: []string{"CWE-20"}, Description: "仅用于界面验收的合成发现。", Evidence: "Fixture raw evidence", Remediation: "Synthetic suggestion only", Raw: json.RawMessage(`{"fixtureOnly":true,"evidence":[{"path":"fixture.ts","startLine":3,"code":"const fixtureOnly = true;","explanation":"Fixture 原始证据说明"}]}`)}}
		if err := os.MkdirAll(claimed.OutputPath, 0700); err != nil {
			t.Fatal(err)
		}
		if err := s.CompleteExecution(t.Context(), store.Completion{LeaseRef: store.LeaseRef{TaskID: id, Attempt: claimed.Attempt, AgentID: claimed.AgentID}, Status: "completed", Findings: findings}); err != nil {
			t.Fatal(err)
		}
		historyID = id
	}
	// Enough genuine stored rows to exercise the existing page transport.
	for n := 0; n < 51; n++ {
		id, err := s.InsertPending(t.Context(), store.Task{ScanNo: fmt.Sprintf("fixture-ui-page-%02d", n), SourcePath: fmt.Sprintf("/synthetic/page/%d", n), Provider: "custom", Model: model.Model, ScanMode: "standard"})
		if err != nil {
			t.Fatal(err)
		}
		if err := s.Finish(t.Context(), id, "completed", "", "", 0); err != nil {
			t.Fatal(err)
		}
	}
	archivePath := filepath.Join(scratch, "fixture-ui-source.zip")
	archive, err := os.Create(archivePath)
	if err != nil {
		t.Fatal(err)
	}
	zw := zip.NewWriter(archive)
	file, err := zw.Create("src/fixture.ts")
	if err != nil {
		t.Fatal(err)
	}
	fmt.Fprint(file, "// Synthetic frontend fixture; no real audit.\nexport const fixtureOnly = true;\n")
	if err := zw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := archive.Close(); err != nil {
		t.Fatal(err)
	}
	srv := &httpserver.Server{Store: s, Secret: secret, AdminUsername: admin.Username, AgentToken: "fixture-no-agent", SnapshotDir: source, Limits: snapshot.DefaultLimits(), QueueLimits: store.ClaimLimits{Global: 2, PerModel: 2, PerProject: 2, Lease: time.Minute, OutputRoot: output}}
	routes := srv.Routes()
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			routes.ServeHTTP(w, r)
			return
		}
		if info, err := os.Stat(filepath.Join(dist, filepath.Clean(r.URL.Path))); err == nil && !info.IsDir() {
			http.FileServer(http.Dir(dist)).ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dist, "index.html"))
	}))
	defer web.Close()
	command := exec.CommandContext(t.Context(), "node", "tests/frontend-web-acceptance.mjs")
	command.Dir = filepath.Join(root, "code-audit-web")
	// Reuse the browser installed for this fixture for genuine PDF exports.
	fixtureBrowser := os.Getenv("AXIOM_TEST_BROWSER_PATH")
	if fixtureBrowser == "" && existsFrontendFile("/Applications/Google Chrome.app/Contents/MacOS/Google Chrome") {
		fixtureBrowser = "/Applications/Google Chrome.app/Contents/MacOS/Google Chrome"
	}
	if fixtureBrowser == "" {
		lookup := exec.CommandContext(t.Context(), "node", "--input-type=module", "-e", "import {chromium} from 'playwright-core'; process.stdout.write(chromium.executablePath())")
		lookup.Dir = command.Dir
		if out, err := lookup.Output(); err == nil {
			fixtureBrowser = strings.TrimSpace(string(out))
		}
	}
	if !existsFrontendFile(fixtureBrowser) {
		t.Fatal("install a browser before full frontend/PDF acceptance")
	}
	t.Setenv("AXIOM_BROWSER", fixtureBrowser)
	command.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+web.URL, "AXIOM_TEST_FRONTEND_ARCHIVE="+archivePath, fmt.Sprintf("AXIOM_TEST_FRONTEND_PROJECT=%d", project.ID), fmt.Sprintf("AXIOM_TEST_FRONTEND_EMPTY_PROJECT=%d", empty.ID), fmt.Sprintf("AXIOM_TEST_FRONTEND_HISTORY=%d", historyID), "AXIOM_TEST_FRONTEND_PROVIDER="+provider.URL)
	if out, err := command.CombinedOutput(); err != nil {
		t.Fatalf("frontend browser: %v\n%s", err, out)
	} else {
		t.Log(string(out))
	}
	t.Logf("fixtureOnly=true realModelCalls=0 syntheticProviderRequests=%d", providerRequests.Load())
	history, err := s.Get(t.Context(), historyID)
	if err != nil || history.Status != "completed" || history.Attempt != 1 {
		t.Fatal("frontend acceptance changed historical execution")
	}
}

func existsFrontendFile(path string) bool {
	info, err := os.Stat(path)
	return err == nil && !info.IsDir()
}
