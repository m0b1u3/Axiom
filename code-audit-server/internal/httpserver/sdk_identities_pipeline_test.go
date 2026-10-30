package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/sourceidentity"
	"context"
	"database/sql"
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

func TestSDKIdentitiesPipeline(t *testing.T) {
	s := inputPipelineStore(t)
	root, _ := filepath.Abs("../../..")
	scratch := t.TempDir()
	data := filepath.Join(scratch, "agent-private")
	agent := filepath.Join(scratch, "agent")
	build := exec.Command("go", "build", "-o", agent, "./cmd/agent")
	build.Dir = filepath.Join(root, "code-audit-agent")
	if out, e := build.CombinedOutput(); e != nil {
		t.Fatal(e, string(out))
	}
	t.Setenv("AXIOM_MASTER_KEY", base64.RawURLEncoding.EncodeToString(make([]byte, 32)))
	t.Setenv("AXIOM_IDENTITY_TEST_FIXTURE", "1")
	t.Setenv("AXIOM_DATA_DIR", data)
	t.Setenv("AXIOM_LEASE", "15")
	t.Setenv("OPENAI_API_KEY", "fixture-ambient-key-must-not-leak")
	secret, agentToken := "fixture-identity-secret", "fixture-identity-agent"
	if _, e := s.EnsureAdministrator(t.Context(), "input-fixture-admin", auth.HashPassword(secret, "fixture-password")); e != nil {
		t.Fatal(e)
	}
	limits := store.ClaimLimits{Global: 3, PerModel: 3, PerProject: 3, Lease: 15 * time.Second, OutputRoot: filepath.Join(scratch, "output"), MaxRecoveries: 0}
	server := &httpserver.Server{Store: s, Secret: secret, AgentToken: agentToken, SnapshotDir: filepath.Join(scratch, "uploads"), QueueLimits: limits}
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
	db, e := sql.Open("pgx", os.Getenv("AXIOM_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	create := func(name, kind string) contracts.SDKIdentitySnapshot {
		t.Helper()
		code, body := doJSON(t, ts.URL+"/api/v1/sdk-identities", token, map[string]any{"name": name, "kind": kind, "agentId": "fixture-input-agent"})
		if code != 200 {
			t.Fatal(code, body)
		}
		var v struct{ Data contracts.SDKIdentitySnapshot }
		json.Unmarshal([]byte(body), &v)
		return v.Data
	}
	start := func(v contracts.SDKIdentitySnapshot, action string) contracts.SDKIdentityOperation {
		t.Helper()
		body := map[string]any{"generation": v.Identity.Generation, "action": action}
		if action == "login_api_key" {
			body["apiKey"] = "fixture-selected-identity-key"
		}
		code, raw := doJSON(t, ts.URL+"/api/v1/sdk-identities/"+v.Identity.ID+"/operations", token, body)
		if code != 200 {
			t.Fatal(code, raw)
		}
		var out struct {
			Data contracts.SDKIdentityOperation
		}
		json.Unmarshal([]byte(raw), &out)
		return out.Data
	}
	fresh := func(id string) contracts.SDKIdentitySnapshot {
		t.Helper()
		v, e := s.SDKIdentity(t.Context(), number(id))
		if e != nil {
			t.Fatal(e)
		}
		return v
	}
	run := func(scenario string) string {
		t.Helper()
		t.Setenv("AXIOM_IDENTITY_TEST_SCENARIO", scenario)
		capture := filepath.Join(scratch, fmt.Sprintf("capture-%d.json", time.Now().UnixNano()))
		cmd := inputAgentCommand(t.Context(), agent, root, ts.URL, agentToken, capture)
		out, e := cmd.CombinedOutput()
		t.Log(string(out))
		if e != nil {
			t.Fatal(e)
		}
		return capture
	}
	opState := func(v contracts.SDKIdentitySnapshot) contracts.SDKIdentityOperation {
		t.Helper()
		list, e := s.SDKIdentityOperations(t.Context(), number(v.Identity.ID))
		if e != nil || len(list) == 0 {
			t.Fatal(e)
		}
		return list[0]
	}
	key := create("Fixtures / Mock API identity", "api_key")
	start(key, "login_api_key")
	run("normal")
	key = fresh(key.Identity.ID)
	if key.State != "authenticated" || key.Identity.Generation != 2 {
		t.Fatal(key)
	}
	var pendingSecrets int
	db.QueryRow(`SELECT count(*) FROM sdk_identity_operations WHERE secret_encrypted<>''`).Scan(&pendingSecrets)
	if pendingSecrets != 0 {
		t.Fatal("pending credential retained after login")
	}
	modelCode, modelBody := doJSON(t, ts.URL+"/api/v1/model-configs", token, map[string]any{"name": "Identity execution fixture", "provider": "openai", "model": "fixture-identity-model", "sdkIdentityOnly": true, "wireApi": "responses"})
	if modelCode != 200 {
		t.Fatal(modelCode, modelBody)
	}
	var model struct{ Data store.ModelConfigRow }
	json.Unmarshal([]byte(modelBody), &model)
	source := filepath.Join(scratch, "source")
	os.MkdirAll(filepath.Join(source, "src"), 0700)
	os.WriteFile(filepath.Join(source, "src/a,b.ts"), []byte("// Fixtures / Mock only\n"), 0600)
	hash, count, size, e := sourceidentity.TreeHash(t.Context(), source)
	if e != nil {
		t.Fatal(e)
	}
	snap, e := s.InsertSnapshot(t.Context(), store.SnapshotRow{OriginalFilename: "Fixtures-Mock-identities.zip", ArchiveSha256: strings.Repeat("a", 64), ContentHash: hash, SnapshotPath: source, FileCount: count, TotalSize: size})
	if e != nil {
		t.Fatal(e)
	}
	launch := func(identity contracts.SDKIdentitySnapshot) *store.Task {
		t.Helper()
		// Identity authentication is independent of configured-price budgeting.
		code, body := doJSON(t, ts.URL+"/api/v1/scans", token, map[string]any{"sourceSnapshotId": snap.ID, "modelConfigId": model.Data.ID, "sdkIdentityId": identity.Identity.ID})
		if code != 200 {
			t.Fatal(code, body)
		}
		var v struct{ Data store.Task }
		json.Unmarshal([]byte(body), &v)
		return &v.Data
	}
	t.Run("account-only-model-requires-valid-identity-before-queueing", func(t *testing.T) {
		code, raw := getJSON(t, ts.URL+"/api/v1/model-configs", token)
		if code != 200 || !strings.Contains(string(raw), `"supportsSdkIdentity":true`) || !strings.Contains(string(raw), `"sdkIdentityOnly":true`) {
			t.Fatal("account-only model capability missing", code)
		}
		unconfigured := create("Fixtures / Mock not logged in", "chatgpt")
		for _, id := range []string{"", "missing", unconfigured.Identity.ID} {
			code, _ := doJSON(t, ts.URL+"/api/v1/scans", token, map[string]any{"sourceSnapshotId": snap.ID, "modelConfigId": model.Data.ID, "sdkIdentityId": id})
			if code != 400 {
				t.Fatalf("invalid identity %q accepted: %d", id, code)
			}
		}
	})
	var historical int64
	t.Run("bound-node-capability-result-and-secret-isolation", func(t *testing.T) {
		task := launch(key)
		historical = task.ID
		caps := contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{1}, JobVersions: []contracts.SchemaVersion{1}, MaxCostUSD: true}
		result := contracts.ResultCapabilities{JobVersions: []int64{2}, ResultVersions: []int64{1}, CompletionVersions: []int64{1}, CommitVersions: []int64{1}}
		obs := contracts.ObservationCapabilities{JobVersions: []int64{3, 4, 5, 6, 7, 8}, ProgressVersions: []int64{2}, EventVersions: []int64{2}, HeartbeatVersions: []int64{1}}
		for _, test := range []struct {
			agent   string
			capable bool
		}{{"fixture-input-agent", false}, {"other-agent", true}} {
			l := limits
			l.SDKIdentityCapable = test.capable
			claimed, e := s.ClaimExecutionWithObservations(t.Context(), test.agent, l, caps, result, obs)
			if e != nil || claimed != nil {
				t.Fatal("foreign or old node claimed identity", e)
			}
		}
		capture := run("normal")
		current, _ := s.Get(t.Context(), task.ID)
		if current.Status != "completed" {
			t.Fatal(current.Status, current.ErrorMessage)
		}
		archive, e := s.AttemptResult(t.Context(), task.ID, 1)
		if e != nil || archive.Result == nil {
			t.Fatal(e)
		}
		binding, e := contracts.DecodeSDKTaskIdentity(archive.Result.Extensions.Value["sdkIdentity"], &archive.Result.Execution)
		if e != nil || binding.Identity != key.Identity {
			t.Fatal(e, binding)
		}
		calls, _ := os.ReadFile(capture + ".auth")
		if !strings.Contains(string(calls), `"auth":"api-key"`) || strings.Contains(string(calls), `"leakedServerCredentials":true`) {
			t.Fatal("identity auth mapping failed")
		}
		if _, e := os.Stat(filepath.Join(task.OutputPath, "runtime/sdk/codex-home")); !os.IsNotExist(e) {
			t.Fatal("credential home entered public execution artifacts")
		}
	})
	t.Run("revocation-blocks-queued-work-and-cleans-private-credentials", func(t *testing.T) {
		pending := launch(key)
		start(key, "logout")
		revoked := fresh(key.Identity.ID)
		if revoked.State != "revoked" || !revoked.CleanupPending || revoked.Identity.Generation != 3 {
			t.Fatal(revoked)
		}
		run("normal")
		revoked = fresh(key.Identity.ID)
		if revoked.CleanupPending {
			t.Fatal("logout cleanup not confirmed")
		}
		old, _ := s.Get(t.Context(), historical)
		if old.SDKIdentity == nil || old.SDKIdentity.Generation != 2 {
			t.Fatal("historical identity mutated")
		}
		queued, _ := s.Get(t.Context(), pending.ID)
		if queued.Attempt != 0 || queued.Status != "pending" {
			t.Fatal("revoked identity was executed")
		}
		s.RequestCancel(t.Context(), pending.ID)
	})
	chat := create("Fixtures / Mock ChatGPT identity", "chatgpt")
	t.Run("device-code-wait-cancel-and-no-automatic-replay", func(t *testing.T) {
		op := start(chat, "login_device_code")
		t.Setenv("AXIOM_IDENTITY_TEST_SCENARIO", "hold")
		capture := filepath.Join(scratch, "device-cancel.json")
		ctx, cancel := context.WithTimeout(t.Context(), 30*time.Second)
		defer cancel()
		cmd := inputAgentCommand(ctx, agent, root, ts.URL, agentToken, capture)
		if e := cmd.Start(); e != nil {
			t.Fatal(e)
		}
		waitIdentity(t, func() bool { return opState(chat).Status == "waiting" })
		overview, e := s.QueueOverview(t.Context(), limits, 0)
		if e != nil || overview.Active != 1 {
			t.Fatal("device login did not occupy a visible worker slot", e, overview.Active)
		}
		state := opState(chat)
		if state.Instructions == nil || state.Instructions.UserCode != "MOCK-1234" {
			t.Fatal(state)
		}
		code, body := doJSON(t, ts.URL+"/api/v1/sdk-identity-operations/"+op.ID+"/cancel", token, map[string]any{})
		if code != 200 {
			t.Fatal(code, body)
		}
		if e := cmd.Wait(); e != nil {
			t.Fatal(e)
		}
		if opState(chat).Status != "canceled" || opState(chat).Instructions != nil {
			t.Fatal("cancel retained instructions")
		}
		run("normal")
		if opState(chat).Status != "canceled" {
			t.Fatal("login automatically replayed")
		}
	})
	t.Run("restart-account-chatgpt-auth-and-no-model-key-fallback", func(t *testing.T) {
		chat = fresh(chat.Identity.ID)
		start(chat, "login_device_code")
		run("normal")
		chat = fresh(chat.Identity.ID)
		if chat.State != "authenticated" {
			t.Fatal(chat)
		}
		start(chat, "account")
		run("normal")
		chat = fresh(chat.Identity.ID)
		if chat.Authenticated == nil || !*chat.Authenticated {
			t.Fatal(chat)
		}
		task := launch(chat)
		capture := run("normal")
		current, _ := s.Get(t.Context(), task.ID)
		if current.Status != "completed" {
			t.Fatal(current.Status, current.ErrorMessage)
		}
		calls, _ := os.ReadFile(capture + ".auth")
		if !strings.Contains(string(calls), `"auth":"chatgpt"`) || strings.Contains(string(calls), `"credentialPresent":true`) {
			t.Fatal("ChatGPT fell back to API key")
		}
		var persistedSession bool
		filepath.WalkDir(filepath.Join(data, ".sdk-identities"), func(p string, d os.DirEntry, e error) error {
			if e == nil && filepath.Base(p) == "auth.json" && filepath.Base(filepath.Dir(filepath.Dir(filepath.Dir(filepath.Dir(p))))) == chat.Identity.ID {
				b, _ := os.ReadFile(p)
				persistedSession = strings.Contains(string(b), `"refreshedByFixture":true`)
			}
			return nil
		})
		if !persistedSession {
			t.Fatal("refreshed ChatGPT session was discarded with execution state")
		}
		start(chat, "account")
		run("normal")
		if fresh(chat.Identity.ID).State != "authenticated" {
			t.Fatal("refreshed session did not survive a new process")
		}
		start(chat, "account")
		run("account-failure")
		if fresh(chat.Identity.ID).State != "unknown" {
			t.Fatal("failed account check became authenticated")
		}
		chat = fresh(chat.Identity.ID)
	})
	t.Run("expired-lease-unknown-cost-free-operation-and-late-report-rejection", func(t *testing.T) {
		start(chat, "account")
		claim, _, e := s.ClaimSDKIdentity(t.Context(), "fixture-input-agent", limits)
		if e != nil || claim == nil {
			t.Fatal(e)
		}
		db.Exec(`UPDATE sdk_identity_operations SET lease_until='2000-01-01T00:00:00Z' WHERE id=$1`, claim.Operation.ID)
		s.SDKIdentities(t.Context())
		if opState(chat).Status != "expired" || fresh(chat.Identity.ID).State != "unknown" {
			t.Fatal("lost lease not expired")
		}
		v := contracts.SDKIdentityReport{SchemaVersion: 1, OperationID: claim.Operation.ID, IdentityID: chat.Identity.ID, AgentID: "fixture-input-agent", Generation: claim.Operation.Identity.Generation, LeaseToken: claim.LeaseToken, Sequence: 1, Status: "completed", Authenticated: boolRef(true)}
		status, e := s.ReportSDKIdentity(t.Context(), v, 15*time.Second)
		if e != nil || status != "expired" {
			t.Fatal("late report changed expiry", e, status)
		}
		if fresh(chat.Identity.ID).State != "unknown" {
			t.Fatal("late report authenticated")
		}
	})
	t.Run("identity-report-binding-ordering-and-idempotency", func(t *testing.T) {
		v := create("Fixtures / Mock report protocol", "chatgpt")
		start(v, "account")
		claim, _, e := s.ClaimSDKIdentity(t.Context(), "fixture-input-agent", limits)
		if e != nil || claim == nil || claim.LeaseSeconds != 15 {
			t.Fatal("claim lease", e)
		}
		report := contracts.SDKIdentityReport{SchemaVersion: 1, OperationID: claim.Operation.ID, IdentityID: v.Identity.ID, AgentID: "fixture-input-agent", Generation: claim.Operation.Identity.Generation, LeaseToken: claim.LeaseToken, Sequence: 1, Status: "running"}
		for _, change := range []func(*contracts.SDKIdentityReport){func(r *contracts.SDKIdentityReport) { r.AgentID = "foreign-agent" }, func(r *contracts.SDKIdentityReport) { r.Generation++ }, func(r *contracts.SDKIdentityReport) { r.LeaseToken = strings.Repeat("b", 64) }, func(r *contracts.SDKIdentityReport) { r.Sequence = 3 }} {
			bad := report
			change(&bad)
			if _, e := s.ReportSDKIdentity(t.Context(), bad, limits.Lease); e == nil {
				t.Fatal("foreign or skipped report accepted")
			}
		}
		if _, e = s.ReportSDKIdentity(t.Context(), report, limits.Lease); e != nil {
			t.Fatal(e)
		}
		report.Sequence = 2
		report.Status = "completed"
		report.Authenticated = boolRef(false)
		for i := 0; i < 2; i++ {
			if status, e := s.ReportSDKIdentity(t.Context(), report, limits.Lease); e != nil || status != "completed" {
				t.Fatal("completion replay", e, status)
			}
		}
		report.Authenticated = boolRef(true)
		if _, e = s.ReportSDKIdentity(t.Context(), report, limits.Lease); e == nil {
			t.Fatal("conflicting completion accepted")
		}
		if fresh(v.Identity.ID).State != "signed_out" {
			t.Fatal("account false became authenticated")
		}
	})
	t.Run("runtime-and-storage-admission-before-secret-delivery", func(t *testing.T) {
		v := create("Fixtures / Mock guarded claim", "api_key")
		op := start(v, "login_api_key")
		for _, guard := range []string{"stale-runtime", "blocked-runtime", "old-scanner", "storage"} {
			var runtimeJSON string
			var seen time.Time
			if e := db.QueryRow(`SELECT runtime_json,runtime_seen_at FROM execution_agents WHERE agent_id='fixture-input-agent'`).Scan(&runtimeJSON, &seen); e != nil {
				t.Fatal(e)
			}
			l := limits
			switch guard {
			case "stale-runtime":
				db.Exec(`UPDATE execution_agents SET runtime_seen_at='2000-01-01' WHERE agent_id='fixture-input-agent'`)
			case "blocked-runtime":
				db.Exec(`UPDATE execution_agents SET runtime_json=jsonb_set(runtime_json::jsonb,'{state}','"blocked"')::text WHERE agent_id='fixture-input-agent'`)
			case "old-scanner":
				db.Exec(`UPDATE execution_agents SET runtime_json=jsonb_set(runtime_json::jsonb,'{scannerDescriptionVersion}','9')::text WHERE agent_id='fixture-input-agent'`)
			case "storage":
				l.StorageCheck = func(context.Context) string { return "fixture-capacity-blocked" }

			}
			claim, secret, e := s.ClaimSDKIdentity(t.Context(), "fixture-input-agent", l)
			db.Exec(`UPDATE execution_agents SET runtime_json=$1,runtime_seen_at=$2 WHERE agent_id='fixture-input-agent'`, runtimeJSON, seen)

			if e != nil || claim != nil || secret != "" {
				t.Fatalf("%s delivered credential: %v", guard, e)
			}
		}
		s.CancelSDKIdentityOperation(t.Context(), number(op.ID))
	})
	t.Run("running-task-revocation-waits-before-cleaning-all-generations", func(t *testing.T) {
		v := create("Fixtures / Mock active logout", "api_key")
		start(v, "login_api_key")
		run("normal")
		v = fresh(v.Identity.ID)
		// Keep a previous generation to prove logout clears the entire identity.
		homeRoot := filepath.Join(data, ".sdk-identities")
		var identityRoot string
		filepath.WalkDir(homeRoot, func(p string, d os.DirEntry, e error) error {
			if e == nil && d.IsDir() && filepath.Base(p) == v.Identity.ID {
				identityRoot = p
			}
			return nil
		})
		if identityRoot == "" {
			t.Fatal("identity private directory missing")
		}
		os.MkdirAll(filepath.Join(identityRoot, "generation-1"), 0700)
		os.WriteFile(filepath.Join(identityRoot, "generation-1", "old-secret"), []byte("Fixtures / Mock"), 0600)
		task := launch(v)
		t.Setenv("AXIOM_RESULT_TEST_SCENARIO", "reliability-hold")
		capture := filepath.Join(scratch, "active-revoke.json")
		ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
		defer cancel()
		cmd := inputAgentCommand(ctx, agent, root, ts.URL, agentToken, capture)
		if e := cmd.Start(); e != nil {
			t.Fatal(e)
		}
		waitIdentity(t, func() bool { _, e := os.Stat(capture + ".processes"); return e == nil })
		pending := launch(v)
		parallelCapture := filepath.Join(scratch, "parallel-active-revoke.json")
		parallel := inputAgentCommand(ctx, agent, root, ts.URL, agentToken, parallelCapture)
		if e := parallel.Start(); e != nil {
			t.Fatal(e)
		}
		waitIdentity(t, func() bool { _, e := os.Stat(parallelCapture + ".processes"); return e == nil })
		for _, id := range []int64{task.ID, pending.ID} {
			active, e := s.Get(t.Context(), id)
			if e != nil || active.Attempt != 1 || (active.Status != "claimed" && active.Status != "scanning") {
				t.Fatal("shared identity was not executing concurrently", active, e)
			}
		}
		start(v, "logout")
		for _, id := range []int64{task.ID, pending.ID} {
			active, _ := s.Get(t.Context(), id)
			if active.Status != "cancel_requested" {
				t.Fatal("revoke did not stop every parallel task", active)
			}
		}
		claim, _, e := s.ClaimSDKIdentity(t.Context(), "fixture-input-agent", limits)
		if e != nil || claim != nil {
			t.Fatal("logout raced active task", e)
		}
		if e := cmd.Wait(); e != nil {
			t.Fatal(e)
		}
		if e := parallel.Wait(); e != nil {
			t.Fatal(e)
		}
		t.Setenv("AXIOM_RESULT_TEST_SCENARIO", "")
		run("normal")
		if fresh(v.Identity.ID).CleanupPending {
			t.Fatal("cleanup not confirmed")
		}
		if _, e := os.Stat(identityRoot); !os.IsNotExist(e) {
			t.Fatal("old identity generations retained")
		}
	})
	t.Run("device-instruction-allowlist-and-redacted-failures", func(t *testing.T) {
		v := create("Fixtures / Mock rejected URL", "chatgpt")
		start(v, "login_device_code")
		run("bad-url")
		op := opState(v)
		raw, _ := json.Marshal(op)
		if op.Status != "failed" || strings.Contains(string(raw), "evil.invalid") || strings.Contains(string(raw), "sensitive") {
			t.Fatal("unsafe device instructions exposed")
		}
	})
	t.Run("bounded-key-input-and-cleared-canceled-secret", func(t *testing.T) {
		v := create("Fixtures / Mock full key boundary", "api_key")
		code, body := doJSON(t, ts.URL+"/api/v1/sdk-identities/"+v.Identity.ID+"/operations", token, map[string]any{"generation": v.Identity.Generation, "action": "login_api_key", "apiKey": strings.Repeat("k", 8192)})
		if code != 200 {
			t.Fatal("contract-valid long key rejected", code)
		}
		var out struct {
			Data contracts.SDKIdentityOperation
		}
		json.Unmarshal([]byte(body), &out)
		if e := s.CancelSDKIdentityOperation(t.Context(), number(out.Data.ID)); e != nil {
			t.Fatal(e)
		}
		var encrypted string
		db.QueryRow(`SELECT secret_encrypted FROM sdk_identity_operations WHERE id=$1`, out.Data.ID).Scan(&encrypted)
		if encrypted != "" {
			t.Fatal("canceled key retained")
		}
		v = fresh(v.Identity.ID)
		for _, key := range []string{strings.Repeat("k", 8193), "fixture\ninvalid"} {
			code, _ := doJSON(t, ts.URL+"/api/v1/sdk-identities/"+v.Identity.ID+"/operations", token, map[string]any{"generation": v.Identity.Generation, "action": "login_api_key", "apiKey": key})
			if code != 400 {
				t.Fatal("invalid key accepted", code)
			}
		}
	})
	t.Run("viewer-permission-and-public-secret-boundary", func(t *testing.T) {
		s.CreateAccount(t.Context(), "identity-viewer", auth.HashPassword(secret, "fixture-password"), "viewer")
		viewer := login(t, ts.URL, "identity-viewer", "fixture-password")
		code, raw := getJSON(t, ts.URL+"/api/v1/sdk-identities", viewer)
		if code != 200 || strings.Contains(string(raw), "selected-identity-key") || strings.Contains(string(raw), "MOCK-1234") {
			t.Fatal("public identity leaked secret")
		}
		code, _ = getJSON(t, ts.URL+"/api/v1/sdk-identities/"+chat.Identity.ID+"/operations", viewer)
		if code != 403 {
			t.Fatal(code)
		}
		code, _ = doJSON(t, ts.URL+"/api/v1/sdk-identities", viewer, map[string]any{"name": "forbidden", "kind": "api_key", "agentId": "fixture-input-agent"})
		if code != 403 {
			t.Fatal(code)
		}
	})
	if os.Getenv("AXIOM_TEST_WEB_IDENTITIES") == "1" {
		cmd := exec.Command("node", filepath.Join(root, "code-audit-web/tests/sdk-identities-web-acceptance.mjs"))
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+ts.URL, "AXIOM_TEST_AGENT_BIN="+agent, "AXIOM_TEST_AGENT_TOKEN="+agentToken, "AXIOM_TEST_IDENTITY_MODEL="+fmt.Sprint(model.Data.ID), "AXIOM_TEST_IDENTITY_SNAPSHOT="+fmt.Sprint(snap.ID))
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatal(e, string(out))
		} else {
			t.Log(string(out))
		}
		settings := exec.Command("node", filepath.Join(root, "code-audit-web/tests/settings-web-acceptance.mjs"))
		settings.Dir = root
		settingsReport := os.Getenv("AXIOM_TEST_SETTINGS_REPORT_DIR")
		if settingsReport == "" {
			settingsReport = filepath.Join(root, "output/playwright/settings")
		}
		settings.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+ts.URL, "AXIOM_SETTINGS_TEST_FIXTURE=1", "AXIOM_TEST_SETTINGS_REPORT_DIR="+settingsReport)
		if out, err := settings.CombinedOutput(); err != nil {
			t.Fatal(err, string(out))
		} else {
			t.Log(string(out))
		}
	}
}
func number(v string) int64 { var id int64; fmt.Sscan(v, &id); return id }
func boolRef(v bool) *bool  { return &v }
func waitIdentity(t *testing.T, check func() bool) {
	t.Helper()
	end := time.Now().Add(15 * time.Second)
	for time.Now().Before(end) {
		if check() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal("identity state did not advance")
}
