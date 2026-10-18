package contracts

import (
	"encoding/json"
	"fmt"
)

// Optional enrichment never changes whether the core historical result is readable.
func NativeSDKResult(result ScanResult) (*SDKNativeResult, error) {
	raw, exists := result.Extensions.Value["sdkNative"]
	if !exists {
		return nil, nil
	}
	value, err := DecodeSDKNativeResult(raw, &result.Execution)
	if err != nil {
		return nil, err
	}
	for _, ref := range []*ArtifactRef{value.Reports.Markdown.Artifact, value.Reports.Sarif.Artifact, value.DiagnosticJournal} {
		if ref == nil {
			continue
		}
		found := false
		for _, archived := range result.Artifacts {
			a, _ := json.Marshal(archived)
			b, _ := json.Marshal(ref)
			if string(a) == string(b) {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("native SDK artifact is not in the archived result")
		}
	}
	return &value, nil
}
