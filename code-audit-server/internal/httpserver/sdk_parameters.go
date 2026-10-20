package httpserver

import (
	"axiom/contracts"
	"encoding/json"
)

func validReasoningEffort(value string) bool {
	if value == "" {
		return true
	}
	raw, _ := json.Marshal(contracts.SDKScanParameters{SchemaVersion: 1, ReasoningEffort: contracts.Some(contracts.ReasoningEffort(value))})
	return contracts.ValidateJSON(contracts.KindSDKScanParameters, raw, nil) == nil
}
