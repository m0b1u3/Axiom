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

func runNativeSDKAcceptance(t *testing.T, s *store.Store, agent, root, url, token, agentToken, scratch string, snapshot int64, encrypted string) {
	cfg, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Native SDK Fixture", Provider: "openai", Model: "fixture-native-configured", ApiKeyEncrypted: encrypted})
	if e != nil {
		t.Fatal(e)
	}
	ids := map[string]int64{}
	var seed *store.Task
	for _, scenario := range []string{"native-rich", "native-zero", "native-unknown", "native-no-exports", "native-bad-sarif", "native-bad-history", "native-old", "native-future"} {
		t.Run(scenario, func(t *testing.T) {
			t.Setenv("AXIOM_RESULT_TEST_SCENARIO", scenario)
			body := map[string]any{"sourceSnapshotId": snapshot, "modelConfigId": cfg.ID}
			if scenario == "native-rich" && seed != nil {
				body["parentTaskId"] = seed.ID
			}
			task := knowledgeTask(t, url, token, body)
			runInputAgent(t, agent, root, url, agentToken, filepath.Join(scratch, scenario+".json"))
			current, e := s.Get(t.Context(), task.ID)
			if e != nil || current.Status != "completed" {
				t.Fatal("optional native enrichment lost core result", e, current)
			}
			archive, e := s.AttemptResult(t.Context(), task.ID, 1)
			if e != nil || archive.Result == nil {
				t.Fatal(e)
			}
			ids[scenario] = task.ID
			native, e := contracts.NativeSDKResult(*archive.Result)
			if scenario == "native-old" {
				if e != nil || native != nil {
					t.Fatal("historical result changed", e)
				}
				code, _ := getJSON(t, url+fmt.Sprintf("/api/v1/scans/%d/attempts/1/sdk-cost-estimate", task.ID), token)
				if code != 404 {
					t.Fatal(code)
				}
				return
			}
			if scenario == "native-future" {
				if e == nil {
					t.Fatal("unknown native extension accepted")
				}
				code, _ := getJSON(t, url+fmt.Sprintf("/api/v1/scans/%d/attempts/1/sdk-cost-estimate", task.ID), token)
				if code != 409 {
					t.Fatal(code)
				}
				return
			}
			if e != nil || native == nil {
				t.Fatal(e)
			}
			if native.UsageEstimate.Execution != archive.Result.Execution || native.ScanID == nil {
				t.Fatal("native identity lost")
			}
			code, raw := getJSON(t, url+fmt.Sprintf("/api/v1/scans/%d/attempts/1/sdk-cost-estimate", task.ID), token)
			if code != 200 {
				t.Fatal(code, string(raw))
			}
			var estimated struct {
				Data contracts.SDKCostEstimate `json:"data"`
			}
			if e = json.Unmarshal(raw, &estimated); e != nil {
				t.Fatal(e)
			}
			if archive.Result.Cost.Value == nil || *archive.Result.Cost.Value.EstimatedUSD != 1.125 {
				t.Fatal("usage estimate changed the scan cost ledger")
			}
			switch scenario {
			case "native-rich":
				if native.Reports.Markdown.Status != "available" || native.Reports.Sarif.Status != "available" || native.Diagnostics.WarningCount != 40 || native.Diagnostics.ObserverErrorCount != 1 || native.Diagnostics.OmittedCount != 9 || native.DiagnosticJournal == nil {
					t.Fatal("native enrichment missing", native)
				}
				journalCode, journal := getJSON(t, url+fmt.Sprintf("/api/v1/scans/%d/attempts/1/artifacts?path=logs/sdk-diagnostics.jsonl", task.ID), token)
				if journalCode != 200 || strings.Count(string(journal), "\n") != 41 || strings.Contains(string(journal), "fixture-private-diagnostic-token") {
					t.Fatal("diagnostic journal incomplete or credential leaked", journalCode)
				}
				if len(native.RepositoryHistory.Records) != 2 || native.RepositoryHistory.Records[0].Status != "closed" || native.RepositoryHistory.Records[0].ConfirmedInLatestScan {
					t.Fatal("native history semantics changed")
				}
				seed = current
			case "native-zero":
				if estimated.Data.EstimatedUSD == nil || *estimated.Data.EstimatedUSD != 0 {
					t.Fatal("known zero became unknown")
				}
			case "native-unknown":
				if estimated.Data.EstimatedUSD != nil || native.RepositoryHistory.State != "not_reported" {
					t.Fatal("unknown became known")
				}
			case "native-no-exports":
				if native.Reports.Markdown.Status != "not_generated" || native.Reports.Sarif.Status != "not_generated" {
					t.Fatal("missing report became available")
				}
			case "native-bad-sarif":
				if native.Reports.Sarif.Status != "invalid" || native.Reports.Markdown.Status != "available" {
					t.Fatal("invalid SARIF not isolated")
				}
			case "native-bad-history":
				if native.RepositoryHistory.State != "invalid" {
					t.Fatal("invalid history accepted")
				}
			}
		})
	}
	// A child retains the SDK parent's real native ID. The Web link must resolve
	// through the normalized index rather than treating that ID as a task number.
	t.Setenv("AXIOM_RESULT_TEST_SCENARIO", "native-rich")
	child := knowledgeTask(t, url, token, map[string]any{"sourceSnapshotId": snapshot, "modelConfigId": cfg.ID, "parentTaskId": seed.ID})
	runInputAgent(t, agent, root, url, agentToken, filepath.Join(scratch, "native-child.json"))
	ids["native-child"] = child.ID
	parentID := fmt.Sprintf("fixture-native-%d-1", seed.ID)
	if _, e := s.SDKExecutionLinks(t.Context(), parentID, seed.SourceContentHash, 1); e != nil {
		t.Fatal("native links query", e)
	}
	code, raw := getJSON(t, url+fmt.Sprintf("/api/v1/scans/%d/attempts/1/sdk-history-links?scanId=%s", child.ID, parentID), token)
	var links struct {
		Data store.SDKExecutionLinks `json:"data"`
	}
	if code != 200 || json.Unmarshal(raw, &links) != nil || links.Data.Total != 1 || string(links.Data.Items[0].Execution.TaskID) != fmt.Sprint(seed.ID) {
		t.Fatal("native parent execution did not resolve", code, string(raw))
	}
	code, _ = getJSON(t, url+fmt.Sprintf("/api/v1/scans/%d/attempts/1/sdk-history-links?scanId=unreferenced-native-id", child.ID), token)
	if code != 400 {
		t.Fatal("arbitrary native lookup allowed", code)
	}
	if os.Getenv("AXIOM_TEST_WEB_INPUT") == "1" {
		bytes, _ := json.Marshal(ids)
		cmd := exec.CommandContext(t.Context(), "node", filepath.Join(root, "code-audit-web/tests/sdk-native-web-acceptance.mjs"))
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+url, "AXIOM_TEST_NATIVE_IDS="+string(bytes), "AXIOM_TEST_NATIVE_PARENT="+fmt.Sprint(seed.ID))
		if out, e := cmd.CombinedOutput(); e != nil {
			t.Fatalf("native Web acceptance: %v %s", e, out)
		}
	}
	current, e := s.Get(t.Context(), child.ID)
	if e != nil {
		t.Fatal(e)
	}
	sarifFile := filepath.Join(current.OutputPath, "raw/codex-security/exports/results.sarif")
	original, e := os.ReadFile(sarifFile)
	if e != nil {
		t.Fatal(e)
	}
	changed := append([]byte(nil), original...)
	changed[0] = ' '
	if e = os.WriteFile(sarifFile, changed, 0600); e != nil {
		t.Fatal(e)
	}
	code, _ = getJSON(t, url+fmt.Sprintf("/api/v1/scans/%d/attempts/1/artifacts?path=raw/codex-security/exports/results.sarif", child.ID), token)
	if e = os.WriteFile(sarifFile, original, 0600); e != nil {
		t.Fatal(e)
	}
	if code != 409 {
		t.Fatal("changed native SARIF was served", code)
	}

}
