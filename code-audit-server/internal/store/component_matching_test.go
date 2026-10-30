package store

import (
	"axiom/contracts"
	"encoding/json"
	"os"
	"testing"
)

func TestSavedMatchingEstimatePreservesLegacyUnknown(t *testing.T) {
	ref := contracts.ExecutionRef{TaskID: "42", Attempt: 1}
	for _, documents := range [][2]string{
		{`{"result":{"extensions":{"executionUsage":{"inputTokens":100}}}}`, ""},
		{"", `{"extensions":{"sdkUsage":{"inputTokens":100},"componentScan":{"matching":{"costState":"unknown"}}}}`},
		{`{"result":{"extensions":{"componentScan":{"snapshot":{"matching":{"costState":"unknown"}}}}}}`, ""},
	} {
		value, e := savedMatchingEstimate(ref, documents[0], documents[1])
		if e != nil || value != nil {
			t.Fatal("legacy usage must not synthesize a matching estimate", value, e)
		}
	}
	data, e := os.ReadFile("../../../contracts/fixtures/v1/token-cost-estimated.json")
	if e != nil {
		t.Fatal(e)
	}
	var estimate contracts.TokenCostEstimate
	if e = json.Unmarshal(data, &estimate); e != nil {
		t.Fatal(e)
	}
	wrap := func(v contracts.TokenCostEstimate) string {
		data, _ := json.Marshal(map[string]any{"result": map[string]any{"extensions": map[string]any{"componentMatchingEstimate": v}}})
		return string(data)
	}
	value, e := savedMatchingEstimate(ref, wrap(estimate), "")
	if e != nil || value == nil || value.AmountMicros == nil {
		t.Fatal(value, e)
	}
	estimate.Execution.Attempt++
	if _, e = savedMatchingEstimate(ref, wrap(estimate), ""); e == nil {
		t.Fatal("foreign matching execution accepted")
	}
}
