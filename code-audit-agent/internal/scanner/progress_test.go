package scanner

import (
	"axiom/contracts"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestProgressReaderChecksIdentitySchemaAndBoundedBytes(t *testing.T) {
	raw, e := os.ReadFile("../../../contracts/fixtures/v1/observation-null.json")
	if e != nil {
		t.Fatal(e)
	}
	p, e := contracts.DecodeProgressSnapshotV2(raw, nil)
	if e != nil {
		t.Fatal(e)
	}
	dir := t.TempDir()
	os.MkdirAll(filepath.Join(dir, "state"), 0700)
	file := filepath.Join(dir, LayoutProgress)
	os.WriteFile(file, raw, 0600)
	if _, e = ReadContractProgress(dir, p.Execution); e != nil {
		t.Fatal(e)
	}
	wrong := p.Execution
	wrong.Attempt++
	if _, e = ReadContractProgress(dir, wrong); e == nil {
		t.Fatal("other execution accepted")
	}
	os.WriteFile(file, []byte(`{"taskId":42,"phase":"discovery"}`), 0600)
	if _, e = ReadContractProgress(dir, p.Execution); e == nil {
		t.Fatal("legacy schema accepted")
	}
	os.WriteFile(file, []byte(strings.Repeat(" ", contracts.MaxProgressBytes+1)), 0600)
	if _, e = ReadContractProgress(dir, p.Execution); e == nil {
		t.Fatal("oversized progress accepted")
	}
}
