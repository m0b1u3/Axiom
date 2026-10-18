package store

import (
	"axiom/contracts"
	"encoding/json"
	"fmt"
	"os"
	"testing"
)

func nativeFixture(t *testing.T, execution contracts.ExecutionRef) contracts.SDKNativeResult {
	t.Helper()
	raw, e := os.ReadFile("../../../contracts/fixtures/v1/sdk-native-result-rich.json")
	if e != nil {
		t.Fatal(e)
	}
	value, e := contracts.DecodeSDKNativeResult(raw, nil)
	if e != nil {
		t.Fatal(e)
	}
	value.Execution = execution
	value.UsageEstimate.Execution = execution
	value.Diagnostics.Execution = execution
	for i := range value.Diagnostics.Entries {
		value.Diagnostics.Entries[i].Execution = execution
	}
	return value
}
func TestPostgresSDKHistoryIndexIsAtomicPaginatedAndSourceBound(t *testing.T) {
	s := pgStore(t)
	ctx := t.Context()
	base := resultFixture(t)
	id, limits := resultTask(t, s, base)
	input, output := modernCaps()
	original, e := s.Get(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	var first contracts.ExecutionCompletion
	for i := 0; i < 53; i++ {
		if i > 0 {
			copy := *original
			copy.ScanNo = fmt.Sprintf("sdk-native-links-%d", i)
			if _, e = s.InsertPending(ctx, copy); e != nil {
				t.Fatal(e)
			}
		}
		task, e := s.ClaimExecutionWithContracts(ctx, "native-index-agent", limits, input, output)
		if e != nil || task == nil {
			t.Fatal(e)
		}
		done := completedFixture(base, task)
		native := nativeFixture(t, done.Execution)
		native.ScanID = new(string)
		*native.ScanID = "fixture-shared-native-id"
		value := done.Result.Value
		extensions := contracts.JSONObject{}
		for k, v := range value.Extensions.Value {
			extensions[k] = v
		}
		value.Extensions = contracts.Some(extensions)
		value.Artifacts = append(value.Artifacts, *native.Reports.Markdown.Artifact)
		raw, e := contracts.EncodeJSON(native)
		if e != nil {
			t.Fatal(e)
		}
		if !value.Extensions.Present {
			value.Extensions = contracts.Some(contracts.JSONObject{})
		}
		value.Extensions.Value["sdkNative"] = json.RawMessage(raw)
		done.Result = contracts.Some(value)
		if e = s.CompleteContractExecution(ctx, done); e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			first = done
		}
	}
	if e = s.CompleteContractExecution(ctx, first); e != nil {
		t.Fatal("duplicate completion must not duplicate index", e)
	}
	page, e := s.SDKExecutionLinks(ctx, "fixture-shared-native-id", original.SourceContentHash, 2)
	if e != nil || page.Total != 53 || len(page.Items) != 3 {
		t.Fatal("history pagination", e, page)
	}
	other, e := s.SDKExecutionLinks(ctx, "fixture-shared-native-id", "different-source", 1)
	if e != nil || other.Total != 0 || len(other.Items) != 0 {
		t.Fatal("native link crossed source identity", e, other)
	}
	// Corrupt/future optional metadata is never projected into a trusted index,
	// while the portable core archive remains readable.
	archive, e := s.AttemptResult(ctx, id, 1)
	if e != nil || archive.Result == nil {
		t.Fatal(e)
	}
	archive.Result.Extensions.Value["sdkNative"] = json.RawMessage(`{"schemaVersion":99}`)
	if _, e = contracts.NativeSDKResult(*archive.Result); e == nil {
		t.Fatal("future native metadata accepted")
	}
}
