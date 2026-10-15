package httpserver_test

import (
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"testing"
	"time"
)

func assertProgressPipeline(t *testing.T, s *store.Store, url, token string, task *store.Task) {
	t.Helper()
	if task.ObservationContractVersion != 1 {
		t.Fatal("new task uses a legacy observation format")
	}
	expected := contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(task.ID)), Attempt: int64(task.Attempt)}
	raw, e := os.ReadFile(filepath.Join(task.OutputPath, "state/progress.json"))
	if e != nil {
		t.Fatal(e)
	}
	disk, e := contracts.DecodeProgressSnapshotV2(raw, &expected)
	if e != nil {
		t.Fatal(e)
	}
	archived, e := contracts.DecodeProgressSnapshotV2([]byte(task.ProgressJSON), &expected)
	if e != nil {
		t.Fatal(e)
	}
	if !reflect.DeepEqual(disk, archived) {
		t.Fatal("Scanner snapshot changed across Agent heartbeat and Server")
	}
	if disk.Phase != "partial" || disk.Cost == nil || disk.Cost.EstimatedUSD == nil || *disk.Cost.EstimatedUSD != 1.125 || *disk.Cost.InputTokens != 0 || disk.PhaseCounts != nil {
		t.Fatal("cost/phase cleanup mismatch", disk)
	}
	if len(disk.Workers) != 2 || disk.Workers[0].Status != "failed" || disk.Workers[1].Status != "waiting" || disk.Error == nil || disk.Error.Code != "rate_limit" {
		t.Fatal("structured worker state missing", disk.Workers)
	}
	status, body := getJSON(t, url+fmt.Sprintf("/api/v1/scans/%d", task.ID), token)
	if status != 200 {
		t.Fatal(status, string(body))
	}
	var public struct {
		Data store.Task `json:"data"`
	}
	if e = json.Unmarshal(body, &public); e != nil || public.Data.Progress == nil || !reflect.DeepEqual(disk, *public.Data.Progress) {
		t.Fatal("public snapshot differs", e)
	}
	// Replay the full cursor response after a simulated reconnect; event semantics
	// are generated DTOs all the way to the browser, with one sequence per attempt.
	for _, after := range []int{0, 0} {
		status, body = getJSON(t, url+fmt.Sprintf("/api/v1/scans/%d/logs?after=%d&attempt=%d", task.ID, after, task.Attempt), token)
		if status != 200 {
			t.Fatal(status, string(body))
		}
		var batch struct {
			Data struct {
				Format    string `json:"format"`
				NextAfter int    `json:"nextAfter"`
				Events    []struct {
					N     int                    `json:"n"`
					Event *contracts.ScanEventV2 `json:"event"`
				} `json:"events"`
			} `json:"data"`
		}
		if e = json.Unmarshal(body, &batch); e != nil {
			t.Fatal(e)
		}
		if batch.Data.Format != "scan_event_v2" || len(batch.Data.Events) == 0 {
			t.Fatal("legacy event response")
		}
		types := map[contracts.EventTypeV2]bool{}
		for n, v := range batch.Data.Events {
			raw, _ := contracts.EncodeJSON(v.Event)
			event, e := contracts.DecodeScanEventV2(raw, &expected)
			if e != nil || event.Sequence != int64(n+1) || v.N != n+1 {
				t.Fatal("event identity/sequence lost", e)
			}
			types[event.Type] = true
		}
		for _, kind := range []contracts.EventTypeV2{"scan.progress", "scan.worker", "scan.worker_status", "scan.activity", "scan.cost", "scan.error", "scan.reconnect", "scan.warning", "scan.finished"} {
			if !types[kind] {
				t.Fatal("missing event", kind)
			}
		}
		if batch.Data.NextAfter != len(batch.Data.Events) {
			t.Fatal("cursor mismatch")
		}
	}
	attempts, e := s.ListAttempts(t.Context(), task.ID)
	if e != nil || attempts[0].Progress == nil || !reflect.DeepEqual(disk, *attempts[0].Progress) {
		t.Fatal("historical snapshot missing", e)
	}
}

func runProgressWebAcceptance(t *testing.T, s *store.Store, agent, root, url, token, agentToken, capture string, id int64) {
	t.Helper()
	ctx, cancel := context.WithTimeout(t.Context(), 45*time.Second)
	defer cancel()
	cmd := inputAgentCommand(ctx, agent, root, url, agentToken, capture)
	var diagnostics bytes.Buffer
	cmd.Stdout = &diagnostics
	cmd.Stderr = &diagnostics
	if e := cmd.Start(); e != nil {
		t.Fatal(e)
	}
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	finished := false
	defer func() {
		if !finished {
			cmd.Process.Kill()
			<-done
		}
	}()
	// Wait for the genuine heartbeat, rather than installing synthetic API data.
	var live *store.Task
	deadline := time.Now().Add(15 * time.Second)
	for time.Now().Before(deadline) {
		live, _ = s.Get(t.Context(), id)
		if live != nil && live.ProgressJSON != "" {
			break
		}
		select {
		case err := <-done:
			finished = true
			t.Fatalf("fixture Agent ended before observation: %v %s", err, diagnostics.String())
		case <-time.After(100 * time.Millisecond):
		}
	}
	if live == nil || live.ProgressJSON == "" {
		t.Fatal("no live heartbeat")
	}
	p, e := contracts.DecodeProgressSnapshotV2([]byte(live.ProgressJSON), &contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: int64(live.Attempt)})
	if e != nil || p.Phase != "unknown" || !p.RawPhase.Present || p.RawPhase.Value != "fixture_native_phase" || p.Files.Completed == nil || *p.Files.Completed != 0 || p.PhaseCounts == nil || *p.PhaseCounts.Completed != 0 {
		t.Fatal("running observer/callback merge mismatch", p, e)
	}
	browser := exec.CommandContext(ctx, "node", filepath.Join(root, "code-audit-web/tests/progress-web-acceptance.mjs"))
	browser.Dir = filepath.Join(root, "code-audit-web")
	browser.Env = append(os.Environ(), "AXIOM_TEST_WEB_URL="+url, "AXIOM_TEST_PROGRESS_ID="+fmt.Sprint(id))
	if out, e := browser.CombinedOutput(); e != nil {
		cmd.Process.Kill()
		<-done
		finished = true
		t.Fatalf("real progress Web acceptance: %v\n%s\nAgent fixture: %s", e, out, diagnostics.String())
	}
	select {
	case e = <-done:
		finished = true
		if e != nil {
			t.Fatalf("Agent after Web cancel: %v %s", e, diagnostics.String())
		}
	case <-ctx.Done():
		t.Fatal("Web cancellation did not finish Agent")
	}
	final, e := s.Get(t.Context(), id)
	if e != nil || final.Status != "canceled" || final.ProgressJSON == "" {
		t.Fatal("Web cancellation lost progress", final, e)
	}
	status, body := getJSON(t, url+fmt.Sprintf("/api/v1/scans/%d", id), token)
	if status != 200 {
		t.Fatal(status, string(body))
	}
}

func getJSON(t *testing.T, url, token string) (int, []byte) {
	t.Helper()
	req, e := http.NewRequestWithContext(t.Context(), "GET", url, nil)
	if e != nil {
		t.Fatal(e)
	}
	req.Header.Set("Authorization", "Bearer "+token)
	res, e := http.DefaultClient.Do(req)
	if e != nil {
		t.Fatal(e)
	}
	defer res.Body.Close()
	raw, e := io.ReadAll(io.LimitReader(res.Body, contracts.MaxDocumentBytes))
	if e != nil {
		t.Fatal(e)
	}
	return res.StatusCode, raw
}
