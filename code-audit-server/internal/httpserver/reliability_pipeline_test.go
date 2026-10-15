package httpserver_test

import (
	"axiom/code-audit-server/internal/store"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
)

// These tests run the production Go Agent and Node Scanner against PostgreSQL.
// Only SDK model calls are replaced with the explicitly marked fixture.
func runDeliveryReliabilityAcceptance(t *testing.T, s *store.Store, api http.Handler, agent, root, url, token, agentToken, scratch string, snapshotID int64, encrypted string) {
	t.Helper()
	for _, mode := range []string{"completion-unavailable", "completion-response-lost", "large-stdout"} {
		t.Run("reliability-"+mode, func(t *testing.T) {
			t.Setenv("AXIOM_RESULT_TEST_SCENARIO", "rich")
			if mode == "large-stdout" {
				t.Setenv("AXIOM_RESULT_TEST_SCENARIO", mode)
			}
			id := createReliabilityTask(t, s, url, token, snapshotID, encrypted, mode)
			var completions atomic.Int32
			proxy := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if strings.HasSuffix(r.URL.Path, "/complete") {
					n := completions.Add(1)
					if mode == "completion-unavailable" && n <= 2 {
						http.Error(w, "Fixture only: temporary control plane failure", http.StatusServiceUnavailable)
						return
					}
					if mode == "completion-response-lost" && n == 1 {
						recorder := httptest.NewRecorder()
						api.ServeHTTP(recorder, r)
						if recorder.Code != http.StatusOK {
							t.Errorf("first completion failed before fault injection: %d", recorder.Code)
						}
						connection, _, e := w.(http.Hijacker).Hijack()
						if e != nil {
							t.Error(e)
							return
						}
						connection.Close() // DB committed; the Agent cannot observe the ACK.
						return
					}
				}
				api.ServeHTTP(w, r)
			}))
			defer proxy.Close()
			capture := filepath.Join(scratch, mode+".json")
			runInputAgent(t, agent, root, proxy.URL, agentToken, capture)
			task, e := s.Get(t.Context(), id)
			if e != nil || task.Status != "completed" || task.Attempt != 1 || task.FindingCount != 1 {
				t.Fatalf("retry changed archived result: %+v %v", task, e)
			}
			if mode != "large-stdout" && completions.Load() < 2 {
				t.Fatal("ambiguous delivery was not acknowledged by an idempotent completion retry")
			}
			assertSDKCallCount(t, capture, 1)
			attempts, e := s.ListAttempts(t.Context(), id)
			if e != nil || len(attempts) != 1 || attempts[0].Status != "completed" {
				t.Fatal("completion retry created another attempt", attempts, e)
			}
		})
	}
}

func createReliabilityTask(t *testing.T, s *store.Store, url, token string, snapshotID int64, encrypted, name string) int64 {
	t.Helper()
	cfg, e := s.CreateModelConfig(t.Context(), store.ModelConfigRow{Name: "Fixture reliability " + name, Provider: "openai", Model: "fixture-reliability-model", ApiKeyEncrypted: encrypted})
	if e != nil {
		t.Fatal(e)
	}
	status, body := doJSON(t, url+"/api/v1/scans", token, map[string]any{"sourceSnapshotId": snapshotID, "modelConfigId": cfg.ID})
	if status != http.StatusOK {
		t.Fatal(status, body)
	}
	var created struct {
		Data store.Task `json:"data"`
	}
	if e = json.Unmarshal([]byte(body), &created); e != nil {
		t.Fatal(e)
	}
	return created.Data.ID
}

func assertSDKCallCount(t *testing.T, capture string, want int) {
	t.Helper()
	data, e := os.ReadFile(capture + ".calls")
	if e != nil || len(strings.Fields(string(data))) != want {
		t.Fatalf("SDK calls=%q want=%d err=%v", data, want, e)
	}
}
