package contracts

import (
	"os"
	"testing"
)

func TestNativeExtensionCannotInventUnarchivedArtifactOrExecution(t *testing.T) {
	raw, e := os.ReadFile("../fixtures/v1/result-rich.json")
	if e != nil {
		t.Fatal(e)
	}
	result, e := DecodeScanResult(raw, nil)
	if e != nil {
		t.Fatal(e)
	}
	raw, e = os.ReadFile("../fixtures/v1/sdk-native-result-rich.json")
	if e != nil {
		t.Fatal(e)
	}
	native, e := DecodeSDKNativeResult(raw, nil)
	if e != nil {
		t.Fatal(e)
	}
	native.Execution = result.Execution
	native.UsageEstimate.Execution = result.Execution
	native.Diagnostics.Execution = result.Execution
	for i := range native.Diagnostics.Entries {
		native.Diagnostics.Entries[i].Execution = result.Execution
	}
	result.Artifacts = append(result.Artifacts, *native.Reports.Markdown.Artifact)
	if !result.Extensions.Present {
		result.Extensions = Some(JSONObject{})
	}
	save := func() {
		raw, e := EncodeJSON(native)
		if e != nil {
			t.Fatal(e)
		}
		result.Extensions.Value["sdkNative"] = raw
	}
	save()
	if _, e = NativeSDKResult(result); e != nil {
		t.Fatal(e)
	}
	native.Reports.Markdown.Artifact.SHA256 = "bbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbbb"
	save()
	if _, e = NativeSDKResult(result); e == nil {
		t.Fatal("unarchived native reference accepted")
	}
	delete(result.Extensions.Value, "sdkNative")
	if v, e := NativeSDKResult(result); e != nil || v != nil {
		t.Fatal("old result changed", e)
	}
}
