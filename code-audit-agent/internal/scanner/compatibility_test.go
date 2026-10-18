package scanner

import (
	"encoding/json"
	"os"
	"testing"
)

func TestDescriptionProjectionUsesDeclaredVersions(t *testing.T) {
	for version := 1; version <= 8; version++ {
		file := "../../../contracts/fixtures/v1/scanner-description.json"
		if version > 1 {
			file = "../../../contracts/fixtures/v1/scanner-description-v" + string(rune('0'+version)) + ".json"
		}
		if version == 4 {
			file = "../../../contracts/fixtures/v1/scanner-description-v4.json"
		}
		raw, e := os.ReadFile(file)
		if e != nil {
			t.Fatal(e)
		}
		c, e := parseDescription(raw)
		if e != nil {
			t.Fatal(e)
		}
		if supportsScanInput(c) != (version >= 5) {
			t.Fatal("advanced input capability invented or lost", version, c)
		}
		if c.DescriptionVersion != version {
			t.Fatal(c)
		}
	}
	raw, e := os.ReadFile("../../../contracts/fixtures/v1/scanner-description-v4.json")
	if e != nil {
		t.Fatal(e)
	}
	var d map[string]any
	json.Unmarshal(raw, &d)
	runtime := d["runtime"].(map[string]any)
	versions := runtime["versions"].(map[string]any)
	versions["job"] = []any{float64(1), float64(2)}
	versions["progress"] = []any{float64(1)}
	versions["event"] = []any{float64(1)}
	changed, _ := json.Marshal(d)
	c, e := parseDescription(changed)
	if e == nil && c.Observation.SupportsObservation(1) {
		t.Fatal("description v4 invented observation support")
	}
	d["schemaVersion"] = 10
	changed, _ = json.Marshal(d)
	if _, e = parseDescription(changed); e == nil {
		t.Fatal("future description accepted")
	}
}
