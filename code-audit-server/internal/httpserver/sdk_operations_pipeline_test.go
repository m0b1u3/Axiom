package httpserver_test

import (
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
)

func runSDKOperationsAcceptance(t *testing.T, s *store.Store, agent, root, url, token, agentToken, scratch string, snapshot int64, encrypted string) {
	cfg, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Pricing: inputFixturePrice(), Name: "SDK operations fixture", Provider: "openai", Model: "fixture-frozen-operation-model", ApiKeyEncrypted: encrypted})
	if e != nil {
		t.Fatal(e)
	}
	t.Setenv("AXIOM_OPERATION_TEST_FINDING_FIELDS", "1")
	t.Setenv("AXIOM_RESULT_TEST_SCENARIO", "rich")
	seed := knowledgeTask(t, url, token, map[string]any{"sourceSnapshotId": snapshot, "modelConfigId": cfg.ID, "includePaths": []string{"src/a,b.ts"}, "maxCostUsd": 5.0, "maxTotalCostUsd": 20.0})
	runInputAgent(t, agent, root, url, agentToken, filepath.Join(scratch, "operation-seed.json"))
	seed, e = s.Get(t.Context(), seed.ID)
	if e != nil || seed.Status != "completed" {
		t.Fatal(e, seed)
	}
	findings, e := s.ListFindings(t.Context(), seed.ID)
	if e != nil || len(findings) == 0 {
		t.Fatal(e, "fixture finding required")
	}
	finding := findings[0]
	baselineStats, e := s.DashboardStats(t.Context())
	if e != nil {
		t.Fatal(e)
	}
	create := func(body map[string]any) *store.Task {
		t.Helper()
		code, raw := doJSON(t, url+fmt.Sprintf("/api/v1/scans/%d/sdk-operations", seed.ID), token, body)
		if code != 200 {
			t.Fatal(code, raw)
		}
		var v struct{ Data store.Task }
		if e := json.Unmarshal([]byte(raw), &v); e != nil {
			t.Fatal(e)
		}
		return &v.Data
	}
	code, _ := doJSON(t, url+fmt.Sprintf("/api/v1/scans/%d/sdk-operations", seed.ID), token, map[string]any{"kind": "validate", "findingId": fmt.Sprint(finding.ID), "originAttempt": 1})
	if code != 400 {
		t.Fatal("missing unknown-cost acknowledgement accepted", code)
	}
	code, _ = doJSON(t, url+fmt.Sprintf("/api/v1/scans/%d/sdk-operations", seed.ID), token, map[string]any{"kind": "validate", "findingId": "9999999", "originAttempt": 1, "acknowledgeUnknownCost": true})
	if code != 404 {
		t.Fatal("cross-scan finding accepted", code)
	}
	ids := map[string]int64{"seed": seed.ID}
	capture := filepath.Join(scratch, "operation-capture.json")
	for _, scenario := range []string{"preflight", "preflight-failure", "reportable", "suppressed", "not_applicable", "deferred", "validation-failure", "validation-large"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("AXIOM_OPERATION_TEST_SCENARIO", scenario)
			if scenario == "reportable" {
				cfg.Model = "fixture-updated-config-model"
				if e = s.UpdateModelConfig(t.Context(), cfg.ID, *cfg); e != nil {
					t.Fatal(e)
				}
			}
			body := map[string]any{"kind": "preflight"}
			if !strings.HasPrefix(scenario, "preflight") {
				body = map[string]any{"kind": "validate", "findingId": fmt.Sprint(finding.ID), "originAttempt": seed.Attempt, "acknowledgeUnknownCost": true}
			}
			task := create(body)
			if task.InputContractVersion != 3 || task.SDKOperation == nil {
				t.Fatal("operation input not marked")
			}
			oldCaps := contracts.ObservationCapabilities{JobVersions: []int64{3, 4}, ProgressVersions: []int64{2}, EventVersions: []int64{2}, HeartbeatVersions: []int64{1}}
			old, e := s.ClaimExecutionWithObservations(t.Context(), "fixture-old-agent", store.ClaimLimits{Global: 4, PerModel: 4, PerProject: 4, Lease: 60000000000, OutputRoot: filepath.Join(scratch, "old")}, contracts.InputCapabilities{AssignmentVersions: []contracts.SchemaVersion{1}, JobVersions: []contracts.SchemaVersion{1}, MaxCostUSD: true}, contracts.ResultCapabilities{JobVersions: []int64{2}, ResultVersions: []int64{1}, CompletionVersions: []int64{1}, CommitVersions: []int64{1}}, oldCaps)
			if e != nil || old != nil {
				t.Fatal("old agent claimed operation", e, old)
			}
			runInputAgent(t, agent, root, url, agentToken, capture)
			archived, e := s.AttemptResult(t.Context(), task.ID, 1)
			if e != nil || archived.Result == nil {
				current, _ := s.Get(t.Context(), task.ID)
				t.Fatal("operation archive missing", e, current)
			}
			op, e := store.OperationResult(*archived.Result)
			if e != nil || op == nil {
				t.Fatal(e)
			}
			if len(archived.Result.Findings) != 0 {
				t.Fatal("operation imported findings")
			}
			if e = s.CompleteContractExecution(t.Context(), *archived.Completion); e != nil {
				t.Fatal("idempotent completion failed", e)
			}
			raw, e := os.ReadFile(capture)
			if e != nil {
				t.Fatal(e)
			}
			var called struct {
				Kind    string
				Source  string
				Options map[string]any
				Config  struct {
					Overrides map[string]any `json:"codexOverrides"`
				}
				Leaked bool `json:"leakedServerCredentials"`
			}
			if e = json.Unmarshal(raw, &called); e != nil {
				t.Fatal(e)
			}
			if called.Leaked || called.Source != seed.SourcePath || called.Config.Overrides["model"] != seed.Model {
				t.Fatal("operation source/model/credential boundary lost", called)
			}
			if called.Kind == "validate" {
				encoded, _ := json.Marshal(called.Options["finding"])
				if !strings.Contains(string(encoded), "fixture-reproduction-field") {
					t.Fatal("secret-shaped finding evidence was dropped")
				}
			}
			if called.Kind == "preflight" {
				if op.Cost.State != "known" || op.Cost.EstimatedUSD == nil || *op.Cost.EstimatedUSD != 0 {
					t.Fatal("preflight cost not zero")
				}
			} else {
				if op.Cost.State != "unknown" || op.Cost.EstimatedUSD != nil || archived.Result.Cost.Value != nil {
					t.Fatal("unknown validation cost became zero")
				}
				for key := range called.Options {
					if key != "repositoryPath" && key != "finding" && key != "outputDir" && key != "auth" && key != "signal" {
						t.Fatal("unsupported validation option", key)
					}
				}
				if scenario != "validation-large" && !strings.HasSuffix(scenario, "failure") && (op.Validation == nil || string(op.Validation.Disposition) != scenario) {
					t.Fatal("native disposition changed", op)
				}
				budget, e := s.BudgetSnapshot(t.Context(), task.ID)
				if e != nil || len(budget.UnknownAttempts) != 1 {
					t.Fatal("unknown validation cost not protected", e, budget)
				}
				if _, e = s.Retry(t.Context(), task.ID); e == nil {
					t.Fatal("validation retry bypassed acknowledgement")
				}
				if e = s.DeleteScan(t.Context(), task.ID); e == nil {
					t.Fatal("unknown validation cost evidence deleted")
				}
			}
			if strings.HasSuffix(scenario, "failure") || scenario == "validation-large" {
				if op.Error == nil || archived.Result.Status != "failed" || strings.Contains(op.Error.Message, "fixture-private-operation-token") {
					t.Fatal("operation error lost or secret leaked")
				}
			}
			if scenario == "validation-large" {
				found := false
				for _, ref := range archived.Result.Artifacts {
					if ref.RelativePath == "raw/codex-security/sdk-operation-native.json" && ref.SizeBytes > 8*1024*1024 {
						found = true
					}
				}
				if !found {
					t.Fatal("large native validation report lost")
				}
			}
			ids[scenario] = task.ID
		})
	}
	stats, e := s.DashboardStats(t.Context())
	if e != nil || stats.Scans != baselineStats.Scans || stats.Failed != baselineStats.Failed {
		t.Fatal("SDK operations contaminated audit statistics", e)
	}
	for _, route := range []string{"compare", "report", "report.pdf"} {
		code, _ := getJSON(t, url+fmt.Sprintf("/api/v1/scans/%d/%s", ids["reportable"], route), token)
		if code != 409 {
			t.Fatal("operation was presented as a scan report", route, code)
		}
	}
	previous, e := s.FindPreviousScan(t.Context(), ids["validation-failure"]+1, seed.SourceSnapshotID, seed.SourcePath)
	if e != nil || previous.ID != seed.ID {
		t.Fatal("scan comparison selected an SDK operation", e, previous)
	}
	after, e := s.ListFindings(t.Context(), seed.ID)
	if e != nil || after[0].Status != finding.Status || string(after[0].Raw) != string(finding.Raw) {
		t.Fatal("validation overwrote manual finding or evidence", e)
	}
	budget, e := s.BudgetSnapshot(t.Context(), seed.ID)
	if e != nil || len(budget.Attempts) != 1 || len(budget.UnknownAttempts) != 0 {
		t.Fatal("operation cost contaminated origin ledger", e)
	}
	code, raw := getJSON(t, url+fmt.Sprintf("/api/v1/scans/%d/attempts/1/sdk-sessions", seed.ID), token)
	if code != 200 {
		t.Fatal(code, string(raw))
	}
	var page struct {
		Data struct {
			Summary contracts.SDKSessionSummary
			Items   []contracts.SDKSessionEvent
			Next    int64
			HasMore bool
		}
	}
	if e = json.Unmarshal(raw, &page); e != nil {
		t.Fatal(e)
	}
	if len(page.Data.Items) != 50 || !page.Data.HasMore || page.Data.Summary.PersistedCount != 73 {
		t.Fatal("session first page incomplete")
	}
	code, raw = getJSON(t, url+fmt.Sprintf("/api/v1/scans/%d/attempts/1/sdk-sessions?after=50", seed.ID), token)
	if code != 200 || strings.Contains(string(raw), "fixture-private-session-token") {
		t.Fatal("session page identity or redaction failed", code)
	}
	if e = json.Unmarshal(raw, &page); e != nil || len(page.Data.Items) != 23 || page.Data.HasMore {
		t.Fatal("session second page incomplete", e)
	}
	if os.Getenv("AXIOM_TEST_WEB_OPERATIONS") == "1" {
		cmd := exec.Command("node", filepath.Join(root, "code-audit-web/tests/sdk-operations-web-acceptance.mjs"))
		cmd.Dir = root
		data, _ := json.Marshal(ids)
		cmd.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+url, "AXIOM_TEST_OPERATION_IDS="+string(data), "AXIOM_TEST_OPERATION_FINDING="+fmt.Sprint(finding.ID), "AXIOM_TEST_AGENT_BIN="+agent, "AXIOM_TEST_AGENT_TOKEN="+agentToken, "AXIOM_TEST_OPERATION_SNAPSHOT="+fmt.Sprint(snapshot), "AXIOM_TEST_OPERATION_MODEL="+fmt.Sprint(cfg.ID))
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("SDK operation Web acceptance: %v %s", e, out)
		}
	}
	current, e := s.Get(t.Context(), seed.ID)
	if e != nil {
		t.Fatal(e)
	}
	journal := filepath.Join(current.OutputPath, "logs/sdk-session-events.jsonl")
	original, e := os.ReadFile(journal)
	if e != nil {
		t.Fatal(e)
	}
	changed := append([]byte{}, original...)
	changed[0] = ' '
	if e = os.WriteFile(journal, changed, 0600); e != nil {
		t.Fatal(e)
	}
	code, _ = getJSON(t, url+fmt.Sprintf("/api/v1/scans/%d/attempts/1/sdk-sessions", seed.ID), token)
	if code != 409 {
		t.Fatal("tampered session journal accepted", code)
	}
	if e = os.WriteFile(journal, original, 0600); e != nil {
		t.Fatal(e)
	}
}
