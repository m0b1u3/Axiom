package contracts

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

type fixture struct {
	ID       string        "json:\"id\""
	Kind     Kind          "json:\"kind\""
	File     string        "json:\"file\""
	Valid    bool          "json:\"valid\""
	Expected *ExecutionRef "json:\"expectedExecution\""
}

func fixtures(t *testing.T) []fixture {
	t.Helper()
	data, err := os.ReadFile("../fixtures/v1/manifest.json")
	if err != nil {
		t.Fatal(err)
	}
	var manifest struct {
		Cases []fixture "json:\"cases\""
	}
	if err := json.Unmarshal(data, &manifest); err != nil {
		t.Fatal(err)
	}
	return manifest.Cases
}
func fixtureData(t *testing.T, name string) []byte {
	t.Helper()
	data, err := os.ReadFile(filepath.Join("../fixtures/v1", name))
	if err != nil {
		t.Fatal(err)
	}
	return data
}

func TestSharedFixtures(t *testing.T) {
	for _, f := range fixtures(t) {
		t.Run(f.ID, func(t *testing.T) {
			data := fixtureData(t, f.File)
			result, err := RoundTrip(f.Kind, data, f.Expected)
			if (err == nil) != f.Valid {
				t.Fatalf("expected valid=%v, got %v", f.Valid, err)
			}
			if f.Valid {
				var before, after any
				json.Unmarshal(data, &before)
				json.Unmarshal(result, &after)
				b, _ := json.Marshal(before)
				a, _ := json.Marshal(after)
				if string(a) != string(b) {
					t.Fatalf("typed codec lost or changed fields in %s", f.ID)
				}
			}
		})
	}
}

func TestOptionalPresenceAndZero(t *testing.T) {
	present, err := DecodeScanResult(fixtureData(t, "result-cost-null.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if !present.Cost.Present || present.Cost.Value != nil {
		t.Fatal("explicit null was not preserved")
	}
	absent, err := DecodeScanResult(fixtureData(t, "result-empty-cost-absent.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	if absent.Cost.Present {
		t.Fatal("absent cost became present")
	}
	rich, err := DecodeScanResult(fixtureData(t, "result-rich.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	f := rich.Findings[0]
	if !f.SeverityScore.Present || f.SeverityScore.Value != 0 {
		t.Fatal("explicit zero lost")
	}
	if !f.Provenance.Present || string(f.Provenance.Value) != "null" {
		t.Fatal("opaque null lost")
	}
	if !f.EngineFindingID.Present || !f.CodeEvidence.Present {
		t.Fatal("normalized evidence missing")
	}
}

func TestTypedEventPayload(t *testing.T) {
	event, err := DecodeScanEvent(fixtureData(t, "event-phase_changed.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	payload, err := event.AsPhaseChangedData()
	if err != nil {
		t.Fatal(err)
	}
	if payload.Phase != ScanPhaseValidation {
		t.Fatal("wrong typed payload")
	}
	if _, err = event.AsScanCost(); err == nil {
		t.Fatal("wrong event type accepted")
	}
	worker, err := DecodeScanEvent(fixtureData(t, "event-worker.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	worker.Data = []byte("{\"worker\":{\"index\":2.0},\"status\":\"dispatch\"}")
	data, err := worker.AsWorkerState()
	if err != nil || data.Worker.Index.Value != 2 {
		t.Fatal("payload integer normalization", err)
	}
}

func TestCompatibilityAndByteLimits(t *testing.T) {
	for before, after := range map[string]ErrorCode{"auth_failed": ErrorCodeAuthenticationError, "rate_limited": ErrorCodeRateLimit, "result_corrupt": ErrorCodeInvalidResult} {
		got, ok := NormalizeLegacyErrorCode(before)
		if !ok || got != after {
			t.Fatalf("alias %s", before)
		}
	}
	if got, ok := NormalizeLegacyErrorCode("message says HTTP 401"); ok || got != ErrorCodeUnknown {
		t.Fatal("message classification leaked into contract")
	}
	if code, ok := ExitCode(TerminalStatusPartial); !ok || code != 2 {
		t.Fatal("partial exit code")
	}
	err := ValidateJSON(KindProgressSnapshot, make([]byte, MaxProgressBytes+1), nil)
	var e *ValidationError
	if !errors.As(err, &e) || e.Code != "payload_too_large" {
		t.Fatal(err)
	}
	err = ValidateJSON(KindExecutionRef, []byte{0xff}, nil)
	if !errors.As(err, &e) || e.Code != "invalid_json" {
		t.Fatal(err)
	}
	if err := ValidateJSON("Unknown", []byte("{}"), nil); !errors.As(err, &e) || e.Code != "unsupported_kind" {
		t.Fatal(err)
	}
}
