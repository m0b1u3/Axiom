package httpserver_test

import (
	"archive/zip"
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/sourceidentity"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func gitPipelineArchive(t *testing.T, scratch string) (archive, base, head string) {
	t.Helper()
	source := filepath.Join(scratch, "submitted-repository")
	if e := os.MkdirAll(source, 0700); e != nil {
		t.Fatal(e)
	}
	git := func(args ...string) string {
		t.Helper()
		c := exec.Command("git", append([]string{"-c", "maintenance.auto=false", "-c", "gc.auto=0", "-C", source}, args...)...)
		c.Env = append(os.Environ(), "GIT_CONFIG_NOSYSTEM=1", "GIT_CONFIG_GLOBAL="+os.DevNull, "GIT_AUTHOR_NAME=Fixture", "GIT_AUTHOR_EMAIL=fixture@example.invalid", "GIT_COMMITTER_NAME=Fixture", "GIT_COMMITTER_EMAIL=fixture@example.invalid")
		out, e := c.CombinedOutput()
		if e != nil {
			t.Fatal(e, string(out))
		}
		return strings.TrimSpace(string(out))
	}
	write := func(name, content string) {
		t.Helper()
		p := filepath.Join(source, name)
		if e := os.MkdirAll(filepath.Dir(p), 0700); e != nil {
			t.Fatal(e)
		}
		if e := os.WriteFile(p, []byte(content), 0600); e != nil {
			t.Fatal(e)
		}
	}
	git("init", "-q", "--template=", "--initial-branch=main", "--object-format=sha1")
	write("fixture.ts", "// Fixtures / Mock only\nconst query = 'base-only';\ndatabase.execute(query);\n")
	write("deleted.ts", "// deleted in HEAD\n")
	write("old-name.ts", "// exact rename fixture content\n")
	git("add", ".")
	git("commit", "-qm", "base")
	base = git("rev-parse", "HEAD")
	write("fixture.ts", "// Fixtures / Mock only\nconst query = request.query.q;\ndatabase.execute(query);\n")
	os.Remove(filepath.Join(source, "deleted.ts"))
	os.Rename(filepath.Join(source, "old-name.ts"), filepath.Join(source, "new-name.ts"))
	write("binary.bin", "\x00\x01\x02\x03")
	write("源码/[literal].ts", "// Unicode and literal pathspec fixture\n")
	write("[glob]*.ts", "// literal Git pathspec, not a wildcard\n")
	var large strings.Builder
	for i := 0; i < 6000; i++ {
		fmt.Fprintf(&large, "// fixture %d %x\n", i, sha256.Sum256([]byte(fmt.Sprint(i))))
	}
	write("large.ts", large.String())
	for i := 0; i < 55; i++ {
		write(fmt.Sprintf("pages/%03d.ts", i), fmt.Sprintf("// fixture %d\n", i))
	}
	git("add", ".")
	git("commit", "-qm", "head")
	head = git("rev-parse", "HEAD")
	git("config", "remote.origin.url", "https://fixture:private-upload-token@example.invalid/repo")
	write("fixture.ts", "uncommitted worktree must not survive")
	write(".git/hooks/post-checkout", "#!/bin/sh\nexit 99\n")
	archive = filepath.Join(scratch, "git-repository.zip")
	out, e := os.Create(archive)
	if e != nil {
		t.Fatal(e)
	}
	z := zip.NewWriter(out)
	e = filepath.Walk(source, func(p string, info os.FileInfo, e error) error {
		if e != nil {
			return e
		}
		if info.IsDir() {
			return nil
		}
		rel, _ := filepath.Rel(source, p)
		h := &zip.FileHeader{Name: "repository/" + filepath.ToSlash(rel), Method: zip.Deflate}
		h.SetMode(info.Mode())
		w, e := z.CreateHeader(h)
		if e != nil {
			return e
		}
		f, e := os.Open(p)
		if e != nil {
			return e
		}
		_, e = io.Copy(w, f)
		f.Close()
		return e
	})
	if e != nil {
		t.Fatal(e)
	}
	if e = z.Close(); e != nil {
		t.Fatal(e)
	}
	if e = out.Close(); e != nil {
		t.Fatal(e)
	}
	return
}
func uploadGitFixture(t *testing.T, url, token, archive, base, head string) (int, []byte) {
	t.Helper()
	var body bytes.Buffer
	m := multipart.NewWriter(&body)
	w, e := m.CreateFormFile("file", filepath.Base(archive))
	if e != nil {
		t.Fatal(e)
	}
	f, e := os.Open(archive)
	if e != nil {
		t.Fatal(e)
	}
	_, e = io.Copy(w, f)
	f.Close()
	if e != nil {
		t.Fatal(e)
	}
	m.WriteField("base", base)
	m.WriteField("head", head)
	m.Close()
	r, e := http.NewRequest("POST", url+"/api/v1/git-snapshots", &body)
	if e != nil {
		t.Fatal(e)
	}
	r.Header.Set("Content-Type", m.FormDataContentType())
	if token != "" {
		r.Header.Set("Authorization", "Bearer "+token)
	}
	response, e := http.DefaultClient.Do(r)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	raw, e := io.ReadAll(response.Body)
	if e != nil {
		t.Fatal(e)
	}
	return response.StatusCode, raw
}
func TestGitDiffPipeline(t *testing.T) {
	root, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, "code-audit-scanner/dist/main.js")); e != nil {
		t.Skip("build Scanner before Git diff acceptance")
	}
	s := inputPipelineStore(t)
	scratch := t.TempDir()
	t.Setenv("AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	agent := filepath.Join(scratch, "agent")
	build := exec.Command("go", "build", "-o", agent, "./cmd/agent")
	build.Dir = filepath.Join(root, "code-audit-agent")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatalf("build Agent: %v %s", e, out)
	}
	secret := "fixture-git-secret"
	if _, e = s.EnsureAdministrator(t.Context(), "input-fixture-admin", auth.HashPassword(secret, "fixture-password")); e != nil {
		t.Fatal(e)
	}
	server := &httpserver.Server{Store: s, Secret: secret, AgentToken: "fixture-git-agent-token", SnapshotDir: filepath.Join(scratch, "uploads"), QueueLimits: store.ClaimLimits{Global: 4, PerModel: 4, PerProject: 4, Lease: time.Minute, OutputRoot: filepath.Join(scratch, "output"), MaxRecoveries: 0}}
	routes := server.Routes()
	dist := filepath.Join(root, "code-audit-web/dist")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			routes.ServeHTTP(w, r)
			return
		}
		if _, e := os.Stat(filepath.Join(dist, filepath.Clean(r.URL.Path))); e == nil && r.URL.Path != "/" {
			http.FileServer(http.Dir(dist)).ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dist, "index.html"))
	}))
	defer ts.Close()
	token := login(t, ts.URL, "input-fixture-admin", "fixture-password")
	archive, base, head := gitPipelineArchive(t, scratch)
	code, raw := uploadGitFixture(t, ts.URL, token, archive, "HEAD~1", "main")
	if code != 200 {
		t.Fatal(code, string(raw))
	}
	var imported struct {
		Data struct {
			Snapshot store.SnapshotRow
			Target   contracts.GitDiffTarget
		}
	}
	if e = json.Unmarshal(raw, &imported); e != nil {
		t.Fatal(e)
	}
	snap, target := imported.Data.Snapshot, imported.Data.Target
	if !snap.GitDiff || target.BaseCommit != base || target.HeadCommit != head {
		t.Fatal("missing immutable Git identity", imported)
	}
	if bytes, e := os.ReadFile(filepath.Join(snap.SnapshotPath, "fixture.ts")); e != nil || !strings.Contains(string(bytes), "request.query.q") {
		t.Fatal("dirty source survived", e)
	}
	beforeHash, _, _, e := sourceidentity.TreeHash(t.Context(), snap.SnapshotPath)
	if e != nil || beforeHash != snap.ContentHash {
		t.Fatal(e)
	}
	previewURL := fmt.Sprintf("%s/api/v1/snapshots/%d/git-diff", ts.URL, snap.ID)
	var preview contracts.GitDiffPreview
	t.Run("preview-pagination-raw-patch-and-identity", func(t *testing.T) {
		code, body := getJSON(t, previewURL, token)
		if code != 200 {
			t.Fatal(code, string(body))
		}
		var v struct{ Data json.RawMessage }
		json.Unmarshal(body, &v)
		var e error
		preview, e = contracts.DecodeGitDiffPreview(v.Data, nil)
		if e != nil || len(preview.Files) != 50 || preview.Total != 62 || preview.Target != target {
			t.Fatal(preview.Total, len(preview.Files), e)
		}
		all := append([]contracts.GitDiffFile{}, preview.Files...)
		code, body = getJSON(t, previewURL+"?page=2", token)
		if code != 200 {
			t.Fatal(code, string(body))
		}
		json.Unmarshal(body, &v)
		p, e := contracts.DecodeGitDiffPreview(v.Data, nil)
		if e != nil || len(p.Files) != 12 || p.Files[0].Index != 50 {
			t.Fatal(p, e)
		}
		all = append(all, p.Files...)
		for _, f := range all {
			if f.NewPath == nil && f.OldPath == nil {
				t.Fatal("missing file identity")
			}
			name := f.NewPath
			if name == nil {
				name = f.OldPath
			}
			if string(*name) != "fixture.ts" && string(*name) != "large.ts" && string(*name) != "binary.bin" && string(*name) != "[glob]*.ts" && f.Status != "deleted" && f.Status != "renamed" {
				continue
			}
			code, body = getJSON(t, fmt.Sprintf("%s/files/%d", previewURL, f.Index), token)
			if code != 200 {
				t.Fatal(code, string(body))
			}
			json.Unmarshal(body, &v)
			content, e := contracts.DecodeGitDiffContent(v.Data, nil)
			if e != nil || content.Target != target || content.ContentHash != contracts.Digest(snap.ContentHash) {
				t.Fatal(content, e)
			}
			switch string(*name) {
			case "fixture.ts":
				if !strings.Contains(content.Patch, "-const query = 'base-only';") || !strings.Contains(content.Patch, "+const query = request.query.q;") {
					t.Fatal(content.Patch)
				}
			case "large.ts":
				if !content.Truncated || len(content.Patch) > 262144 || content.Reason == nil {
					t.Fatal("large patch unbounded")
				}
			case "binary.bin":
				if content.Reason == nil || !strings.Contains(*content.Reason, "二进制") {
					t.Fatal(content)
				}
			case "[glob]*.ts":
				if strings.Count(content.Patch, "diff --git") != 1 {
					t.Fatal("filename interpreted as pathspec")
				}
			}
		}
		for _, suffix := range []string{"?page=0", "?page=9223372036854775807", "/files/-1"} {
			if c, _ := getJSON(t, previewURL+suffix, token); c != 400 {
				t.Fatal(suffix, c)
			}
		}
		if c, _ := getJSON(t, previewURL+"?page=9007199254740991", token); c != 200 {
			t.Fatal("large safe page overflowed", c)
		}
	})
	t.Run("imports-reject-unsafe-refs-and-unauthed-writes", func(t *testing.T) {
		for _, pair := range [][2]string{{"HEAD", "HEAD"}, {"HEAD~1", "--upload-pack=evil"}, {"HEAD~1", "missing"}} {
			if c, _ := uploadGitFixture(t, ts.URL, token, archive, pair[0], pair[1]); c != 400 {
				t.Fatal(pair, c)
			}
		}
		if c, _ := uploadGitFixture(t, ts.URL, "", archive, base, head); c != 401 {
			t.Fatal(c)
		}
		if c, _ := getJSON(t, previewURL, ""); c != 401 {
			t.Fatal(c)
		}
		if c, body := doJSON(t, ts.URL+"/api/v1/users", token, map[string]any{"username": "git-fixture-viewer", "password": "fixture-password", "role": "viewer"}); c != 200 {
			t.Fatal(c, body)
		}
		viewer := login(t, ts.URL, "git-fixture-viewer", "fixture-password")
		if c, _ := getJSON(t, previewURL, viewer); c != 200 {
			t.Fatal(c)
		}
		if c, _ := uploadGitFixture(t, ts.URL, viewer, archive, base, head); c != 403 {
			t.Fatal(c)
		}
		if c, body := uploadGitFixture(t, ts.URL, token, archive, base, head); c != 200 || !strings.Contains(string(body), fmt.Sprintf(`"id":%d`, snap.ID)) {
			t.Fatal("equivalent refs not deduplicated", c, string(body))
		}
	})
	encrypted, e := cryptokey.Encrypt("fixture-only-no-real-model")
	if e != nil {
		t.Fatal(e)
	}
	model, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Pricing: inputFixturePrice(), Name: "Git diff fixture model", Provider: "openai", Model: "fixture-git-model", ApiKeyEncrypted: encrypted})
	if e != nil {
		t.Fatal(e)
	}
	payload := map[string]any{"sourceSnapshotId": snap.ID, "modelConfigId": model.ID, "scanMode": "standard", "maxCostUsd": 2, "maxTotalCostUsd": 4}
	for _, bad := range []map[string]any{{"scanMode": "deep"}, {"includePaths": []string{"fixture.ts"}}} {
		v := map[string]any{}
		for k, x := range payload {
			v[k] = x
		}
		for k, x := range bad {
			v[k] = x
		}
		if c, b := doJSON(t, ts.URL+"/api/v1/scans", token, v); c != 400 {
			t.Fatal(c, b)
		}
	}
	calls := []string{}
	run := func(name, scenario string) *store.Task {
		t.Helper()
		task := knowledgeTask(t, ts.URL, token, payload)
		if task.InputContractVersion != 4 || task.GitTarget == nil || *task.GitTarget != target {
			t.Fatal("task target lost", task)
		}
		capture := filepath.Join(scratch, name+"-sdk.json")
		calls = append(calls, capture)
		cmd := exec.Command(agent, "--once")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "AXIOM_SERVER_URL="+ts.URL, "AXIOM_AGENT_TOKEN="+server.AgentToken, "AXIOM_AGENT_ID=fixture-git-agent", "AXIOM_AGENT_CONCURRENCY=1", "AXIOM_SCANNER_DIR="+filepath.Join(root, "code-audit-scanner"), "AXIOM_SCAN_MOCK=0", "AXIOM_INPUT_TEST_CAPTURE="+capture, "AXIOM_RESULT_TEST_SCENARIO="+scenario, "NODE_OPTIONS=--import="+filepath.Join(root, "contracts/tests/input-sdk-register.mjs"))
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("fixture Agent: %v %s", e, out)
		}
		task, e = s.Get(t.Context(), task.ID)
		if e != nil {
			t.Fatal(e)
		}
		return task
	}
	rich := run("rich", "git-evidence")
	if rich.Status != "completed" {
		t.Fatal(rich.Status, rich.ErrorMessage)
	}
	t.Run("real-adapter-receives-sdk-refs-and-private-source-copy", func(t *testing.T) {
		var capture struct {
			Source  string
			Options struct {
				Target struct{ Kind, Base, Head string }
			}
			LeakedServerCredentials bool
		}
		raw, e := os.ReadFile(calls[0])
		if e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(raw, &capture); e != nil {
			t.Fatal(e)
		}
		if capture.Options.Target.Kind != "refs" || capture.Options.Target.Base != base || capture.Options.Target.Head != head || capture.Source == snap.SnapshotPath || !strings.HasPrefix(capture.Source, rich.OutputPath) || capture.LeakedServerCredentials {
			t.Fatal("SDK input mismatch", capture)
		}
		current, _, _, e := sourceidentity.TreeHash(t.Context(), snap.SnapshotPath)
		if e != nil || current != beforeHash {
			t.Fatal("shared source modified", e)
		}
	})
	graphURL := fmt.Sprintf("%s/api/v1/scans/%d/attempts/1/evidence-graph?fingerprint=fixture%%3Aevidence-chain", ts.URL, rich.ID)
	t.Run("result-and-graph-bind-head-without-promoting-base-evidence", func(t *testing.T) {
		full, e := s.AttemptResult(t.Context(), rich.ID, 1)
		if e != nil || full.Result == nil {
			t.Fatal(e)
		}
		binding, e := contracts.DecodeGitDiffAssignment(full.Result.Extensions.Value["gitDiff"], &full.Result.Execution)
		if e != nil || binding.Target != target || binding.SourceContentHash != contracts.Digest(snap.ContentHash) {
			t.Fatal(binding, e)
		}
		code, body := getJSON(t, graphURL, token)
		if code != 200 {
			t.Fatal(code, string(body))
		}
		var v struct{ Data contracts.EvidenceGraph }
		if e = json.Unmarshal(body, &v); e != nil {
			t.Fatal(e)
		}
		observed, baseOnly := 0, 0
		for _, n := range v.Data.Nodes {
			if n.State == "observed" {
				observed++
				if !strings.Contains(n.StateReason, head) {
					t.Fatal("observation omitted HEAD revision")
				}
			}
			if n.SDKEvidenceID != nil && (*n.SDKEvidenceID == "E-base-only" || *n.SDKEvidenceID == "E-deleted") {
				baseOnly++
				if n.State == "observed" || n.State == "verified" {
					t.Fatal("base/deleted snippet promoted", n)
				}
			}
		}
		if observed != 2 || baseOnly != 2 {
			t.Fatal("head evidence mapping incomplete", observed, baseOnly)
		}
		raw, e := os.ReadFile(filepath.Join(rich.OutputPath, "normalized/result.json"))
		if e != nil {
			t.Fatal(e)
		}
		disk, e := contracts.DecodeScanResult(raw, &full.Result.Execution)
		if e != nil {
			t.Fatal(e)
		}
		a, _ := contracts.EncodeJSON(disk)
		b, _ := contracts.EncodeJSON(full.Result)
		if string(a) != string(b) {
			t.Fatal("cross-language result fields lost")
		}
	})
	t.Run("foreign-result-binding-rejected-at-store-boundary", func(t *testing.T) {
		db, e := sql.Open("pgx", os.Getenv("AXIOM_TEST_DATABASE_URL"))
		if e != nil {
			t.Fatal(e)
		}
		defer db.Close()
		var saved string
		if e = db.QueryRowContext(t.Context(), "SELECT result_json FROM scan_attempts WHERE task_id=$1 AND attempt=1", rich.ID).Scan(&saved); e != nil {
			t.Fatal(e)
		}
		for _, field := range []string{"result", "finding", "missing"} {
			full, e := contracts.DecodeExecutionCompletion([]byte(saved), nil)
			if e != nil {
				t.Fatal(e)
			}
			if field == "missing" {
				delete(full.Result.Value.Extensions.Value, "gitDiff")
			} else {
				where := full.Result.Value.Extensions.Value
				key := "gitDiff"
				if field == "finding" {
					where = full.Result.Value.Findings[0].Extensions.Value
					key = "axiomGitTarget"
				}
				binding, _ := contracts.DecodeGitDiffAssignment(where[key], nil)
				binding.Target.HeadCommit = base
				where[key], _ = contracts.EncodeJSON(binding)
			}
			if e = s.CompleteContractExecution(t.Context(), full); !errors.Is(e, store.ErrInvalidResult) {
				t.Fatal("foreign target accepted", field, e)
			}
		}
	})
	bad := run("wrong-native", "git-wrong-target")
	if bad.Status == "completed" || bad.FindingCount != 0 {
		t.Fatal("foreign native SDK target accepted", bad)
	}
	t.Run("tampered-frozen-source-blocks-preview", func(t *testing.T) {
		p := filepath.Join(snap.SnapshotPath, "fixture.ts")
		saved, e := os.ReadFile(p)
		if e != nil {
			t.Fatal(e)
		}
		defer os.WriteFile(p, saved, 0600)
		os.WriteFile(p, []byte("tamper"), 0600)
		if c, _ := getJSON(t, previewURL, token); c != 409 {
			t.Fatal("tamper was silently read", c)
		}
	})
	if os.Getenv("AXIOM_TEST_WEB_GIT_DIFF") == "1" {
		t.Run("browser-git-diff-workflow", func(t *testing.T) {
			ids, _ := json.Marshal(map[string]any{"rich": rich.ID, "snapshot": snap.ID, "model": model.ID, "base": base, "head": head})
			cmd := exec.Command("node", filepath.Join(root, "code-audit-web/tests/git-diff-web-acceptance.mjs"))
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+ts.URL, "AXIOM_TEST_GIT_IDS="+string(ids), "AXIOM_TEST_GIT_ARCHIVE="+archive, "AXIOM_TEST_AGENT_BIN="+agent, "AXIOM_TEST_AGENT_TOKEN="+server.AgentToken)
			if out, e := cmd.CombinedOutput(); e != nil {
				t.Fatalf("Git browser: %v %s", e, out)
			} else {
				t.Log(string(out))
			}
		})
	}
	for _, capture := range calls {
		assertSDKCallCount(t, capture, 1)
	}
}
