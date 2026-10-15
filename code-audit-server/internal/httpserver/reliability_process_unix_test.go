//go:build !windows

package httpserver_test

import (
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"context"
	"database/sql"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"syscall"
	"testing"
	"time"
)

func runProcessReliabilityAcceptance(t *testing.T, s *store.Store, server *httpserver.Server, agent, root, url, token, agentToken, scratch string, snapshotID int64, encrypted string) {
	t.Helper()
	for _, mode := range []string{"scanner-killed", "agent-graceful-stop", "agent-killed", "cancel-dead-agent", "lease-outage"} {
		t.Run("reliability-"+mode, func(t *testing.T) {
			originalLease := server.QueueLimits.Lease
			server.QueueLimits.Lease = 12 * time.Second
			defer func() { server.QueueLimits.Lease = originalLease }()
			t.Setenv("AXIOM_RESULT_TEST_SCENARIO", "reliability-hold")
			id := createReliabilityTask(t, s, url, token, snapshotID, encrypted, mode)
			db, e := sql.Open("pgx", os.Getenv("AXIOM_TEST_DATABASE_URL"))
			if e != nil {
				t.Fatal(e)
			}
			defer db.Close()
			defer func() {
				ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
				defer cancel()
				_, _ = s.RequestCancel(ctx, id)
				_, _ = db.ExecContext(ctx, "UPDATE scan_tasks SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=$1 AND status='cancel_requested'", id)
			}()
			api := server.Routes()
			var outage atomic.Bool
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if outage.Load() && strings.HasSuffix(r.URL.Path, "/heartbeat") {
					http.Error(w, "Fixture only: disconnected Agent", http.StatusServiceUnavailable)
					return
				}
				api.ServeHTTP(w, r)
			}))
			defer proxy.Close()
			capture := filepath.Join(scratch, mode+".json")
			ctx, cancel := context.WithTimeout(t.Context(), 25*time.Second)
			defer cancel()
			cmd := inputAgentCommand(ctx, agent, root, proxy.URL, agentToken, capture)
			if e := cmd.Start(); e != nil {
				t.Fatal(e)
			}
			finished := make(chan error, 1)
			go func() { finished <- cmd.Wait() }()
			t.Cleanup(func() { cancel(); _ = cmd.Process.Kill() })
			var p struct {
				ScannerPID int `json:"scannerPid"`
				ChildPID   int `json:"childPid"`
			}
			waitReliability(t, 8*time.Second, func() bool {
				data, e := os.ReadFile(capture + ".processes")
				return e == nil && json.Unmarshal(data, &p) == nil && p.ScannerPID > 1 && p.ChildPID > 1
			}, "Scanner did not enter the SDK fixture")
			// Even a failing test must not leave a live fixture process behind.
			t.Cleanup(func() {
				_ = syscall.Kill(-p.ScannerPID, syscall.SIGKILL)
				_ = syscall.Kill(p.ChildPID, syscall.SIGKILL)
			})
			first, e := s.Get(t.Context(), id)
			if e != nil || first.Attempt != 1 {
				t.Fatal(first, e)
			}
			switch mode {
			case "scanner-killed":
				e = syscall.Kill(-p.ScannerPID, syscall.SIGKILL)
			case "agent-graceful-stop":
				e = cmd.Process.Signal(syscall.SIGTERM)
			case "agent-killed", "cancel-dead-agent":
				e = cmd.Process.Kill()
			case "lease-outage":
				outage.Store(true)
			}
			if e != nil {
				t.Fatal(e)
			}
			select {
			case <-finished:
			case <-time.After(15 * time.Second):
				t.Fatal("Agent did not finish after the execution fault")
			}
			waitReliability(t, 4*time.Second, func() bool {
				return syscall.Kill(p.ScannerPID, 0) != nil && syscall.Kill(p.ChildPID, 0) != nil
			}, "Agent interruption left Scanner or its SDK subprocess running")
			assertSDKCallCount(t, capture, 1)
			if mode == "scanner-killed" {
				last, e := s.Get(t.Context(), id)
				if e != nil || last.Status != "failed" || last.ErrorCode != "invalid_result" || last.Attempt != 1 {
					t.Fatal(last, e)
				}
				return
			}
			if mode == "cancel-dead-agent" {
				code, body := doJSON(t, url+"/api/v1/scans/"+jsonNumber(id)+"/cancel", token, map[string]any{})
				if code != http.StatusOK {
					t.Fatal(code, body)
				}
			}
			waitReliability(t, 15*time.Second, func() bool {
				var lease string
				if e := db.QueryRowContext(t.Context(), "SELECT lease_expires_at FROM scan_tasks WHERE id=$1", id).Scan(&lease); e != nil {
					return false
				}
				until, e := time.Parse(time.RFC3339, lease)
				return e == nil && time.Now().After(until.Add(time.Second))
			}, "execution lease did not expire")
			t.Setenv("AXIOM_RESULT_TEST_SCENARIO", "rich")
			recoveredCapture := capture + ".recovered"
			runInputAgent(t, agent, root, url, agentToken, recoveredCapture)
			last, e := s.Get(t.Context(), id)
			if e != nil {
				t.Fatal(e)
			}
			if mode == "cancel-dead-agent" {
				if last.Status != "canceled" || last.Attempt != 1 {
					t.Fatal("dead canceled task was restarted", last)
				}
				if _, e := os.Stat(recoveredCapture + ".calls"); !os.IsNotExist(e) {
					t.Fatal("cancel recovery called SDK", e)
				}
				return
			}
			if last.Status != "completed" || last.Attempt != 2 {
				t.Fatal("lease recovery failed", last)
			}
			assertSDKCallCount(t, recoveredCapture, 1)
			attempts, e := s.ListAttempts(t.Context(), id)
			if e != nil || len(attempts) != 2 || attempts[1].Status != "abandoned" {
				t.Fatal("old attempt lost its history", attempts, e)
			}
			if _, e = s.HeartbeatExecution(t.Context(), store.LeaseRef{TaskID: id, Attempt: 1, AgentID: first.AgentID}, time.Minute, "", ""); e == nil {
				t.Fatal("old attempt renewed a recovered lease")
			}
		})
	}
}

func jsonNumber(id int64) string { b, _ := json.Marshal(id); return string(b) }

func waitReliability(t *testing.T, limit time.Duration, check func() bool, message string) {
	t.Helper()
	deadline := time.Now().Add(limit)
	for time.Now().Before(deadline) {
		if check() {
			return
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatal(message)
}
