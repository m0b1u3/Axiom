package httpserver_test

import (
	"archive/zip"
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/sourceidentity"
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/base64"
	"encoding/hex"
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

func inputPipelineStore(t *testing.T) *store.Store {
	t.Helper()
	dsn := os.Getenv("AXIOM_TEST_DATABASE_URL")
	if dsn == "" {
		t.Skip("isolated AXIOM_TEST_DATABASE_URL is required for input pipeline acceptance")
	}
	parsed, parseErr := url.Parse(dsn)
	if parseErr != nil || !strings.HasPrefix(parsed.Path, "/axiom_test_input") || parsed.Hostname() != "127.0.0.1" {
		t.Fatal("input tests require a dedicated axiom_test database")
	}
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	s, err := store.Open(dsn)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { s.Close() })
	if _, err = db.ExecContext(t.Context(), "TRUNCATE sdk_identities,knowledge_versions,knowledge_documents,storage_cleanup_jobs,storage_settings,auth_sessions,login_limits,operation_audits,execution_settings,execution_setting_changes,execution_agents,scan_attempts,findings,scan_tasks,source_snapshots,model_configs,projects,users RESTART IDENTITY CASCADE"); err != nil {
		t.Fatal(err)
	}
	return s
}

func TestInputPipelineToSDK(t *testing.T) {
	root, err := filepath.Abs("../../..")
	if err != nil {
		t.Fatal(err)
	}
	if _, err = os.Stat(filepath.Join(root, "code-audit-scanner/dist/main.js")); err != nil {
		t.Skip("build Scanner before full input acceptance")
	}
	s := inputPipelineStore(t)
	t.Setenv("AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	scratch := t.TempDir()
	agent := filepath.Join(scratch, "agent")
	build := exec.Command("go", "build", "-o", agent, "./cmd/agent")
	build.Dir = filepath.Join(root, "code-audit-agent")
	if out, err := build.CombinedOutput(); err != nil {
		t.Fatalf("build Agent: %v %s", err, out)
	}
	secret := "fixture-input-secret"
	agentToken := "fixture-agent-token"
	_, err = s.EnsureAdministrator(t.Context(), "input-fixture-admin", auth.HashPassword(secret, "fixture-password"))
	if err != nil {
		t.Fatal(err)
	}
	srv := &httpserver.Server{Store: s, Secret: secret, AdminUsername: "input-fixture-admin", AgentToken: agentToken, SnapshotDir: filepath.Join(scratch, "uploads"), QueueLimits: store.ClaimLimits{Global: 4, PerModel: 4, PerProject: 4, Lease: time.Minute, OutputRoot: filepath.Join(scratch, "output"), MaxRecoveries: 2}}
	api := srv.Routes()
	dist := filepath.Join(root, "code-audit-web/dist")
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if strings.HasPrefix(r.URL.Path, "/api/") {
			api.ServeHTTP(w, r)
			return
		}
		if _, err := os.Stat(filepath.Join(dist, filepath.Clean(r.URL.Path))); err == nil && r.URL.Path != "/" {
			http.FileServer(http.Dir(dist)).ServeHTTP(w, r)
			return
		}
		http.ServeFile(w, r, filepath.Join(dist, "index.html"))
	}))
	defer ts.Close()
	token := login(t, ts.URL, "input-fixture-admin", "fixture-password")
	fixtureKey := "fixture-input-key-not-a-real-credential"
	encrypted, err := cryptokey.Encrypt(fixtureKey)
	if err != nil {
		t.Fatal(err)
	}
	source := filepath.Join(scratch, "source")
	if err = os.MkdirAll(filepath.Join(source, "src"), 0o700); err != nil {
		t.Fatal(err)
	}
	if err = os.MkdirAll(filepath.Join(source, "源码"), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"src/a,b.ts", "源码/测试.ts"} {
		if err = os.WriteFile(filepath.Join(source, name), []byte("// Synthetic input-contract fixture\n"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	hash, count, size, err := sourceidentity.TreeHash(t.Context(), source)
	if err != nil {
		t.Fatal(err)
	}
	snap, err := s.InsertSnapshot(t.Context(), store.SnapshotRow{OriginalFilename: "fixture-input.zip", ArchiveSha256: fmt.Sprintf("%x", sha256.Sum256([]byte(scratch))), ContentHash: hash, SnapshotPath: source, FileCount: count, TotalSize: size})
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name, provider, mode string
		budget               *float64
		workers              *int
		mutate               bool
	}{
		{"native-frozen-model", "openai", "standard", nil, nil, true},
		{"native-model-budget", "openai", "standard", ptrFloat(4.25), nil, false},
		{"deep-unset-budget-workers", "openai", "deep", nil, nil, false},
		{"deep-explicit-workers", "openai", "deep", ptrFloat(8.5), ptrInt(3), false},
		{"compatible-frozen-connection", "custom", "standard", nil, nil, true},
		{"compatible-model-budget", "custom", "standard", ptrFloat(2.75), nil, false},
	} {
		t.Run(test.name, func(t *testing.T) {
			model := store.ModelConfigRow{Pricing: inputFixturePrice(), Name: "Fixture " + test.name, Provider: test.provider, Model: "fixture-selected-" + test.name, ApiKeyEncrypted: encrypted, WireAPI: "responses"}
			if test.provider == "custom" {
				model.BaseURL = "https://fixture.invalid/v1"
				model.WireAPI = "responses"
			}
			cfg, err := s.CreateModelConfig(t.Context(), model)
			if err != nil {
				t.Fatal(err)
			}
			body := map[string]any{"sourceSnapshotId": snap.ID, "modelConfigId": cfg.ID, "scanMode": test.mode, "timeoutMinutes": 47, "maxCostUsd": test.budget, "includePaths": []string{"src/a,b.ts", "源码/测试.ts"}, "scanFocus": []string{"authz"}}
			if test.mode == "standard" {
				body["validationMode"] = "exploitability"
				body["businessContext"] = "普通用户可注册\n第二行业务背景"
			}
			if test.workers != nil {
				body["workers"] = *test.workers
			}
			code, response := doJSON(t, ts.URL+"/api/v1/scans", token, body)
			if code != 200 {
				t.Fatalf("Web API create: %d %s", code, response)
			}
			var created struct {
				Data store.Task `json:"data"`
			}
			if err = json.Unmarshal([]byte(response), &created); err != nil {
				t.Fatal(err)
			}
			if test.mutate {
				changed := model
				changed.Model = "fixture-edited-after-queue"
				changed.BaseURL = "https://different-fixture.invalid/v1"
				changed.WireAPI = "responses"
				changed.ApiKeyEncrypted, _ = cryptokey.Encrypt("fixture-different-destination-key")
				if err = s.UpdateModelConfig(t.Context(), cfg.ID, changed); err != nil {
					t.Fatal(err)
				}
			}
			capture := filepath.Join(scratch, test.name+"-sdk.json")
			runInputAgent(t, agent, root, ts.URL, agentToken, capture)
			input, err := s.Get(t.Context(), created.Data.ID)
			if err != nil {
				t.Fatal(err)
			}
			if input.Status != "completed" {
				t.Fatalf("pipeline failed before SDK completion: %+v", input)
			}
			assertSDKInput(t, capture, input, model, fixtureKey, test.budget, test.workers, test.mode)
			raw, err := os.ReadFile(filepath.Join(input.OutputPath, "runtime/job.json"))
			if err != nil {
				t.Fatal(err)
			}
			job, err := contracts.DecodeScannerJobV3(raw, &contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(input.ID)), Attempt: int64(input.Attempt)})
			if err != nil {
				t.Fatal(err)
			}
			if strings.Contains(string(raw), fixtureKey) || strings.Contains(string(raw), "credential\"") {
				t.Fatal("secret leaked into durable job")
			}
			if job.Policy.MaxCostUSD.Present != (test.budget != nil) || job.Policy.Workers.Present != (test.workers != nil) {
				t.Fatal("optional input presence changed")
			}
			info, _ := os.Stat(filepath.Join(input.OutputPath, "runtime/job.json"))
			if info.Mode().Perm() != 0o600 {
				t.Fatal("job file is not private")
			}
		})
	}

	t.Run("advanced-input-knowledge", func(t *testing.T) {
		runKnowledgeInputAcceptance(t, s, agent, root, ts.URL, token, agentToken, scratch, snap.ID, encrypted)
	})
	runLegacyInputAcceptance(t, s, agent, root, ts.URL, agentToken, scratch, snap.ID, source, encrypted, fixtureKey)
	resultIDs := map[string]int64{}
	for _, scenario := range []string{"rich", "empty", "partial-empty", "failed", "canceled", "late-exit", "wrong-task", "wrong-attempt", "tampered", "no-commit", "bad-json", "oversized", "observations", "progress-io"} {
		t.Run("result-"+scenario, func(t *testing.T) {
			t.Setenv("AXIOM_RESULT_TEST_SCENARIO", scenario)
			cfg, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Pricing: inputFixturePrice(), Name: "Fixture result " + scenario, Provider: "openai", Model: "fixture-result-configured-model", ApiKeyEncrypted: encrypted})
			if e != nil {
				t.Fatal(e)
			}
			request := map[string]any{"sourceSnapshotId": snap.ID, "modelConfigId": cfg.ID}
			if scenario == "observations" {
				request["maxCostUsd"] = 1.25
			}
			status, body := doJSON(t, ts.URL+"/api/v1/scans", token, request)
			if status != 200 {
				t.Fatal(status, body)
			}
			var created struct {
				Data store.Task `json:"data"`
			}
			if e = json.Unmarshal([]byte(body), &created); e != nil {
				t.Fatal(e)
			}
			runInputAgent(t, agent, root, ts.URL, agentToken, filepath.Join(scratch, "result-"+scenario+".json"))
			task, e := s.Get(t.Context(), created.Data.ID)
			if e != nil {
				t.Fatal(e)
			}
			resultIDs[scenario] = task.ID
			assertResultPipeline(t, s, ts.URL, token, task, scenario)
			if scenario == "progress-io" {
				if task.Status != "completed" || task.ProgressDiagnosticJSON == "" {
					t.Fatal("progress I/O failure changed the SDK conclusion", task)
				}
			}
			if scenario == "observations" {
				assertProgressPipeline(t, s, ts.URL, token, task)
			}
		})
	}

	t.Run("sdk-operations", func(t *testing.T) {
		runSDKOperationsAcceptance(t, s, agent, root, ts.URL, token, agentToken, scratch, snap.ID, encrypted)
	})
	t.Run("sdk-native", func(t *testing.T) {
		runNativeSDKAcceptance(t, s, agent, root, ts.URL, token, agentToken, scratch, snap.ID, encrypted)
	})
	runDeliveryReliabilityAcceptance(t, s, api, agent, root, ts.URL, token, agentToken, scratch, snap.ID, encrypted)
	runProcessReliabilityAcceptance(t, s, srv, agent, root, ts.URL, token, agentToken, scratch, snap.ID, encrypted)
	if os.Getenv("AXIOM_TEST_WEB_INPUT") == "1" {
		t.Run("browser-knowledge", func(t *testing.T) {
			runKnowledgeWebAcceptance(t, s, agent, root, ts.URL, token, agentToken, scratch, snap.ID, encrypted)
		})
		t.Run("browser-progress", func(t *testing.T) {
			t.Setenv("AXIOM_RESULT_TEST_SCENARIO", "observation-cancel")
			cfg, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Pricing: inputFixturePrice(), Name: "Fixture live progress", Provider: "openai", Model: "fixture-live-progress-model", ApiKeyEncrypted: encrypted})
			if e != nil {
				t.Fatal(e)
			}
			status, body := doJSON(t, ts.URL+"/api/v1/scans", token, map[string]any{"sourceSnapshotId": snap.ID, "modelConfigId": cfg.ID, "scanMode": "deep", "maxCostUsd": 1.25})
			if status != 200 {
				t.Fatal(status, string(body))
			}
			var created struct {
				Data store.Task `json:"data"`
			}
			if e = json.Unmarshal([]byte(body), &created); e != nil {
				t.Fatal(e)
			}
			runProgressWebAcceptance(t, s, agent, root, ts.URL, token, agentToken, filepath.Join(scratch, "browser-progress-sdk.json"), created.Data.ID)
		})
		t.Run("browser-results", func(t *testing.T) {
			cmd := exec.Command("node", filepath.Join(root, "code-audit-web/tests/result-web-acceptance.mjs"))
			cmd.Dir = root
			ids, _ := json.Marshal(resultIDs)
			cmd.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+ts.URL, "AXIOM_TEST_RESULT_IDS="+string(ids))
			if out, e := cmd.Output(); e != nil {
				if failed, ok := e.(*exec.ExitError); ok {
					t.Fatalf("result browser: %v %s", e, failed.Stderr)
				}
				t.Fatalf("result browser: %v %s", e, out)
			}
		})

		t.Run("browser-create-scan", func(t *testing.T) {
			cfg, err := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Pricing: inputFixturePrice(), Name: "Fixture browser input", Provider: "openai", Model: "fixture-browser-selected-model", ApiKeyEncrypted: encrypted})
			if err != nil {
				t.Fatal(err)
			}
			if err = s.SetModelConfigEnabled(t.Context(), cfg.ID, true); err != nil {
				t.Fatal(err)
			}
			archive := filepath.Join(scratch, "fixture-browser-source.zip")
			f, err := os.Create(archive)
			if err != nil {
				t.Fatal(err)
			}
			zw := zip.NewWriter(f)
			for _, name := range []string{"src/a,b.ts", "源码/测试.ts"} {
				entry, err := zw.Create(name)
				if err != nil {
					t.Fatal(err)
				}
				_, _ = entry.Write([]byte("// Browser input fixture\n"))
			}
			if err = zw.Close(); err != nil {
				t.Fatal(err)
			}
			f.Close()
			cmd := exec.Command("node", filepath.Join(root, "code-audit-web/tests/input-web-acceptance.mjs"))
			cmd.Dir = root
			cmd.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+ts.URL, "AXIOM_TEST_WEB_ARCHIVE="+archive)
			out, err := cmd.Output()
			if err != nil {
				if failed, ok := err.(*exec.ExitError); ok {
					t.Fatalf("browser acceptance: %v %s", err, failed.Stderr)
				}
				t.Fatalf("browser acceptance: %v %s", err, out)
			}
			var result struct {
				ID int64 `json:"id"`
			}
			if err = json.Unmarshal(out, &result); err != nil {
				t.Fatalf("browser result: %v %s", err, out)
			}
			capture := filepath.Join(scratch, "browser-sdk.json")
			runInputAgent(t, agent, root, ts.URL, agentToken, capture)
			task, err := s.Get(t.Context(), result.ID)
			if err != nil || task.Status != "completed" {
				t.Fatalf("browser pipeline completion: %+v %v", task, err)
			}
			assertSDKInput(t, capture, task, *cfg, fixtureKey, ptrFloat(4.25), nil, "standard")
		})
	}
}

func ptrFloat(v float64) *float64 { return &v }
func ptrInt(v int) *int           { return &v }

func runInputAgent(t *testing.T, agent, root, url, token, capture string) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
	defer cancel()
	cmd := inputAgentCommand(ctx, agent, root, url, token, capture)
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("real Agent/Scanner fixture run: %v %s", err, out)
	}
}
func inputAgentCommand(ctx context.Context, agent, root, url, token, capture string) *exec.Cmd {
	cmd := exec.CommandContext(ctx, agent, "--once")
	cmd.Dir = root
	cmd.Env = append(os.Environ(), "AXIOM_SERVER_URL="+url, "AXIOM_AGENT_TOKEN="+token, "AXIOM_AGENT_ID=fixture-input-agent", "AXIOM_AGENT_CONCURRENCY=1", "AXIOM_SCANNER_DIR="+filepath.Join(root, "code-audit-scanner"), "AXIOM_SCAN_MOCK=0", "AXIOM_INPUT_TEST_CAPTURE="+capture,
		"NODE_OPTIONS=--import="+filepath.Join(root, "contracts/tests/input-sdk-register.mjs"))
	return cmd
}

func assertSDKInput(t *testing.T, capture string, task *store.Task, model store.ModelConfigRow, key string, budget *float64, workers *int, mode string) {
	t.Helper()
	raw, err := os.ReadFile(capture)
	if err != nil {
		t.Fatal(err)
	}
	var v struct {
		Source string `json:"source"`
		Config struct {
			Overrides map[string]any `json:"codexOverrides"`
		} `json:"config"`
		Options          map[string]any `json:"options"`
		CredentialDigest string         `json:"credentialDigest"`
		Leaked           bool           `json:"leakedServerCredentials"`
	}
	if err = json.Unmarshal(raw, &v); err != nil {
		t.Fatal(err)
	}
	if v.Config.Overrides["model"] != model.Model || v.Source != task.SourcePath || v.Options["mode"] != mode {
		t.Fatalf("selected model/source/mode did not reach SDK: %s", raw)
	}
	if target, _ := json.Marshal(v.Options["target"]); string(target) != `["src/a,b.ts","源码/测试.ts"]` {
		t.Fatalf("SDK scope was split: %s", target)
	}
	value, present := v.Options["maxCostUsd"]
	if present {
		t.Fatal("configured budget must not use the SDK price list")
	}
	if budget != nil {
		raw, e := os.ReadFile(filepath.Join(task.OutputPath, "runtime/token-budget.json"))
		if e != nil {
			t.Fatal(e)
		}
		assignment, e := contracts.DecodeTokenBudgetAssignment(raw, &contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(task.ID)), Attempt: int64(task.Attempt)})
		if e != nil || float64(assignment.LimitMicros)/1e6 != *budget || assignment.PricingSnapshot.Model != model.Model {
			t.Fatal("configured budget or frozen price missing", e, assignment)
		}
	}
	value, present = v.Options["workers"]
	if present != (workers != nil) || (workers != nil && value != float64(*workers)) {
		t.Fatal("SDK worker defaults or explicit count were changed")
	}
	if mode == "deep" && v.Options["maxTimeMinutes"] != float64(47) {
		t.Fatal("SDK discovery budget differs from configured time")
	}
	if mode == "standard" && !strings.Contains(v.Options["scanPrompt"].(string), "第二行业务背景") {
		t.Fatal("multiline business context lost before SDK")
	}
	digest := sha256.Sum256([]byte(key))
	if v.CredentialDigest != hex.EncodeToString(digest[:]) || v.Leaked {
		t.Fatal("credential binding changed or unrelated server credentials leaked")
	}
}

func inputFixturePrice() *contracts.ModelPricing {
	rate := contracts.TokenUnitPrice("1")
	output := contracts.TokenUnitPrice("66176.470588") // 17 fixture output tokens round once to the historical $1.125 receipt.
	return &contracts.ModelPricing{SchemaVersion: 1, Currency: "USD", Unit: "per_million_tokens", CacheMode: "input_only", InputPerMillion: &rate, OutputPerMillion: &output, Note: "Fixtures / Mock only; synthetic prices, no model calls"}
}
