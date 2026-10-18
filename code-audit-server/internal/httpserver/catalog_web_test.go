package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/snapshot"
	"axiom/code-audit-server/internal/store"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"net/url"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// The fixture writes historical rows directly; no worker, SDK or model starts.
func TestCatalogPagesHTTPAndBrowser(t *testing.T) {
	s := inputPipelineStore(t)
	ctx := t.Context()
	scratch := t.TempDir()
	secret, password := "fixture-catalog-secret", "fixture-catalog-password"
	admin, err := s.EnsureAdministrator(ctx, "fixture-catalog-admin", auth.HashPassword(secret, password))
	if err != nil {
		t.Fatal(err)
	}
	if _, err = s.CreateAccount(ctx, "fixture-catalog-viewer", auth.HashPassword(secret, password), "viewer"); err != nil {
		t.Fatal(err)
	}
	project, err := s.CreateProject(ctx, "Fixture 历史分页项目", admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	other, err := s.CreateProject(ctx, "Fixture 第二项目", admin.ID)
	if err != nil {
		t.Fatal(err)
	}
	snapRoot, output := filepath.Join(scratch, "snapshots"), filepath.Join(scratch, "output")
	if err = os.MkdirAll(snapRoot, 0700); err != nil {
		t.Fatal(err)
	}
	var oldest, referenced *store.SnapshotRow
	for i := 0; i < 251; i++ {
		name := fmt.Sprintf("Fixture asset %03d.zip", i)
		if i == 0 {
			name = "Fixture 历史中文 100%_!.zip"
		}
		if i >= 2 && i <= 52 {
			name = fmt.Sprintf("Fixture disposable %03d.zip", i)
		}
		projectID := int64(0)
		if i < 120 {
			projectID = project.ID
		}
		if i >= 120 && i < 170 {
			projectID = other.ID
		}
		dir := filepath.Join(snapRoot, fmt.Sprintf("fixture-%03d", i))
		if err = os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		if err = os.WriteFile(filepath.Join(dir, "fixture.ts"), []byte("// Synthetic fixture; no audit.\n"), 0600); err != nil {
			t.Fatal(err)
		}
		item, e := s.InsertSnapshot(ctx, store.SnapshotRow{OriginalFilename: name, ArchiveSha256: fmt.Sprintf("fixture-archive-%03d", i), ContentHash: fmt.Sprintf("fixture-content-%03d", i), SnapshotPath: dir, FileCount: i + 1, TotalSize: int64(i + 1), ProjectID: projectID})
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			oldest = item
		}
		if i == 1 {
			referenced = item
		}
	}
	model, err := s.CreateModelConfig(ctx, store.ModelConfigRow{Name: "Fixture 无调用模型", Provider: "custom", Model: "fixture-only", BaseURL: "http://127.0.0.1:1", ApiKeyEncrypted: ""})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.SetModelConfigEnabled(ctx, model.ID, true); err != nil {
		t.Fatal(err)
	}
	var history int64
	for occurrence := 0; occurrence < 2; occurrence++ {
		id, e := s.InsertPending(ctx, store.Task{ScanNo: fmt.Sprintf("fixture-catalog-history-%d", occurrence), SourcePath: referenced.SnapshotPath, SourceSnapshotID: referenced.ID, ProjectID: project.ID, Provider: "custom", Model: "fixture-only", ScanMode: "standard"})
		if e != nil {
			t.Fatal(e)
		}
		items := []store.Finding{}
		for i := 0; i < 401; i++ {
			if occurrence == 1 && i > 0 {
				break
			}
			v := store.Finding{Fingerprint: fmt.Sprintf("fixture-pool-%03d", i), Title: fmt.Sprintf("Fixture result %03d", i), Severity: "high", Category: "fixture", FilePath: fmt.Sprintf("src/fixture-%03d.ts", i), Description: "仅用于分页回归的合成发现"}
			if i == 398 {
				v.Severity = "medium"
			}
			if i == 0 {
				line := 3
				v.Title = "Fixture 历史漏洞 中文 100%_!"
				v.Severity = "low"
				v.CWE = []string{"CWE-9999"}
				v.StartLine = &line
				v.Evidence = "Fixture original evidence"
				v.Raw = json.RawMessage(`{"fixtureOnly":true,"codeEvidence":[{"code":"historical fixture raw","path":"fixture.ts","startLine":3,"explanation":"Fixture original evidence"}]}`)
			}
			items = append(items, v)
		}
		if e = s.ReplaceFindings(ctx, id, items); e != nil {
			t.Fatal(e)
		}
		if e = s.Finish(ctx, id, "completed", "", "", len(items)); e != nil {
			t.Fatal(e)
		}
		if occurrence == 0 {
			old, e := s.ListFindings(ctx, id)
			if e != nil {
				t.Fatal(e)
			}
			for _, v := range old {
				if v.Fingerprint == "fixture-pool-000" {
					if _, e = s.SetFindingStatus(ctx, v.ID, "confirmed"); e != nil {
						t.Fatal(e)
					}
				}
			}
		}
		history = id
	}
	server := &httpserver.Server{Store: s, Secret: secret, AdminUsername: admin.Username, AgentToken: "fixture-no-agent", SnapshotDir: snapRoot, Limits: snapshot.DefaultLimits(), QueueLimits: store.ClaimLimits{Global: 1, PerProject: 1, PerModel: 1, Lease: time.Minute, OutputRoot: output}}
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	dist := filepath.Join(root, "code-audit-web/dist")
	routes := server.Routes()
	web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			routes.ServeHTTP(w, r)
			return
		}
		if info, e := os.Stat(filepath.Join(dist, filepath.Clean(r.URL.Path))); e == nil && !info.IsDir() {
			http.FileServer(http.Dir(dist)).ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dist, "index.html"))
	}))
	defer web.Close()
	status, body := doJSON(t, web.URL+"/api/v1/auth/login", "", map[string]string{"username": admin.Username, "password": password})
	var login struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err = json.Unmarshal([]byte(body), &login); err != nil || status != 200 || login.Data.Token == "" {
		t.Fatal(status, body, err)
	}
	read := func(path string, want int) json.RawMessage {
		t.Helper()
		code, raw := getJSON(t, web.URL+"/api/v1"+path, login.Data.Token)
		if code != want {
			t.Fatalf("%s: status %d, %s", path, code, raw)
		}
		var envelope struct {
			Data json.RawMessage `json:"data"`
		}
		if err = json.Unmarshal(raw, &envelope); err != nil {
			t.Fatal(err)
		}
		return envelope.Data
	}
	for _, tc := range []struct {
		path  string
		total int
	}{
		{"/snapshots?page=1&pageSize=50", 251}, {"/snapshots?page=999&pageSize=50&q=" + url.QueryEscape("100%_!"), 1},
		{"/snapshots?page=1&projectId=0", 81}, {"/snapshots?page=1&usage=referenced", 1}, {"/snapshots?page=1&usage=unused", 250},
		{"/findings?page=1", 401}, {"/findings?page=999&severity=low", 1}, {"/findings?page=1&status=confirmed", 1},
		{"/findings?page=1&q=cwe-9999", 1}, {"/findings?page=1&q=" + url.QueryEscape("100%_!"), 1},
		{fmt.Sprintf("/findings?page=1&sourceSnapshotId=%d", referenced.ID), 401},
	} {
		var got struct {
			Items                 []json.RawMessage `json:"items"`
			Total, Page, PageSize int
		}
		if err = json.Unmarshal(read(tc.path, 200), &got); err != nil || got.Total != tc.total || got.PageSize < 1 || len(got.Items) > got.PageSize {
			t.Fatalf("%s: %+v %v", tc.path, got, err)
		}
	}
	for _, path := range []string{"/snapshots?page=0", "/snapshots?page=1&pageSize=101", "/snapshots?page=1&usage=invalid", "/snapshots?page=1&projectId=-1", "/snapshots?page=1&sort=id%3BDROP", "/findings?page=1&status=resolved", "/findings?page=1&sourceSnapshotId=oops"} {
		read(path, 400)
	}
	for _, tc := range []struct {
		path string
		want int
	}{{"/snapshots", 200}, {"/snapshots?projectId=0", 200}, {"/findings", 300}, {"/findings?q=cwe-9999", 1}} {
		var items []json.RawMessage
		if err = json.Unmarshal(read(tc.path, 200), &items); err != nil || len(items) != tc.want {
			t.Fatalf("legacy array %s: %d, %v", tc.path, len(items), err)
		}
	}
	read(fmt.Sprintf("/scans/%d", history), 200)
	read(fmt.Sprintf("/scans/%d/findings", history), 200)
	if code, raw := getJSON(t, web.URL+fmt.Sprintf("/api/v1/scans/%d/report", history), login.Data.Token); code != 200 || !strings.Contains(string(raw), "Fixture 历史漏洞") {
		t.Fatal("historical report unreadable", code)
	}
	if os.Getenv("AXIOM_TEST_WEB_INPUT") == "1" {
		if !existsFrontendFile(filepath.Join(dist, "index.html")) {
			t.Fatal("build Web first")
		}
		command := exec.CommandContext(ctx, "node", "tests/catalog-web-acceptance.mjs")
		command.Dir = filepath.Join(root, "code-audit-web")
		command.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+web.URL, fmt.Sprintf("AXIOM_TEST_CATALOG_PROJECT=%d", project.ID), fmt.Sprintf("AXIOM_TEST_CATALOG_SNAPSHOT=%d", oldest.ID), fmt.Sprintf("AXIOM_TEST_CATALOG_HISTORY=%d", history))
		if out, e := command.CombinedOutput(); e != nil {
			t.Fatalf("catalog browser: %v\n%s", e, out)
		} else {
			t.Log(string(out))
		}
	}
	old, err := s.Get(ctx, history)
	if err != nil || old.Status != "completed" || old.Attempt != 0 {
		t.Fatal("historical execution changed", err)
	}
	// This fixture never creates executions, regardless of Web interactions.
	list, err := s.ListScans(ctx, 0)
	if err != nil || len(list) != 2 {
		t.Fatal("browser started an audit", err)
	}
}
