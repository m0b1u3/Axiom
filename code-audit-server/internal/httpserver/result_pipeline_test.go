package httpserver_test

import (
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func assertResultPipeline(t *testing.T, s *store.Store, url, token string, task *store.Task, scenario string) {
	t.Helper()
	archive, e := s.AttemptResult(t.Context(), task.ID, task.Attempt)
	if e != nil {
		t.Fatal(e)
	}
	if !archive.Available || archive.ContractVersion != 1 || archive.Completion == nil {
		t.Fatalf("no versioned completion: %+v", archive)
	}
	if strings.HasPrefix(scenario, "wrong-") || scenario == "tampered" || scenario == "no-commit" || scenario == "bad-json" || scenario == "oversized" {
		if task.Status != "failed" || task.ErrorCode != "invalid_result" || archive.Result != nil || archive.Completion.Process.ResultCommitted {
			t.Fatalf("invalid result appeared successful: %+v %+v", task, archive)
		}
		return
	}
	result := archive.Result
	if result == nil {
		t.Fatal("full SDK result was dropped")
	}
	raw, e := os.ReadFile(filepath.Join(task.OutputPath, "normalized/result.json"))
	if e != nil {
		t.Fatal(e)
	}
	disk, e := contracts.DecodeScanResult(raw, &result.Execution)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(disk, *result) {
		t.Fatal("Node result changed through Go Agent and Server")
	}
	if result.Execution.TaskID != contracts.TaskID(fmt.Sprint(task.ID)) || result.Execution.Attempt != int64(task.Attempt) {
		t.Fatal("foreign execution accepted")
	}
	if scenario == "empty" {
		if len(result.Findings) != 0 || result.Cost.Value != nil {
			t.Fatal("unknown cost or empty findings changed")
		}
		return
	}
	if scenario == "failed" {
		progress, e := contracts.DecodeProgressSnapshotV2([]byte(task.ProgressJSON), &result.Execution)
		if e != nil || result.Cost.Value != nil || progress.Cost == nil || *progress.Cost.EstimatedUSD != 1.125 || *progress.Cost.InputTokens != 0 || len(result.Extensions.Value["lastObservedCost"]) == 0 {
			t.Fatal("partial cost lost or promoted to final cost", e)
		}
	} else if result.Cost.Value == nil || *result.Cost.Value.EstimatedUSD != 1.125 || *result.Cost.Value.InputTokens != 0 {
		t.Fatal("observed cost or zero tokens were lost")
	}
	if scenario == "partial-empty" {
		if task.Status != "partial" || len(result.Findings) != 0 {
			t.Fatal("partial empty result reclassified")
		}
		return
	}
	if scenario == "failed" {
		if task.Status != "failed" || task.ErrorCode != "authentication_error" || len(result.Findings) != 0 {
			t.Fatal("structured SDK failure changed")
		}
		return
	}
	if len(result.Findings) != 1 {
		t.Fatal("rich finding was lost")
	}
	f := result.Findings[0]
	if !f.CodeEvidence.Present || f.CodeEvidence.Value[0].Explanation.Value != "完整证据解释：用于合同验收" || f.Confidence.Value == nil || f.Confidence.Value.Rationale == nil || *f.Confidence.Value.Rationale != "合成置信理由；未执行独立验证" || f.Confidence.Value.Value.Present {
		t.Fatal("evidence or original confidence was lost or invented")
	}
	if f.EngineFindingID.Value != "fixture-finding-1" || f.EngineOccurrenceID.Value != "fixture-occurrence-1" || !f.Provenance.Present || !f.Extensions.Present {
		t.Fatal("native identity, provenance or extensions lost")
	}
	var native, stored any
	nativeBytes, e := os.ReadFile(filepath.Join(task.OutputPath, "raw/codex-security/sdk-result.json"))
	if e != nil {
		t.Fatal(e)
	}
	if json.Unmarshal(nativeBytes, &native) != nil || json.Unmarshal(result.Extensions.Value["sdk"], &stored) != nil || !reflect.DeepEqual(native, stored) {
		t.Fatal("full SDK snapshot was not retained")
	}
	if scenario == "late-exit" {
		if task.Status != "completed" || archive.Completion.Process.Code == nil || *archive.Completion.Process.Code != 10 || !archive.Completion.Error.Present {
			t.Fatal("late process exit was silently ignored or dropped committed result")
		}
	}
	findings, e := s.ListFindings(t.Context(), task.ID)
	if e != nil || len(findings) != 1 || len(findings[0].Normalized) == 0 {
		t.Fatal("Web evidence projection missing", e)
	}
	// Exercise the authenticated API, not just the persistence method.
	req, _ := http.NewRequest("GET", fmt.Sprintf("%s/api/v1/scans/%d/attempts/%d/result", url, task.ID, task.Attempt), nil)
	req.Header.Set("Authorization", "Bearer "+token)
	resp, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	data, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	var envelope struct {
		Data store.AttemptResult `json:"data"`
	}
	if resp.StatusCode != 200 || json.Unmarshal(data, &envelope) != nil || envelope.Data.Result == nil || !reflect.DeepEqual(*envelope.Data.Result, *result) {
		t.Fatal("Web API lost full result", string(data))
	}
	for _, ref := range result.Artifacts {
		if ref.Kind != "report" {
			continue
		}
		req, _ = http.NewRequest("GET", fmt.Sprintf("%s/api/v1/scans/%d/attempts/%d/artifacts?path=%s", url, task.ID, task.Attempt, ref.RelativePath), nil)
		req.Header.Set("Authorization", "Bearer "+token)
		resp, e = http.DefaultClient.Do(req)
		if e != nil {
			t.Fatal(e)
		}
		data, _ = io.ReadAll(resp.Body)
		resp.Body.Close()
		if resp.StatusCode != 200 || string(data) != "Synthetic SDK report. No model called.\n" {
			t.Fatal("verified artifact download failed", string(data))
		}
		if scenario == "rich" {
			if e = os.WriteFile(filepath.Join(task.OutputPath, string(ref.RelativePath)), []byte(strings.Repeat("x", len(data))), 0600); e != nil {
				t.Fatal(e)
			}
			resp, e = http.DefaultClient.Do(req)
			if e != nil {
				t.Fatal(e)
			}
			resp.Body.Close()
			if resp.StatusCode != 409 {
				t.Fatal("altered artifact was served")
			}
		}
	}
}
