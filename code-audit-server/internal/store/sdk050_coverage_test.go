package store

import (
	"axiom/contracts"
	"encoding/json"
	"testing"
)

func TestCoverageDocumentPreservesAbsentConclusionAndLegacyFallback(t *testing.T) {
	for _, fixture := range []struct {
		name       string
		conclusion contracts.CoverageCompleteness
		details    string
		want       string
	}{
		{"sdk050", "unknown", `{"documentType":"codex-security.coverage","resolvedDeferred":["Fixtures / Mock"],"future":{"retained":true}}`, `{"documentType":"codex-security.coverage","resolvedDeferred":["Fixtures / Mock"],"future":{"retained":true}}`},
		{"old-complete", "complete", `{"surfaces":[]}`, `{"surfaces":[],"completeness":"complete"}`},
		{"old-partial", "partial", `{}`, `{"completeness":"partial"}`},
		{"legacy-conclusion", "partial", `{"completeness":"complete","surfaces":[]}`, `{"completeness":"partial","surfaces":[]}`},
		{"missing", "unknown", `{}`, `{"completeness":"unknown"}`},
		{"unknown-native-field", "unknown", `{"completeness":"future-native-value"}`, `{"completeness":"future-native-value"}`},
	} {
		t.Run(fixture.name, func(t *testing.T) {
			var details contracts.JSONObject
			json.Unmarshal([]byte(fixture.details), &details)
			raw := coverageDocument(contracts.Coverage{Completeness: fixture.conclusion, Details: details})
			var got, want any
			json.Unmarshal(raw, &got)
			json.Unmarshal([]byte(fixture.want), &want)
			a, _ := json.Marshal(got)
			b, _ := json.Marshal(want)
			if string(a) != string(b) {
				t.Fatal(string(a), string(b))
			}
			// Projection must not rewrite the original native details held in the archive.
			var before any
			json.Unmarshal([]byte(fixture.details), &before)
			a, _ = json.Marshal(details)
			b, _ = json.Marshal(before)
			if string(a) != string(b) {
				t.Fatal("native details changed")
			}
		})
	}
}
