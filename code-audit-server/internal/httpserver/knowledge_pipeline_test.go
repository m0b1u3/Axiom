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

func knowledgeTask(t *testing.T, url, token string, body map[string]any) *store.Task {
	t.Helper()
	status, raw := doJSON(t, url+"/api/v1/scans", token, body)
	if status != 200 {
		t.Fatal(status, raw)
	}
	if strings.Contains(raw, `"parentState"`) {
		t.Fatal("private SDK state leaked into the Web task response")
	}
	var v struct {
		Data store.Task `json:"data"`
	}
	if e := json.Unmarshal([]byte(raw), &v); e != nil {
		t.Fatal(e)
	}
	return &v.Data
}
func assertKnowledgeSDK(t *testing.T, capture string, input contracts.ScanInput, subagents bool) {
	t.Helper()
	raw, e := os.ReadFile(capture)
	if e != nil {
		t.Fatal(e)
	}
	var v struct {
		Options   map[string]any                   `json:"options"`
		Knowledge []struct{ Path, Content string } `json:"knowledge"`
	}
	if e = json.Unmarshal(raw, &v); e != nil {
		t.Fatal(e)
	}
	for _, pair := range []struct{ Key, Value string }{{"workflowId", input.WorkflowID.Value}, {"safetyIdentifier", input.SafetyIdentifier.Value}, {"parentScanId", input.ParentScanID.Value}, {"expectedPluginVersion", input.ExpectedPluginVersion.Value}, {"postScanPrompt", input.PostScanPrompt.Value}} {
		if v.Options[pair.Key] != pair.Value {
			t.Fatalf("SDK parameter lost: %s", pair.Key)
		}
	}
	if v.Options["failureSeverity"] != string(input.FailureSeverity.Value) {
		t.Fatal("severity threshold lost")
	}
	if !strings.Contains(v.Options["scanPrompt"].(string), input.ScanPrompt.Value) {
		t.Fatal("scan instruction lost")
	}
	if subagents {
		if v.Options["subagents"] != float64(0) {
			t.Fatal("zero subagents lost")
		}
	} else if v.Options["validationPrompt"] != input.ValidationPrompt.Value {
		t.Fatal("custom validation lost")
	}
	if len(v.Knowledge) != 1 || v.Knowledge[0].Content != input.Knowledge[0].Content || !strings.HasSuffix(v.Knowledge[0].Path, "/runtime/knowledge/"+input.Knowledge[0].VersionID+".md") {
		t.Fatal("frozen knowledge did not reach SDK", v.Knowledge)
	}
}
func runKnowledgeInputAcceptance(t *testing.T, s *store.Store, agent, root, url, token, agentToken, scratch string, snapshot int64, encrypted string) {
	ctx := t.Context()
	first, e := s.SaveKnowledge(ctx, "", 0, "API 合成知识", "第一版不可变知识\n第二行")
	if e != nil {
		t.Fatal(e)
	}
	cfg, e := s.CreateModelConfig(ctx, store.ModelConfigRow{Name: "Fixture advanced input", Provider: "openai", Model: "fixture-advanced", ApiKeyEncrypted: encrypted})
	if e != nil {
		t.Fatal(e)
	}
	seed := knowledgeTask(t, url, token, map[string]any{"sourceSnapshotId": snapshot, "modelConfigId": cfg.ID})
	runInputAgent(t, agent, root, url, agentToken, filepath.Join(scratch, "parent-seed.json"))
	seedTask, e := s.Get(ctx, seed.ID)
	if e != nil {
		t.Fatal(e)
	}
	stateFile := filepath.Join(seedTask.OutputPath, "normalized/parent-state.json")
	stateRaw, e := os.ReadFile(stateFile)
	if e != nil {
		t.Fatal(e)
	}
	status, metadata := getJSON(t, url+fmt.Sprintf("/api/v1/scans/%d/parent-state", seed.ID), token)
	if status != 200 || strings.Contains(string(metadata), `"sha256"`) || strings.Contains(string(metadata), `"sourceHash"`) {
		t.Fatal("parent selection must expose only metadata", status, string(metadata))
	}
	for _, alter := range []func(*contracts.SDKParentState){
		func(v *contracts.SDKParentState) { v.Execution.Attempt++ },
		func(v *contracts.SDKParentState) { v.SourceHash = strings.Repeat("0", 64) },
		func(v *contracts.SDKParentState) { v.ResultSHA256 = strings.Repeat("0", 64) },
		func(v *contracts.SDKParentState) { v.Data = "eA==" },
	} {
		state, e := contracts.DecodeSDKParentState(stateRaw, nil)
		if e != nil {
			t.Fatal(e)
		}
		alter(&state)
		changed, e := json.Marshal(state)
		if e != nil {
			t.Fatal(e)
		}
		if e = os.WriteFile(stateFile, changed, 0600); e != nil {
			t.Fatal(e)
		}
		status, _ := doJSON(t, url+"/api/v1/scans", token, map[string]any{"sourceSnapshotId": snapshot, "modelConfigId": cfg.ID, "parentTaskId": seed.ID})
		if e = os.WriteFile(stateFile, stateRaw, 0600); e != nil {
			t.Fatal(e)
		}
		if status != 400 {
			t.Fatal("corrupt parent evidence accepted", status)
		}
	}
	var standardTask *store.Task
	for _, mode := range []string{"standard", "deep"} {
		input := contracts.ScanInput{SchemaVersion: 1, Knowledge: []contracts.KnowledgeDocument{}, ScanPrompt: contracts.Some("合成扫描补充\n第二行"), PostScanPrompt: contracts.Some("合成后处理"), WorkflowID: contracts.Some("fixture-workflow"), SafetyIdentifier: contracts.Some("fixture-anonymous"), ExpectedPluginVersion: contracts.Some("0.1.94"), FailureSeverity: contracts.Some(contracts.ScanInputFailureSeverityInformational)}
		if mode == "standard" {
			input.ValidationPrompt = contracts.Some("合成独立验证要求")
		} else {
			input.Subagents = contracts.Some(int64(0))
		}
		task := knowledgeTask(t, url, token, map[string]any{"sourceSnapshotId": snapshot, "modelConfigId": cfg.ID, "scanMode": mode, "scanInput": input, "parentTaskId": seed.ID, "knowledgeVersionIds": []int64{first.ID}, "maxCostUsd": 2, "maxTotalCostUsd": 3})
		if mode == "standard" {
			standardTask = task
		}
		if task.ScanInput == nil || task.ScanInput.Knowledge[0].Content != first.Content {
			t.Fatal("API did not freeze selected knowledge")
		}
		capture := filepath.Join(scratch, "advanced-"+mode+".json")
		seedTask, e := s.Get(ctx, seed.ID)
		if e != nil {
			t.Fatal(e)
		}
		stateFile := filepath.Join(seedTask.OutputPath, "normalized/parent-state.json")
		if e = os.Rename(stateFile, stateFile+".held"); e != nil {
			t.Fatal(e)
		}
		runInputAgent(t, agent, root, url, agentToken, capture)
		if e = os.Rename(stateFile+".held", stateFile); e != nil {
			t.Fatal(e)
		}

		currentBefore, e := s.Get(ctx, task.ID)
		if e != nil || currentBefore.Status != "completed" {
			t.Fatalf("advanced task before SDK capture: %v %s %s", e, currentBefore.ErrorCode, currentBefore.ErrorMessage)
		}
		assertKnowledgeSDK(t, capture, *task.ScanInput, mode == "deep")
		current, e := s.Get(ctx, task.ID)
		if e != nil || current.Status != "completed" {
			t.Fatal("advanced completion", e, current)
		}
		job, e := os.ReadFile(filepath.Join(current.OutputPath, "runtime/job.json"))
		if e != nil {
			t.Fatal(e)
		}
		if _, e = contracts.DecodeScannerJobV4(job, &contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(task.ID)), Attempt: 1}); e != nil {
			t.Fatal(e)
		}
	}
	status, _ = doJSON(t, url+"/api/v1/scans", token, map[string]any{"sourceSnapshotId": snapshot, "modelConfigId": cfg.ID, "parentTaskId": standardTask.ID, "scanInput": map[string]any{"schemaVersion": 1, "knowledge": []any{}, "workflowId": "fixture-workflow"}})
	if status != 400 {
		t.Fatal("a completed parent workflow was reused as a new scan", status)
	}

	if _, e = s.SaveKnowledge(ctx, first.DocumentID, 1, "已更新的文档", "更新后的正文"); e != nil {
		t.Fatal(e)
	}
	if e = s.ArchiveKnowledge(ctx, first.DocumentID, true); e != nil {
		t.Fatal(e)
	}
	status, body := doJSON(t, url+fmt.Sprintf("/api/v1/scans/%d/retry", standardTask.ID), token, map[string]any{})
	if status != 200 {
		t.Fatal("retry frozen input", status, body)
	}
	retryCapture := filepath.Join(scratch, "advanced-retry.json")
	runInputAgent(t, agent, root, url, agentToken, retryCapture)
	assertKnowledgeSDK(t, retryCapture, *standardTask.ScanInput, false)
	retryRaw, e := os.ReadFile(retryCapture)
	if e != nil {
		t.Fatal(e)
	}
	var retry struct {
		Options map[string]any `json:"options"`
	}
	json.Unmarshal(retryRaw, &retry)
	if retry.Options["maxCostUsd"] != 1.875 {
		t.Fatal("advanced retry bypassed remaining task budget", retry.Options["maxCostUsd"])
	}
	for _, extra := range []map[string]any{{"scanInput": map[string]any{"schemaVersion": 1, "knowledge": []any{}, "subagents": 0}}, {"scanInput": map[string]any{"schemaVersion": 1, "knowledge": []any{}, "knowledgeBasePaths": []string{"/etc/passwd"}}}, {"knowledgeVersionIds": []int64{first.ID, first.ID}}, {"knowledgeVersionIds": []int64{999999}}, {"scanInput": map[string]any{"schemaVersion": 1, "knowledge": []any{}, "expectedPluginVersion": "latest"}}} {
		body := map[string]any{"sourceSnapshotId": snapshot, "modelConfigId": cfg.ID}
		for k, v := range extra {
			body[k] = v
		}
		status, _ := doJSON(t, url+"/api/v1/scans", token, body)
		if status != 400 {
			t.Fatal("invalid input accepted", extra, status)
		}
	}
}
func runKnowledgeWebAcceptance(t *testing.T, s *store.Store, agent, root, url, token, agentToken, scratch string, snapshot int64, encrypted string) {
	cfg, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Browser knowledge model", Provider: "openai", Model: "fixture-web-knowledge", ApiKeyEncrypted: encrypted})
	if e != nil {
		t.Fatal(e)
	}
	seed := knowledgeTask(t, url, token, map[string]any{"sourceSnapshotId": snapshot, "modelConfigId": cfg.ID})
	runInputAgent(t, agent, root, url, agentToken, filepath.Join(scratch, "browser-parent-seed.json"))
	run := func(phase string, id int64) []byte {
		t.Helper()
		cmd := exec.CommandContext(t.Context(), "node", filepath.Join(root, "code-audit-web/tests/knowledge-web-acceptance.mjs"))
		cmd.Dir = root
		cmd.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+url, "AXIOM_TEST_KNOWLEDGE_PHASE="+phase, "AXIOM_TEST_KNOWLEDGE_TASK="+fmt.Sprint(id), "AXIOM_TEST_KNOWLEDGE_SNAPSHOT="+fmt.Sprint(snapshot), "AXIOM_TEST_KNOWLEDGE_PARENT="+fmt.Sprint(seed.ID), "AXIOM_TEST_KNOWLEDGE_MODEL="+fmt.Sprint(cfg.ID))
		out, e := cmd.Output()
		if e != nil {
			if exit, ok := e.(*exec.ExitError); ok {
				t.Fatalf("knowledge browser: %v %s", e, exit.Stderr)
			}
			t.Fatal(e)
		}
		return out
	}
	out := run("create", 0)
	var result struct {
		ID int64 `json:"id"`
	}
	if e = json.Unmarshal(out, &result); e != nil || result.ID < 1 {
		t.Fatal(e, string(out))
	}
	frozen, e := s.Get(t.Context(), result.ID)
	if e != nil || frozen.ScanInput == nil {
		t.Fatal("browser input missing", e)
	}
	capture := filepath.Join(scratch, "browser-knowledge-sdk.json")
	runInputAgent(t, agent, root, url, agentToken, capture)
	assertKnowledgeSDK(t, capture, *frozen.ScanInput, false)
	_ = run("verify", result.ID)
}
