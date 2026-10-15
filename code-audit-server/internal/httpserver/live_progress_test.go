package httpserver

import (
	"axiom/code-audit-server/internal/store"
	"os"
	"path/filepath"
	"testing"
)

func TestLiveProgressUsesOnlyAcceptedHeartbeat(t *testing.T) {
	raw, e := os.ReadFile("../../../contracts/fixtures/v1/observation-zero.json")
	if e != nil {
		t.Fatal(e)
	}
	task := store.Task{ID: 9007199254740993, Attempt: 2, ObservationContractVersion: 1, Status: "scanning", CurrentStage: "discovery", ProgressJSON: string(raw), OutputPath: t.TempDir()}
	os.MkdirAll(filepath.Join(task.OutputPath, "state"), 0700)
	os.WriteFile(filepath.Join(task.OutputPath, "state/sdk-progress.json"), []byte(`{"taskId":9007199254740993,"phase":"failed"}`), 0600)
	enrichLiveProgress(&task)
	if task.Progress == nil || task.Progress.Phase != "discovery" || task.Progress.Files.Completed == nil || *task.Progress.Files.Completed != 0 {
		t.Fatal(task.Progress)
	}
	task.Status = "completed"
	enrichLiveProgress(&task)
	if task.Status != "completed" || task.Progress.Phase != "discovery" {
		t.Fatal("terminal conclusion overwritten")
	}
	task.Attempt++
	enrichLiveProgress(&task)
	if task.Progress != nil || task.ProgressDiagnostic == nil {
		t.Fatal("old attempt snapshot was exposed")
	}
}
