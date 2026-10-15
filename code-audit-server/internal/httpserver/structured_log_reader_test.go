package httpserver

import (
	"axiom/contracts"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

var logExecution = contracts.ExecutionRef{TaskID: "42", Attempt: 2}

func eventBytes(t *testing.T, sequence int64, execution contracts.ExecutionRef) []byte {
	t.Helper()
	data, _ := json.Marshal(contracts.WarningData{Code: "unknown", Origin: "scanner", Message: "fixture，非真实事件"})
	v := contracts.ScanEventV2{SchemaVersion: 2, Execution: execution, Sequence: sequence, OccurredAt: "2026-10-13T01:00:00Z", Type: "scan.warning", Level: "warn", Data: data, Message: "任意展示文字"}
	raw, e := contracts.EncodeJSON(v)
	if e != nil {
		t.Fatal(e)
	}
	return append(raw, '\n')
}
func TestStructuredLogCursorDeduplicatesAndWaitsForCompleteLines(t *testing.T) {
	file := filepath.Join(t.TempDir(), "events.jsonl")
	first := eventBytes(t, 1, logExecution)
	second := eventBytes(t, 2, logExecution)
	os.WriteFile(file, append(append(append([]byte{}, first...), first...), second[:len(second)-1]...), 0600)
	b, e := readStructuredLogBatch(file, 0, logExecution)
	if e != nil || len(b.Events) != 1 || b.NextAfter != 1 {
		t.Fatal(b, e)
	}
	b, e = readStructuredLogBatch(file, 1, logExecution)
	if e != nil || len(b.Events) != 0 || b.NextAfter != 1 {
		t.Fatal(b, e)
	}
	f, _ := os.OpenFile(file, os.O_APPEND|os.O_WRONLY, 0600)
	f.Write([]byte("\n"))
	f.Close()
	b, e = readStructuredLogBatch(file, 1, logExecution)
	if e != nil || len(b.Events) != 1 || b.NextAfter != 2 {
		t.Fatal(b, e)
	}
	replay := b.Events[0].Contract
	if replay.Execution != logExecution || replay.Sequence != 2 {
		t.Fatal(replay)
	}
	// Replacement/truncation clears the offset cache; a cursor beyond EOF resets.
	os.WriteFile(file, first, 0600)
	b, e = readStructuredLogBatch(file, 2, logExecution)
	if e != nil || len(b.Events) != 1 || b.NextAfter != 1 {
		t.Fatal(b, e)
	}
}
func TestStructuredLogRejectsMixedIdentityAndOversizedRecords(t *testing.T) {
	file := filepath.Join(t.TempDir(), "events.jsonl")
	old := logExecution
	old.Attempt--
	data := eventBytes(t, 1, old)
	data = append(data, []byte("{\n")...)
	data = append(data, []byte(strings.Repeat(" ", contracts.MaxEventBytes+1)+"\n")...)
	data = append(data, eventBytes(t, 4, logExecution)...)
	os.WriteFile(file, data, 0600)
	b, e := readStructuredLogBatch(file, 0, logExecution)
	if e != nil || len(b.Events) != 1 || b.NextAfter != 4 || b.InvalidRecords != 3 || b.Diagnostic == nil || b.RecordCount != 1 {
		t.Fatal(b, e)
	}
}
func TestStructuredLogIncrementalBatchesPreserveSequenceAcrossReconnect(t *testing.T) {
	file := filepath.Join(t.TempDir(), "events.jsonl")
	var data []byte
	for n := int64(1); n <= 501; n++ {
		data = append(data, eventBytes(t, n, logExecution)...)
	}
	os.WriteFile(file, data, 0600)
	b, e := readStructuredLogBatch(file, 0, logExecution)
	if e != nil || len(b.Events) != 500 || !b.HasMore || b.NextAfter != 500 {
		t.Fatal(b.NextAfter, e)
	}
	b, e = readStructuredLogBatch(file, b.NextAfter, logExecution)
	if e != nil || len(b.Events) != 1 || b.HasMore || b.NextAfter != 501 {
		t.Fatal(b, e)
	}
	b, e = readStructuredLogBatch(file, 500, logExecution)
	if e != nil || b.Events[0].Contract.Sequence != 501 {
		t.Fatal(b, e)
	}
}
