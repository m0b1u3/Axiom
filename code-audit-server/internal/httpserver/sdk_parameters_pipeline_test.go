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

// Called by the disposable component pipeline. The browser creates real queued
// tasks; only the SDK inference/auth/publication boundaries are substituted.
func runSDKParametersWebAcceptance(t *testing.T, s *store.Store, agent, root, url, token, agentToken, scratch string, snapshot int64, plan contracts.TaskID) {
	t.Helper()
	browser := func(phase string, ids any) []byte {
		t.Helper()
		raw, _ := json.Marshal(ids)
		cmd := exec.Command("node", filepath.Join(root, "code-audit-web/tests/sdk-parameters-web-acceptance.mjs"))
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+url, "AXIOM_TEST_PARAMETER_PHASE="+phase, "AXIOM_TEST_PARAMETER_IDS="+string(raw), "AXIOM_TEST_PARAMETER_SNAPSHOT="+fmt.Sprint(snapshot), "AXIOM_TEST_PARAMETER_PLAN="+string(plan))
		out, e := cmd.Output()
		if e != nil {
			if failure, ok := e.(*exec.ExitError); ok {
				t.Fatalf("SDK parameters browser %s: %v %s", phase, e, failure.Stderr)
			}
			t.Fatal(e, string(out))
		}
		return out
	}
	var ids struct{ ScanID, ComponentID, ModelID, KnowledgeID int64 }
	if e := json.Unmarshal(browser("create", nil), &ids); e != nil {
		t.Fatal(e)
	}
	if ids.ScanID < 1 || ids.ComponentID < 1 || ids.ModelID < 1 || ids.KnowledgeID < 1 {
		t.Fatal("Browser did not create the fixture objects")
	}
	// This capability set has no v11 runtime. It must not consume the new inputs.
	code, body := doJSON(t, url+"/api/internal/v4/executions/claim", agentToken, map[string]any{
		"agentId": "fixture-old-parameter-node", "inputCapabilities": map[string]any{"assignmentVersions": []int{1}, "jobVersions": []int{1}, "maxCostUsd": true},
		"resultCapabilities":      map[string]any{"jobVersions": []int{2}, "resultVersions": []int{1}, "completionVersions": []int{1}, "commitVersions": []int{1}},
		"observationCapabilities": map[string]any{"jobVersions": []int{3, 4, 8}, "progressVersions": []int{2}, "eventVersions": []int{2}, "heartbeatVersions": []int{1}}})
	if code != 200 || strings.Contains(body, `"assignment"`) {
		t.Fatal("Old node claimed parameterized work", code, body)
	}
	t.Setenv("AXIOM_COMPONENT_SCAN_FIXTURE", "0")
	capture := filepath.Join(scratch, "web-sdk-parameters-standard.json")
	runInputAgent(t, agent, root, url, agentToken, capture)
	var ordinary struct {
		Config struct {
			Overrides map[string]any `json:"codexOverrides"`
		}
		Options map[string]any
	}
	raw, e := os.ReadFile(capture)
	if e != nil {
		t.Fatal(e)
	}
	json.Unmarshal(raw, &ordinary)
	if ordinary.Config.Overrides["model_reasoning_effort"] != "medium" {
		t.Fatal("Frozen ordinary reasoning did not reach SDK", string(raw))
	}
	if os.Getenv("AXIOM_TEST_NATIVE_CONCURRENCY") == "1" {
		for key, want := range map[string]any{"mode": "deep", "workers": float64(129), "subagents": float64(65), "maxDiscoveryRuns": float64(1001), "stopAfterNoNew": float64(1002), "maxTimeHours": float64(47.0 / 60), "maxCostUsd": float64(3)} {
			if ordinary.Options[key] != want {
				t.Fatalf("Web -> SDK parameter %s = %v, want %v", key, ordinary.Options[key], want)
			}
		}
		paths, _ := ordinary.Options["target"].([]any)
		if len(paths) != 1 || paths[0] != "api/a.ts" {
			t.Fatal("operator scope changed", ordinary.Options["target"])
		}
	}
	task, e := s.Get(t.Context(), ids.ScanID)
	if e != nil || task.Status != "completed" || task.SDKParameters == nil || task.SDKParameters.ReasoningEffort.Value != "medium" {
		t.Fatal(e, task)
	}
	current, e := s.GetModelConfig(t.Context(), ids.ModelID)
	if e != nil || current.ReasoningEffort != "high" {
		t.Fatal("Browser model edit was not persisted", e, current)
	}
	t.Setenv("AXIOM_COMPONENT_SCAN_FIXTURE", "1")
	capture = filepath.Join(scratch, "web-sdk-parameters-components.json")
	runInputAgent(t, agent, root, url, agentToken, capture)
	raw, e = os.ReadFile(capture + ".component-calls")
	if e != nil {
		t.Fatal(e)
	}
	lines := strings.Split(strings.TrimSpace(string(raw)), "\n")
	if len(lines) != 3 {
		t.Fatal("Expected three component SDK calls", len(lines))
	}
	if os.Getenv("AXIOM_TEST_NATIVE_CONCURRENCY") == "1" {
		var scheduler struct{ Workers, ComponentCount int }
		body, e := os.ReadFile(capture + ".component-options")
		if e != nil {
			t.Fatal(e)
		}
		if e = json.Unmarshal(body, &scheduler); e != nil || scheduler.Workers != 9 || scheduler.ComponentCount != 3 {
			t.Fatal("component workers were clamped before native SDK", e, string(body))
		}
	}
	for _, line := range lines {
		var v struct {
			Config struct {
				Overrides map[string]any `json:"codexOverrides"`
			}
			Options   map[string]any
			Knowledge []struct{ Content string }
		}
		if e := json.Unmarshal([]byte(line), &v); e != nil {
			t.Fatal(e)
		}
		if v.Config.Overrides["model_reasoning_effort"] != "medium" || v.Options["scanPrompt"] != "合成组件扫描指令\n第二行" || v.Options["postScanPrompt"] != "合成组件后处理\n保留证据" || len(v.Knowledge) != 1 || !strings.Contains(v.Knowledge[0].Content, "冻结第一版") || strings.Contains(v.Knowledge[0].Content, "第二版") {
			t.Fatal("Component SDK inputs changed or disappeared", line)
		}
	}
	component, e := s.Get(t.Context(), ids.ComponentID)
	if e != nil || component.Status != "completed" || component.SDKParameters == nil || !component.SDKParameters.ComponentInput.Present {
		t.Fatal(e, component)
	}
	// Preflight retains the source's medium configuration despite the edited model.
	code, body = doJSON(t, url+fmt.Sprintf("/api/v1/scans/%d/sdk-operations", ids.ScanID), token, map[string]any{"kind": "preflight"})
	if code != 200 {
		t.Fatal(code, body)
	}
	capture = filepath.Join(scratch, "web-sdk-parameters-preflight.json")
	runInputAgent(t, agent, root, url, agentToken, capture)
	raw, e = os.ReadFile(capture)
	if e != nil {
		t.Fatal(e)
	}
	json.Unmarshal(raw, &ordinary)
	if ordinary.Config.Overrides["model_reasoning_effort"] != "medium" {
		t.Fatal("Preflight lost frozen reasoning", string(raw))
	}
	t.Log(string(browser("verify", ids)))
}
