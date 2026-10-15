package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"bytes"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

type storageFixture struct {
	s           *store.Store
	server      *httpserver.Server
	web         *httptest.Server
	token, root string
}

func storageSetup(t *testing.T, pg bool) *storageFixture {
	t.Helper()
	root := t.TempDir()
	var s *store.Store
	var e error
	if pg {
		s = inputPipelineStore(t)
	} else {
		s, e = store.Open(filepath.Join(root, "test.db"))
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { s.Close() })
	}
	if _, e = s.EnsureAdministrator(t.Context(), "input-fixture-admin", auth.HashPassword("fixture-storage-secret", "fixture-password")); e != nil {
		t.Fatal(e)
	}
	scans, snaps := filepath.Join(root, "scans"), filepath.Join(root, "snapshots")
	os.MkdirAll(scans, 0700)
	os.MkdirAll(snaps, 0700)
	server := &httpserver.Server{Store: s, Secret: "fixture-storage-secret", SnapshotDir: snaps, QueueLimits: store.ClaimLimits{Global: 2, PerModel: 2, PerProject: 2, Lease: time.Minute, OutputRoot: scans, MaxRecoveries: 2}}
	web := httptest.NewServer(server.Routes())
	t.Cleanup(web.Close)
	f := &storageFixture{s, server, web, "", root}
	f.token = login(t, web.URL, "input-fixture-admin", "fixture-password")
	return f
}
func storageRequest(t *testing.T, f *storageFixture, method, path string, body any, want int) json.RawMessage {
	t.Helper()
	raw, _ := json.Marshal(body)
	request, _ := http.NewRequest(method, f.web.URL+path, bytes.NewReader(raw))
	request.Header.Set("Authorization", "Bearer "+f.token)
	request.Header.Set("Content-Type", "application/json")
	res, e := http.DefaultClient.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	b, _ := io.ReadAll(res.Body)
	if res.StatusCode != want {
		t.Fatalf("%s %s: HTTP %d want %d: %s", method, path, res.StatusCode, want, b)
	}
	var v struct {
		Data json.RawMessage `json:"data"`
	}
	json.Unmarshal(b, &v)
	return v.Data
}
func fixtureDir(t *testing.T, root, name string) string {
	t.Helper()
	path := filepath.Join(root, name)
	os.MkdirAll(path, 0700)
	if e := os.WriteFile(filepath.Join(path, "proof.txt"), []byte("Synthetic fixture; never a real audit."), 0600); e != nil {
		t.Fatal(e)
	}
	old := time.Now().Add(-72 * time.Hour)
	os.Chtimes(path, old, old)
	return path
}
func previewJob(t *testing.T, f *storageFixture, kind string, id int64) contracts.CleanupJob {
	t.Helper()
	raw := storageRequest(t, f, "POST", "/api/v1/storage/cleanup", map[string]any{"kind": kind, "resourceId": id}, 200)
	v, e := contracts.DecodeCleanupJob(raw, nil)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func runJob(t *testing.T, f *storageFixture, j contracts.CleanupJob, want int) contracts.CleanupJob {
	t.Helper()
	storageRequest(t, f, "POST", "/api/v1/storage/cleanup/"+string(j.ID)+"/execute", map[string]any{"revision": j.Revision}, want)
	raw := storageRequest(t, f, "GET", "/api/v1/storage/cleanup/"+string(j.ID), nil, 200)
	v, e := contracts.DecodeCleanupJob(raw, nil)
	if e != nil {
		t.Fatal(e)
	}
	return v
}
func legacyStorageTask(t *testing.T, f *storageFixture, name string) (int64, string) {
	t.Helper()
	path := fixtureDir(t, f.server.QueueLimits.OutputRoot, name)
	id, e := f.s.InsertPending(t.Context(), store.Task{ScanNo: "fixture-storage-" + name, SourcePath: "/synthetic/source", Provider: "openai", Model: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	if e = f.s.SetOutputPath(t.Context(), id, path); e != nil {
		t.Fatal(e)
	}
	if e = f.s.Finish(t.Context(), id, "completed", "", "", 0); e != nil {
		t.Fatal(e)
	}
	return id, path
}

func TestStoragePreviewProtectionAndChangedReferences(t *testing.T) {
	f := storageSetup(t, false)
	id, report := legacyStorageTask(t, f, "history")
	orphan := fixtureDir(t, f.server.QueueLimits.OutputRoot, "old-orphan")
	unregistered := fixtureDir(t, f.server.QueueLimits.OutputRoot, "unregistered-report")
	os.WriteFile(filepath.Join(unregistered, "result.json"), []byte(`{"fixtureOnly":true}`), 0600)
	old := time.Now().Add(-72 * time.Hour)
	os.Chtimes(unregistered, old, old)
	recent := fixtureDir(t, f.server.QueueLimits.OutputRoot, "recent")
	os.Chtimes(recent, time.Now(), time.Now())
	snapPath := fixtureDir(t, f.server.SnapshotDir, "registered")
	_, e := f.s.InsertSnapshot(t.Context(), store.SnapshotRow{OriginalFilename: "fixture.zip", ArchiveSha256: "fixture-hash", ContentHash: "fixture-content", SnapshotPath: snapPath})
	if e != nil {
		t.Fatal(e)
	}
	j := previewJob(t, f, "orphans", 0)
	states := map[string]contracts.CleanupItemState{}
	for _, i := range j.Items {
		states[string(i.RelativePath)] = i.State
	}
	if states["history"] != "protected" || states["registered"] != "protected" || states["recent"] != "protected" || states["old-orphan"] != "planned" {
		t.Fatal(j)
	}
	if states["unregistered-report"] != "protected" {
		t.Fatal("missing database linkage discarded old evidence", j)
	}
	j = runJob(t, f, j, 200)
	if j.State != "completed" {
		t.Fatal(j)
	}
	if _, e = os.Stat(orphan); !os.IsNotExist(e) {
		t.Fatal("orphan retained", e)
	}
	if _, e = os.ReadFile(filepath.Join(report, "proof.txt")); e != nil {
		t.Fatal("historical proof deleted", e)
	}
	if _, e = f.s.Get(t.Context(), id); e != nil {
		t.Fatal(e)
	}
	candidate := fixtureDir(t, f.server.QueueLimits.OutputRoot, "new-reference")
	j = previewJob(t, f, "orphans", 0)
	newID, e := f.s.InsertPending(t.Context(), store.Task{ScanNo: "fixture-reference", SourcePath: candidate, Provider: "openai", Model: "fixture"})
	if e != nil || newID < 1 {
		t.Fatal(e)
	}
	runJob(t, f, j, 409)
	if _, e = os.Stat(candidate); e != nil {
		t.Fatal("new reference lost", e)
	}
}
func TestStorageRecoverableDeleteAndUnknownCosts(t *testing.T) {
	f := storageSetup(t, false)
	id, path := legacyStorageTask(t, f, "delete")
	j := previewJob(t, f, "scan", id)
	f.server.StoragePurge = func(context.Context, string, string) error { return errors.New("synthetic disk I/O failure") }
	j = runJob(t, f, j, 503)
	if j.State != "failed" || j.Items[0].State != "quarantined" {
		t.Fatal(j)
	}
	if _, e := f.s.Get(t.Context(), id); e != nil {
		t.Fatal("metadata deleted before files", e)
	}
	if _, e := f.s.Retry(t.Context(), id); !errors.Is(e, store.ErrStorageProtected) {
		t.Fatal("retry raced cleanup", e)
	}
	f.web.Close()
	next := &httpserver.Server{Store: f.s, Secret: f.server.Secret, SnapshotDir: f.server.SnapshotDir, QueueLimits: f.server.QueueLimits}
	f.server = next
	f.web = httptest.NewServer(next.Routes())
	t.Cleanup(f.web.Close)
	j = runJob(t, f, j, 200)
	if j.State != "completed" {
		t.Fatal(j)
	}
	if _, e := f.s.Get(t.Context(), id); e == nil {
		t.Fatal("metadata retained after purge")
	}
	if _, e := os.Stat(path); !os.IsNotExist(e) {
		t.Fatal(e)
	}
	runJob(t, f, j, 200)
	// Persist explicit unknown-cost evidence in the same disposable SQLite DB.
	id, path = legacyStorageTask(t, f, "unknown")
	dbPath := filepath.Join(f.root, "test.db")
	db, e := sql.Open("sqlite", dbPath)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(`INSERT INTO task_budgets(task_id,total_micros) VALUES(?,5000000)`, id); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO task_cost_attempts(task_id,attempt,state,updated_at) VALUES(?,1,'unknown','2026-10-15T00:00:00Z')`, id); e != nil {
		t.Fatal(e)
	}
	storageRequest(t, f, "POST", "/api/v1/storage/cleanup", map[string]any{"kind": "scan", "resourceId": id}, 409)
	storageRequest(t, f, "DELETE", "/api/v1/scans/"+strconv.FormatInt(id, 10), nil, 409)
	j = previewJob(t, f, "orphans", 0)
	for _, i := range j.Items {
		if i.RelativePath == "unknown" && i.Reason != "unknown_cost" {
			t.Fatal(i)
		}
	}
	if _, e = os.Stat(path); e != nil {
		t.Fatal(e)
	}
}
func TestStorageLostRenameACKPreservesRecreatedSource(t *testing.T) {
	f := storageSetup(t, false)
	path := fixtureDir(t, f.server.QueueLimits.OutputRoot, "recreated")
	j := previewJob(t, f, "orphans", 0)
	f.server.StorageCheckpoint = func(string) error { return errors.New("synthetic crash after rename") }
	j = runJob(t, f, j, 503)
	if j.State != "failed" || j.Items[0].State != "planned" {
		t.Fatal(j)
	}
	fixtureDir(t, f.server.QueueLimits.OutputRoot, "recreated")
	_, e := f.s.InsertPending(t.Context(), store.Task{ScanNo: "fixture-recreated", SourcePath: path, Provider: "openai", Model: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	f.server.StorageCheckpoint = nil
	j = runJob(t, f, j, 200)
	if j.State != "completed" {
		t.Fatal(j)
	}
	if _, e = os.Stat(filepath.Join(path, "proof.txt")); e != nil {
		t.Fatal("recreated source was deleted", e)
	}
}
func TestStorageCapacityAndWeb(t *testing.T) {
	f := storageSetup(t, true)
	id, _ := legacyStorageTask(t, f, "browser-history")
	fixtureDir(t, f.server.QueueLimits.OutputRoot, "browser-orphan")
	pendingID, e := f.s.InsertPending(t.Context(), store.Task{ScanNo: "fixture-capacity-pending", SourcePath: "/fixture/pending", Provider: "openai", Model: "fixture"})
	if e != nil {
		t.Fatal(e)
	}
	f.server.StorageSpace = func(string) (int64, int64, error) { return 1, 1 << 40, nil }
	raw := storageRequest(t, f, "GET", "/api/v1/storage", nil, 200)
	snapshot, e := contracts.DecodeStorageSnapshot(raw, nil)
	if e != nil || snapshot.State != "low" {
		t.Fatal(snapshot, e)
	}
	// Exercise genuine HTTP admission through its original transport and auth.
	f.server.AgentToken = "fixture-storage-agent"
	f.web.Close()
	f.web = httptest.NewServer(f.server.Routes())
	t.Cleanup(f.web.Close)
	request, _ := http.NewRequest("POST", f.web.URL+"/api/internal/executions/claim", bytes.NewBufferString(`{"agentId":"storage-fixture"}`))
	request.Header.Set("Authorization", "Bearer fixture-storage-agent")
	res, e := http.DefaultClient.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	if res.StatusCode != 200 {
		t.Fatal(res.StatusCode)
	}
	task, _ := f.s.Get(t.Context(), pendingID)
	if task.Status != "pending" || task.Attempt != 0 {
		t.Fatal("capacity claim started work", task)
	}
	raw = storageRequest(t, f, "GET", "/api/v1/executions/overview?taskId="+strconv.FormatInt(pendingID, 10), nil, 200)
	if !bytes.Contains(raw, []byte("storage_low")) {
		t.Fatal(string(raw))
	}
	f.server.StorageSpace = func(string) (int64, int64, error) { return 0, 0, errors.New("synthetic probe outage") }
	raw = storageRequest(t, f, "GET", "/api/v1/storage", nil, 200)
	snapshot, e = contracts.DecodeStorageSnapshot(raw, nil)
	if e != nil || snapshot.State != "unknown" || snapshot.Roots[0].AvailableBytes != nil {
		t.Fatal(snapshot, e)
	}
	f.server.StorageSpace = nil
	f.s.RequestCancel(t.Context(), pendingID)
	if os.Getenv("AXIOM_TEST_WEB_INPUT") == "1" {
		root, _ := filepath.Abs("../../..")
		files := http.FileServer(http.Dir(filepath.Join(root, "code-audit-web/dist")))
		routes := f.server.Routes()
		web := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if len(r.URL.Path) >= 4 && r.URL.Path[:4] == "/api" {
				routes.ServeHTTP(w, r)
				return
			}
			if !bytes.Contains([]byte(r.URL.Path), []byte(".")) {
				r.URL.Path = "/"
			}
			files.ServeHTTP(w, r)
		}))
		defer web.Close()
		cmd := exec.Command("node", "tests/storage-web-acceptance.mjs")
		cmd.Dir = filepath.Join(root, "code-audit-web")
		cmd.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+web.URL, "AXIOM_TEST_STORAGE_TASK="+fmt.Sprint(id))
		out, e := cmd.CombinedOutput()
		if e != nil {
			t.Fatalf("storage browser: %v %s", e, out)
		}
		t.Log(string(out))
	}
}

func TestStorageChangedPlanCanBeAbandonedAndRepreviewed(t *testing.T) {
	f := storageSetup(t, false)
	id, path := legacyStorageTask(t, f, "changed")
	j := previewJob(t, f, "scan", id)
	if e := os.WriteFile(filepath.Join(path, "proof.txt"), []byte("new synthetic evidence"), 0600); e != nil {
		t.Fatal(e)
	}
	j = runJob(t, f, j, 409)
	if j.State != "failed" {
		t.Fatal(j)
	}
	storageRequest(t, f, "POST", "/api/v1/storage/cleanup/"+string(j.ID)+"/cancel", map[string]any{"revision": j.Revision}, 200)
	next := previewJob(t, f, "scan", id)
	if next.ID == j.ID || next.Items[0].Fingerprint == j.Items[0].Fingerprint {
		t.Fatal("preview failed to refresh evidence", next)
	}
	if _, e := os.Stat(filepath.Join(path, "proof.txt")); e != nil {
		t.Fatal(e)
	}
	// A pre-existing symlink is never followed, even when its destination is
	// another managed report or outside the managed roots.
	outside := fixtureDir(t, t.TempDir(), "retain")
	if e := os.Symlink(outside, filepath.Join(f.server.QueueLimits.OutputRoot, "unsafe-link")); e != nil {
		t.Fatal(e)
	}
	plan := previewJob(t, f, "orphans", 0)
	for _, item := range plan.Items {
		if item.RelativePath == "unsafe-link" && item.State != "protected" {
			t.Fatal(item)
		}
	}
}

func TestStorageParentSourceReferenceProtectsAllDescendants(t *testing.T) {
	f := storageSetup(t, false)
	fixtureDir(t, f.server.QueueLimits.OutputRoot, "parent-protected")
	before := previewJob(t, f, "orphans", 0)
	if _, e := f.s.InsertPending(t.Context(), store.Task{ScanNo: "fixture-parent-scope", SourcePath: f.server.QueueLimits.OutputRoot, Provider: "openai", Model: "fixture"}); e != nil {
		t.Fatal(e)
	}
	runJob(t, f, before, 409)
	after := previewJob(t, f, "orphans", 0)
	for _, item := range after.Items {
		if item.RelativePath == "parent-protected" && item.State != "protected" {
			t.Fatal("parent source reference was ignored", item)
		}
	}
}
