package httpserver

import (
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"axiom/contracts/control"
	"encoding/json"
	"os"
	"reflect"
	"strings"
	"testing"
)

func sameJSON(t *testing.T, before, after any) {
	t.Helper()
	a, err := contracts.EncodeJSON(before)
	if err != nil {
		t.Fatal(err)
	}
	b, err := contracts.EncodeJSON(after)
	if err != nil {
		t.Fatal(err)
	}
	var x, y any
	decode := func(raw []byte, v *any) {
		d := json.NewDecoder(strings.NewReader(string(raw)))
		d.UseNumber()
		if err := d.Decode(v); err != nil {
			t.Fatal(err)
		}
	}
	decode(a, &x)
	decode(b, &y)
	if !reflect.DeepEqual(x, y) {
		t.Fatalf("legacy HTTP representation changed\nbefore %s\nafter  %s", a, b)
	}
}

func TestLegacyProjectionPreservesWireAndHidesDatabaseSecrets(t *testing.T) {
	// The golden document freezes the pre-migration wire shape. Future database
	// columns need not be added to legacy transport DTOs.
	raw, err := os.ReadFile("testdata/legacy-task.json")
	if err != nil {
		t.Fatal(err)
	}
	var expected any
	d := json.NewDecoder(strings.NewReader(string(raw)))
	d.UseNumber()
	if err = d.Decode(&expected); err != nil {
		t.Fatal(err)
	}
	v := &store.Task{}
	if err = json.Unmarshal(raw, v); err != nil {
		t.Fatal(err)
	}
	v.CredentialSnapshotEncrypted = "secret-must-stay-in-server"
	sameJSON(t, expected, legacyTask(v))
	encoded, _ := contracts.EncodeJSON(legacyTask(v))
	if strings.Contains(string(encoded), "secret-must-stay-in-server") {
		t.Fatal("database credential leaked")
	}
	sameJSON(t, struct {
		Task         *store.Task `json:"task"`
		LeaseSeconds int         `json:"leaseSeconds"`
	}{nil, 0},
		struct {
			Task         *control.LegacyTask `json:"task"`
			LeaseSeconds int                 `json:"leaseSeconds"`
		}{legacyTask(nil), 0})
}

func TestLegacyCompletionProjectionPreservesAllEvidence(t *testing.T) {
	line, confidence := 17, 0.6
	f := control.LegacyFinding{Normalized: json.RawMessage(`{"future":true}`), ID: 4, ScanID: 9007199254740993,
		Fingerprint: "fixture-fingerprint", Title: "Fixture only", Severity: "high", Category: "injection", CWE: []string{"CWE-89"},
		FilePath: "源码/a,b.ts", StartLine: &line, EndLine: &line, Description: "fixture", Evidence: "<code>&证据",
		Remediation: "fixture", Confidence: &confidence, Status: "open", Raw: json.RawMessage(`{"nativeId":"9007199254740993","extra":{"future":1}}`)}
	for _, findings := range [][]control.LegacyFinding{nil, {}, {f}} {
		v := control.LegacyCompletion{LegacyLeaseRef: control.LegacyLeaseRef{TaskID: 9007199254740993, Attempt: 2, AgentID: "fixture-agent"},
			Status: "completed", Coverage: json.RawMessage(`{"future":{"files":0}}`), ProgressJSON: `{"phase":"completed"}`, Findings: findings}
		sameJSON(t, v, legacyCompletion(v))
	}
}
