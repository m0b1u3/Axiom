package httpserver

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLogBatchIncremental(t *testing.T) {
	path := filepath.Join(t.TempDir(), "events.jsonl")
	record := "{\"message\":\"ok\"}\n"
	if err := os.WriteFile(path, []byte(strings.Repeat(record, 1600)), 0600); err != nil {
		t.Fatal(err)
	}
	b, err := readLogBatch(path, 0)
	if err != nil || len(b.Events) != 500 || b.Events[0].N != 101 || b.NextAfter != 600 || !b.HasMore {
		t.Fatalf("initial %v %+v", err, b)
	}
	b, err = readLogBatch(path, 1600)
	if err != nil || len(b.Events) != 0 {
		t.Fatal(err, b)
	}
	f, _ := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString("{\"message\":")
	f.Close()
	b, err = readLogBatch(path, 1600)
	if err != nil || b.Total != 1600 {
		t.Fatal(err, b)
	}
	f, _ = os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0600)
	f.WriteString("\"tail\"}\ninvalid\n")
	f.Close()
	b, err = readLogBatch(path, 1600)
	if err != nil || len(b.Events) != 1 || b.Events[0].Message != "tail" || b.NextAfter != 1602 {
		t.Fatal(err, b)
	}
	os.WriteFile(path, []byte(record), 0600)
	b, err = readLogBatch(path, 1602)
	if err != nil || b.Total != 1 || len(b.Events) != 1 {
		t.Fatal(err, b)
	}
}
