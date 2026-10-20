package scanner

import (
	"encoding/json"
	"os"
	"testing"
)

func TestSDK050DescriptionIsPinnedAndUnknownVersionsFailBeforeExecution(t *testing.T) {
	raw, err := os.ReadFile("../../../contracts/fixtures/v1/scanner-description-v13.json")
	if err != nil {
		t.Fatal(err)
	}
	c, err := parseDescription(raw)
	if err != nil {
		t.Fatal(err)
	}
	if c.DescriptionVersion != 13 || c.ConcurrencyVersion != 1 || c.ParametersVersion != 1 || c.IdentityVersion != 1 {
		t.Fatal(c)
	}
	for _, kind := range []string{"sdk", "adapter", "description"} {
		var value map[string]any
		json.Unmarshal(raw, &value)
		switch kind {
		case "sdk":
			value["configuredSdk"].(map[string]any)["version"] = "0.1.25"
		case "adapter":
			value["sdkResultAdapterVersion"] = 1
		case "description":
			value["schemaVersion"] = 14
		}
		changed, _ := json.Marshal(value)
		if _, err := parseDescription(changed); err == nil {
			t.Fatal("unsupported description admitted", kind)
		}
	}
}
