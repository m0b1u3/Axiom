package scanner

import (
	"encoding/json"
	"testing"
)

func TestDeepDeadlineAndProgress(t *testing.T) {
	if HardTimeoutMinutes("deep", 3) != 13 || HardTimeoutMinutes("standard", 3) != 3 {
		t.Fatal("native deadline must leave a bounded finalization window")
	}
	var p Progress
	if e := json.Unmarshal([]byte(`{"taskId":1,"phase":"discovery","deep":{"completed":2,"active":4,"maximum":100},"worker":{"kind":"dispatch"},"reconnect":{"attempt":1}}`), &p); e != nil {
		t.Fatal(e)
	}
	b, _ := json.Marshal(p)
	var doc map[string]any
	json.Unmarshal(b, &doc)
	for _, k := range []string{"deep", "worker", "reconnect"} {
		if doc[k] == nil {
			t.Fatal(k, "lost in agent heartbeat")
		}
	}
}
