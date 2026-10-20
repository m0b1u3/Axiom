package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/sourceidentity"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
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

func TestEvidenceGraphPipeline(t *testing.T) {
	runEvidenceGraphPipeline(t, false)
}

func TestSDK050EvidenceCompatibilityPipeline(t *testing.T) {
	runEvidenceGraphPipeline(t, true)
}

func runEvidenceGraphPipeline(t *testing.T, sdk050 bool) {
	if sdk050 {
		t.Setenv("AXIOM_SDK050_FIXTURE", "1")
	}
	root, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, "code-audit-scanner/dist/main.js")); e != nil {
		t.Skip("build Scanner before graph acceptance")
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
	secret := "fixture-evidence-secret"
	if _, e = s.EnsureAdministrator(t.Context(), "input-fixture-admin", auth.HashPassword(secret, "fixture-password")); e != nil {
		t.Fatal(e)
	}
	server := &httpserver.Server{Store: s, Secret: secret, AgentToken: "fixture-evidence-agent-token", SnapshotDir: filepath.Join(scratch, "uploads"), QueueLimits: store.ClaimLimits{Global: 4, PerModel: 4, PerProject: 4, Lease: time.Minute, OutputRoot: filepath.Join(scratch, "output"), MaxRecoveries: 0}}
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
	source := filepath.Join(scratch, "source")
	os.MkdirAll(source, 0700)
	if e = os.WriteFile(filepath.Join(source, "fixture.ts"), []byte("// Fixtures / Mock only\nconst query = request.query.q;\ndatabase.execute(query);\n"), 0600); e != nil {
		t.Fatal(e)
	}
	hash, count, size, e := sourceidentity.TreeHash(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	snapshot, e := s.InsertSnapshot(t.Context(), store.SnapshotRow{OriginalFilename: "evidence-fixture.zip", ArchiveSha256: fmt.Sprintf("%x", sha256.Sum256([]byte(scratch))), ContentHash: hash, SnapshotPath: source, FileCount: count, TotalSize: size})
	if e != nil {
		t.Fatal(e)
	}
	encrypted, e := cryptokey.Encrypt("fixture-only-no-real-model")
	if e != nil {
		t.Fatal(e)
	}
	config, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixture evidence model", Provider: "openai", Model: "fixture-evidence-model", ApiKeyEncrypted: encrypted})
	if e != nil {
		t.Fatal(e)
	}
	db, e := sql.Open("pgx", os.Getenv("AXIOM_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	ids := map[string]int64{}
	calls := []string{}
	create := func(name string) *store.Task {
		t.Helper()
		task := knowledgeTask(t, ts.URL, token, map[string]any{"sourceSnapshotId": snapshot.ID, "modelConfigId": config.ID, "maxCostUsd": 2, "maxTotalCostUsd": 4})
		ids[name] = task.ID
		capture := filepath.Join(scratch, name+"-sdk.json")
		calls = append(calls, capture)
		cmd := exec.Command(agent, "--once")
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "AXIOM_SERVER_URL="+ts.URL, "AXIOM_AGENT_TOKEN="+server.AgentToken, "AXIOM_AGENT_ID=fixture-evidence-agent", "AXIOM_AGENT_CONCURRENCY=1", "AXIOM_SCANNER_DIR="+filepath.Join(root, "code-audit-scanner"), "AXIOM_SCAN_MOCK=0", "AXIOM_INPUT_TEST_CAPTURE="+capture, "AXIOM_RESULT_TEST_SCENARIO=evidence-chain", "NODE_OPTIONS=--import="+filepath.Join(root, "contracts/tests/input-sdk-register.mjs"))
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("fixture Agent: %v %s", e, out)
		}
		task, e = s.Get(t.Context(), task.ID)
		if e != nil || task.Status != "completed" {
			t.Fatal(task, e)
		}
		return task
	}
	rich := create("rich")
	legacy := create("legacy")
	graphURL := func(id int64, attempt int, fp string) string {
		return fmt.Sprintf("%s/api/v1/scans/%d/attempts/%d/evidence-graph?fingerprint=%s", ts.URL, id, attempt, url.QueryEscape(fp))
	}
	endpoint := graphURL(rich.ID, 1, "fixture:evidence-chain")
	t.Run("scanner-agent-server-portable-evidence-and-source-observation", func(t *testing.T) {
		archive, e := s.AttemptResult(t.Context(), rich.ID, 1)
		if e != nil || archive.Result == nil {
			t.Fatal(archive, e)
		}
		original, e := os.ReadFile(filepath.Join(rich.OutputPath, "normalized/result.json"))
		if e != nil {
			t.Fatal(e)
		}
		disk, e := contracts.DecodeScanResult(original, &archive.Result.Execution)
		if e != nil {
			t.Fatal(e)
		}
		a, _ := contracts.EncodeJSON(disk)
		b, _ := contracts.EncodeJSON(archive.Result)
		if string(a) != string(b) {
			t.Fatal("Agent or Server changed source evidence")
		}
		facts, e := contracts.FindingEvidenceFor(disk.Findings[0], disk.Execution, hash)
		if e != nil || facts == nil {
			t.Fatal("SDK refs not persisted", e)
		}
		code, body := getJSON(t, endpoint, token)
		if code != 200 {
			t.Fatal(code, string(body))
		}
		var response struct {
			Data contracts.EvidenceGraph `json:"data"`
		}
		if e = json.Unmarshal(body, &response); e != nil {
			t.Fatal(e)
		}
		raw, _ := contracts.EncodeJSON(response.Data)
		if e = contracts.ValidateJSON(contracts.KindEvidenceGraph, raw, &disk.Execution); e != nil {
			t.Fatal(e)
		}
		observed, supports := 0, 0
		for _, n := range response.Data.Nodes {
			if n.State == "observed" {
				observed++
			}
			if n.Kind == "validation" && n.State != "inferred" {
				t.Fatal("SDK verified status promoted")
			}
		}
		for _, e := range response.Data.Edges {
			if e.Kind == "supports" {
				supports++
			}
		}
		if observed != 2 || supports < 5 || response.Data.ArchiveSHA256 == nil {
			t.Fatal("incomplete graph", observed, supports)
		}
	})
	var saved string
	if e = db.QueryRowContext(t.Context(), "SELECT result_json FROM scan_attempts WHERE task_id=$1 AND attempt=1", rich.ID).Scan(&saved); e != nil {
		t.Fatal(e)
	}
	t.Run("invalid-optional-evidence-isolated-from-report-reader", func(t *testing.T) {
		defer db.ExecContext(t.Context(), "UPDATE scan_attempts SET result_json=$1 WHERE task_id=$2 AND attempt=1", saved, rich.ID)
		for _, key := range []string{"execution", "sourceContentHash", "sourcePointer"} {
			var v map[string]any
			json.Unmarshal([]byte(saved), &v)
			finding := v["result"].(map[string]any)["findings"].([]any)[0].(map[string]any)
			facts := finding["extensions"].(map[string]any)["axiomEvidence"].(map[string]any)
			switch key {
			case "execution":
				facts["execution"].(map[string]any)["attempt"] = 2
			case "sourceContentHash":
				facts[key] = strings.Repeat("f", 64)
			case "sourcePointer":
				facts["nodes"].([]any)[0].(map[string]any)[key] = "/raw/absent"
			}
			raw, _ := json.Marshal(v)
			if _, e = db.ExecContext(t.Context(), "UPDATE scan_attempts SET result_json=$1 WHERE task_id=$2 AND attempt=1", string(raw), rich.ID); e != nil {
				t.Fatal(e)
			}
			if code, body := getJSON(t, endpoint, token); code != 409 {
				t.Fatal(key, code, string(body))
			}
			if code, _ := getJSON(t, fmt.Sprintf("%s/api/v1/scans/%d/attempts/1/result", ts.URL, rich.ID), token); code != 200 {
				t.Fatal("optional graph broke original result")
			}
		}
		if _, e = db.ExecContext(t.Context(), "UPDATE scan_attempts SET result_json=$1 WHERE task_id=$2 AND attempt=1", saved, rich.ID); e != nil {
			t.Fatal(e)
		}
	})
	t.Run("legacy-result-is-readable-without-sdk-parsing", func(t *testing.T) {
		// Old completion envelope: no portable evidence extension, no migration or
		// rewrite of the historic source object is required by the graph reader.
		archive, _ := s.AttemptResult(t.Context(), legacy.ID, 1)
		f := archive.Findings[0]
		f.Normalized = nil
		f.Evidence = "Fixtures / Mock：历史证据文本"
		old, _ := json.Marshal(store.Completion{Findings: []store.Finding{f}})
		if _, e = db.ExecContext(t.Context(), "UPDATE scan_attempts SET result_contract_version=0,result_json=$1 WHERE task_id=$2 AND attempt=1", string(old), legacy.ID); e != nil {
			t.Fatal(e)
		}
		code, body := getJSON(t, graphURL(legacy.ID, 1, f.Fingerprint), token)
		if code != 200 || !strings.Contains(string(body), "legacy_projection") {
			t.Fatal(code, string(body))
		}
		var retained string
		db.QueryRowContext(t.Context(), "SELECT result_json FROM scan_attempts WHERE task_id=$1 AND attempt=1", legacy.ID).Scan(&retained)
		if retained != string(old) {
			t.Fatal("legacy source rewritten")
		}
	})
	t.Run("exact-attempt-finding-auth-and-read-only-permissions", func(t *testing.T) {
		for _, target := range []string{graphURL(rich.ID, 2, "fixture:evidence-chain"), graphURL(rich.ID, 1, "foreign-finding")} {
			if code, _ := getJSON(t, target, token); code != 404 {
				t.Fatal(code)
			}
		}
		if code, _ := getJSON(t, endpoint, ""); code != 401 {
			t.Fatal(code)
		}
		if code, body := doJSON(t, ts.URL+"/api/v1/users", token, map[string]any{"username": "evidence-fixture-viewer", "password": "fixture-password", "role": "viewer"}); code != 200 {
			t.Fatal(code, body)
		}
		viewer := login(t, ts.URL, "evidence-fixture-viewer", "fixture-password")
		if code, _ := getJSON(t, endpoint, viewer); code != 200 {
			t.Fatal("viewer cannot read evidence", code)
		}
		if code, _ := doJSON(t, endpoint, viewer, map[string]any{}); code != 405 {
			t.Fatal("unexpected graph write endpoint", code)
		}
	})
	if os.Getenv("AXIOM_TEST_WEB_EVIDENCE") == "1" {
		t.Run("browser-evidence-workflow", func(t *testing.T) {
			data, _ := json.Marshal(ids)
			cmd := exec.Command("node", "tests/evidence-graph-web-acceptance.mjs")
			cmd.Dir = filepath.Join(root, "code-audit-web")
			cmd.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+ts.URL, "AXIOM_TEST_EVIDENCE_IDS="+string(data))
			if sdk050 {
				cmd.Env = append(cmd.Env, "AXIOM_TEST_EVIDENCE_REPORT_DIR="+filepath.Join(root, "output/sdk050/browser-evidence-050"))
			}
			if out, e := cmd.CombinedOutput(); e != nil {
				t.Fatalf("evidence browser: %v %s", e, out)
			} else {
				t.Log(string(out))
			}
		})
		if sdk050 {
			t.Run("browser-sdk050-result-worker-and-usage", func(t *testing.T) {
				data, _ := json.Marshal(ids)
				cmd := exec.Command("node", "tests/sdk050-web-acceptance.mjs")
				cmd.Dir = filepath.Join(root, "code-audit-web")
				cmd.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+ts.URL, "AXIOM_TEST_EVIDENCE_IDS="+string(data))
				if out, err := cmd.CombinedOutput(); err != nil {
					t.Fatalf("SDK 0.5 browser: %v %s", err, out)
				} else {
					t.Log(string(out))
				}
			})
		}
	}
	for _, file := range calls {
		assertSDKCallCount(t, file, 1)
	}
}
