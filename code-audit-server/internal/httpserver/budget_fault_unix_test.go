//go:build !windows

package httpserver_test

import (
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"context"
	"database/sql"
	"encoding/json"
	"os"
	"path/filepath"
	"syscall"
	"testing"
	"time"
)

func runBudgetProcessFault(t *testing.T, s *store.Store, server *httpserver.Server, agent, root, url, agentToken, scratch string, create func(float64) int64) {
	t.Helper()
	t.Setenv("AXIOM_RESULT_TEST_SCENARIO", "reliability-hold")
	id := create(5)
	capture := filepath.Join(scratch, "budget-crash")
	ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
	defer cancel()
	cmd := inputAgentCommand(ctx, agent, root, url, agentToken, capture)
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	defer cmd.Process.Kill()
	var p struct {
		ScannerPID int `json:"scannerPid"`
		ChildPID   int `json:"childPid"`
	}
	waitReliability(t, 8*time.Second, func() bool {
		raw, e := os.ReadFile(capture + ".processes")
		return e == nil && json.Unmarshal(raw, &p) == nil && p.ScannerPID > 1 && p.ChildPID > 1
	}, "budget fixture never started")
	defer syscall.Kill(-p.ScannerPID, syscall.SIGKILL)
	defer syscall.Kill(p.ChildPID, syscall.SIGKILL)
	if e := cmd.Process.Kill(); e != nil {
		t.Fatal(e)
	}
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("killed Agent did not exit")
	}
	waitReliability(t, 5*time.Second, func() bool { return syscall.Kill(p.ScannerPID, 0) != nil && syscall.Kill(p.ChildPID, 0) != nil }, "budget fault left fixture children running")
	db, e := sql.Open("pgx", os.Getenv("AXIOM_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.ExecContext(t.Context(), `UPDATE scan_tasks SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=$1`, id); e != nil {
		t.Fatal(e)
	}
	t.Setenv("AXIOM_RESULT_TEST_SCENARIO", "rich")
	again := capture + ".recovery"
	runInputAgent(t, agent, root, url, agentToken, again)
	if _, e = os.Stat(again + ".calls"); !os.IsNotExist(e) {
		t.Fatal("unknown-cost recovery started SDK", e)
	}
	b, e := s.BudgetSnapshot(t.Context(), id)
	if e != nil || b.State != "unknown" || len(b.UnknownAttempts) != 1 {
		t.Fatal(b, e)
	}
	if task, e := s.Get(t.Context(), id); e != nil || task.Attempt != 1 || task.Status != "failed" {
		t.Fatal(task, e)
	}
	assertSDKCallCount(t, capture, 1)
}
