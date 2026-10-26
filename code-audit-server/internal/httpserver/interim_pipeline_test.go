package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/sourceidentity"
	"context"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// Real Server, Agent and Scanner. Only the model SDK runtime is replaced.
func TestStandardInterimPipeline(t *testing.T) {
	root, e := filepath.Abs("../../..")
	if e != nil {
		t.Fatal(e)
	}
	if _, e = os.Stat(filepath.Join(root, "code-audit-scanner/dist/main.js")); e != nil {
		t.Skip("build Scanner first")
	}
	s := inputPipelineStore(t)
	scratch := t.TempDir()
	t.Setenv("AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	secret, agentToken := "fixture-interim-session", "fixture-interim-agent"
	_, e = s.EnsureAdministrator(t.Context(), "input-fixture-admin", auth.HashPassword(secret, "fixture-password"))
	if e != nil {
		t.Fatal(e)
	}
	agent := filepath.Join(scratch, "agent")
	build := exec.Command("go", "build", "-o", agent, "./cmd/agent")
	build.Dir = filepath.Join(root, "code-audit-agent")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	source := filepath.Join(scratch, "source")
	os.MkdirAll(filepath.Join(source, "src"), 0700)
	os.WriteFile(filepath.Join(source, "src/query.ts"), []byte("// Fixtures / Mock only\nconst q = input;\ndatabase.execute(q);\n"), 0600)
	hash, count, size, e := sourceidentity.TreeHash(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	snap, e := s.InsertSnapshot(t.Context(), store.SnapshotRow{OriginalFilename: "Fixtures-Mock-interim.zip", ArchiveSha256: strings.Repeat("b", 64), ContentHash: hash, SnapshotPath: source, FileCount: count, TotalSize: size})
	if e != nil {
		t.Fatal(e)
	}
	key, e := cryptokey.Encrypt("fixture-interim-only-key")
	if e != nil {
		t.Fatal(e)
	}
	model, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixtures / Mock intermediate", Provider: "openai", Model: "gpt-6-sol", ApiKeyEncrypted: key})
	if e != nil {
		t.Fatal(e)
	}
	srv := &httpserver.Server{Store: s, Secret: secret, AgentToken: agentToken, SnapshotDir: filepath.Join(scratch, "uploads"), QueueLimits: store.ClaimLimits{Lease: time.Minute, OutputRoot: filepath.Join(scratch, "scans"), MaxRecoveries: 0, SDKConcurrencyCapable: true}}
	api := srv.Routes()
	dist := filepath.Join(root, "code-audit-web/dist")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
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
	task := knowledgeTask(t, ts.URL, token, map[string]any{"sourceSnapshotId": snap.ID, "modelConfigId": model.ID, "scanMode": "standard"})
	if code, body := doJSON(t, ts.URL+"/api/v1/users", token, map[string]any{"username": "interim-fixture-viewer", "password": "fixture-password", "role": "viewer"}); code != 200 {
		t.Fatal(code, body)
	}
	if os.Getenv("AXIOM_TEST_WEB_INTERIM") == "1" {
		browser := exec.Command("node", "tests/interim-web-acceptance.mjs")
		browser.Dir = filepath.Join(root, "code-audit-web")
		browser.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+ts.URL, "AXIOM_TEST_INTERIM_TASK="+fmt.Sprint(task.ID), "AXIOM_TEST_AGENT_BIN="+agent, "AXIOM_TEST_AGENT_TOKEN="+agentToken)
		if out, e := browser.CombinedOutput(); e != nil {
			t.Fatal("interim browser", e, string(out))
		} else {
			t.Log(string(out))
		}
	} else {
		for _, scenario := range []string{"interim-failure", "interim-timeout"} {
			capture := filepath.Join(scratch, scenario+".json")
			os.WriteFile(capture+".release", []byte("Fixtures / Mock release"), 0600)
			ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
			command := inputAgentCommand(ctx, agent, root, ts.URL, agentToken, capture)
			command.Env = append(command.Env, "AXIOM_RESULT_TEST_SCENARIO="+scenario)
			out, err := command.CombinedOutput()
			cancel()
			if err != nil {
				t.Fatal(err, string(out))
			}
			current, e := s.Get(t.Context(), task.ID)
			if e != nil {
				t.Fatal(e)
			}
			if current.Status != "failed" && current.Status != "timeout" {
				t.Fatal(current.Status)
			}
			if scenario == "interim-failure" {
				if ok, e := s.Retry(t.Context(), task.ID); e != nil || !ok {
					t.Fatal(ok, e)
				}
			}
		}
	}

	catalog := func(query string) (int, map[string]any) {
		req, _ := http.NewRequest("GET", ts.URL+"/api/v1/findings/interim"+query, nil)
		req.Header.Set("Authorization", "Bearer "+token)
		response, err := http.DefaultClient.Do(req)
		if err != nil {
			t.Fatal(err)
		}
		defer response.Body.Close()
		var body struct {
			Data map[string]any `json:"data"`
		}
		json.NewDecoder(response.Body).Decode(&body)
		return response.StatusCode, body.Data
	}
	code, catalogPage := catalog("?page=999&pageSize=1")
	if code != 200 || catalogPage["total"] != float64(2) || catalogPage["page"] != float64(2) {
		t.Fatal("interim catalog pagination", code, catalogPage)
	}
	entry := catalogPage["items"].([]any)[0].(map[string]any)
	if entry["attempt"] != float64(1) || entry["state"] != "observed" || entry["observation"].(map[string]any)["candidateRecords"] != float64(25) {
		t.Fatal("original execution binding", entry)
	}
	if _, exposed := entry["outputPath"]; exposed {
		t.Fatal("private output path exposed")
	}
	observation, _ := json.Marshal(entry["observation"])
	expected := contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(task.ID)), Attempt: 1}
	if _, err := contracts.DecodeInterimObservation(observation, &expected); err != nil {
		t.Fatal("generated portable observation rejected catalog", err)
	}
	for query, want := range map[string]float64{"?status=failed": 1, "?status=timeout": 1, "?projectId=0": 2, "?projectId=9999": 0, "?q=Fixtures-Mock-interim": 2, "?q=100%25_!": 0} {
		if c, v := catalog(query); c != 200 || v["total"] != want {
			t.Fatal("interim catalog filter", query, c, v)
		}
	}
	for _, query := range []string{"?page=-1", "?pageSize=101", "?page=1&page=2", "?status=verified", "?projectId=-1", "?sort=title", "?order=desc"} {
		if c, _ := catalog(query); c != 400 {
			t.Fatal("invalid interim catalog query", query, c)
		}
	}
	if list, err := s.ListFindings(t.Context(), task.ID); err != nil || len(list) != 0 {
		t.Fatal("candidates promoted to findings", err)
	}
	get := func(suffix string) (int, []byte) {
		request, _ := http.NewRequest("GET", ts.URL+fmt.Sprintf("/api/v1/scans/%d/attempts/1/interim", task.ID)+suffix, nil)
		request.Header.Set("Authorization", "Bearer "+token)
		response, e := http.DefaultClient.Do(request)
		if e != nil {
			t.Fatal(e)
		}
		defer response.Body.Close()
		var body json.RawMessage
		json.NewDecoder(response.Body).Decode(&body)
		return response.StatusCode, body
	}
	code, raw := get("?page=2&pageSize=20")
	if code != 200 {
		t.Fatal(code, string(raw))
	}
	var page struct {
		Data contracts.InterimPage `json:"data"`
	}
	if e = json.Unmarshal(raw, &page); e != nil || page.Data.Total != 25 || len(page.Data.Items) != 5 {
		t.Fatal(e, string(raw))
	}
	if code, _ = get("?page=-1"); code != 400 {
		t.Fatal("invalid pagination", code)
	}
	a, e := s.GetAttempt(t.Context(), task.ID, 1)
	if e != nil {
		t.Fatal(e)
	}
	ref := page.Data.Documents[0].Artifact
	original, e := os.ReadFile(filepath.Join(a.OutputPath, string(ref.RelativePath)))
	if e != nil {
		t.Fatal(e)
	}
	os.WriteFile(filepath.Join(a.OutputPath, string(ref.RelativePath)), []byte("tampered"), 0600)
	if code, _ = get("/" + string(ref.SHA256)); code != 409 {
		t.Fatal("tamper accepted", code)
	}
	os.WriteFile(filepath.Join(a.OutputPath, string(ref.RelativePath)), original, 0600)
	if code, _ = get("/" + strings.Repeat("f", 64)); code != 404 {
		t.Fatal("foreign artifact", code)
	}
	index := filepath.Join(a.OutputPath, "checkpoints/interim/index.json")
	old, e := os.ReadFile(index)
	if e != nil {
		t.Fatal(e)
	}
	var object map[string]any
	json.Unmarshal(old, &object)
	object["sourceContentHash"] = strings.Repeat("f", 64)
	bad, _ := json.Marshal(object)
	os.WriteFile(index, bad, 0600)
	if code, _ = get(""); code != 409 {
		t.Fatal("foreign source accepted", code)
	}

	if c, v := catalog("?page=2&pageSize=1"); c != 200 {
		t.Fatal(c, v)
	} else {
		row := v["items"].([]any)[0].(map[string]any)
		if row["state"] != "rejected" || row["observation"] != nil {
			t.Fatal("corrupt evidence presented as empty", row)
		}
	}
	os.WriteFile(index, old, 0600)
	if err := os.Rename(index, index+".offline"); err != nil {
		t.Fatal(err)
	}
	if c, v := catalog("?page=2&pageSize=1"); c != 200 {
		t.Fatal(c, v)
	} else {
		row := v["items"].([]any)[0].(map[string]any)
		if row["state"] != "unavailable" || row["observation"] != nil {
			t.Fatal("missing evidence presented as zero", row)
		}
	}
	if err := os.Rename(index+".offline", index); err != nil {
		t.Fatal(err)
	}
	attempts, e := s.ListAttempts(t.Context(), task.ID)
	if e != nil || len(attempts) != 2 {
		t.Fatal("history changed", e, attempts)
	}
}
