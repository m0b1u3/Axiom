package store

import (
	"axiom/contracts"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"sync"
	"testing"
	"time"
)

func observationCaps() contracts.ObservationCapabilities {
	return contracts.ObservationCapabilities{JobVersions: []int64{3}, ProgressVersions: []int64{2}, EventVersions: []int64{2}, HeartbeatVersions: []int64{1}}
}
func progressFixture(t *testing.T, task *Task, revision int64) contracts.ProgressSnapshotV2 {
	t.Helper()
	raw, e := os.ReadFile("../../../contracts/fixtures/v1/observation-null.json")
	if e != nil {
		t.Fatal(e)
	}
	p, e := contracts.DecodeProgressSnapshotV2(raw, nil)
	if e != nil {
		t.Fatal(e)
	}
	p.Execution = contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(task.ID)), Attempt: int64(task.Attempt)}
	p.Revision = revision
	return p
}
func TestPostgresObservationContract(t *testing.T) {
	s := pgStore(t)
	ctx := context.Background()
	r := resultFixture(t)
	id, limits := resultTask(t, s, r)
	if _, e := s.db.ExecContext(ctx, `UPDATE scan_tasks SET observation_contract_version=1 WHERE id=?`, id); e != nil {
		t.Fatal(e)
	}
	input, output := modernCaps()
	if task, e := s.ClaimExecutionWithContracts(ctx, "old-agent", limits, input, output); e != nil || task != nil {
		t.Fatal("old observer started a new task", e)
	}
	if e := s.ReportAgentWithContracts(ctx, "old-agent", 1, input, output); e != nil {
		t.Fatal(e)
	}
	q, e := s.QueueOverview(ctx, limits, id)
	if e != nil || q.Tasks[0].Reason != "observation_incompatible" {
		t.Fatal(q, e)
	}
	task, e := s.ClaimExecutionWithObservations(ctx, "agent", limits, input, output, observationCaps())
	if e != nil || task == nil {
		t.Fatal(e)
	}
	p := progressFixture(t, task, 2)
	h := contracts.ExecutionHeartbeat{SchemaVersion: 1, AgentID: contracts.Identifier(task.AgentID), Execution: p.Execution, Progress: &p}
	reply, e := s.HeartbeatContractExecution(ctx, h, time.Minute)
	if e != nil || !reply.Accepted || reply.Revision != 2 {
		t.Fatal(reply, e)
	}
	p.Revision = 1
	p.Phase = "validation"
	reply, e = s.HeartbeatContractExecution(ctx, h, time.Minute)
	if e != nil || reply.Accepted || reply.Revision != 2 {
		t.Fatal("old revision replaced snapshot", reply, e)
	}
	stored, e := s.Get(ctx, id)
	if e != nil {
		t.Fatal(e)
	}
	before := stored.ProgressJSON
	if stored.CurrentStage != "preparing" {
		t.Fatal(stored.CurrentStage)
	}
	h.Progress = nil
	reply, e = s.HeartbeatContractExecution(ctx, h, time.Minute)
	if e != nil || reply.Accepted {
		t.Fatal(reply, e)
	}
	stored, _ = s.Get(ctx, id)
	if stored.ProgressJSON != before {
		t.Fatal("null heartbeat erased valid snapshot")
	}
	bad := h
	bad.Execution.Attempt++
	if _, e = s.HeartbeatContractExecution(ctx, bad, time.Minute); !errors.Is(e, ErrLeaseLost) {
		t.Fatal("old execution accepted", e)
	}
	bad = h
	bad.AgentID = "other-agent"
	if _, e = s.HeartbeatContractExecution(ctx, bad, time.Minute); !errors.Is(e, ErrLeaseLost) {
		t.Fatal("wrong agent accepted", e)
	}
	if _, e = s.HeartbeatExecution(ctx, LeaseRef{TaskID: id, Attempt: task.Attempt, AgentID: task.AgentID}, time.Minute, "failed", `{}`); e == nil {
		t.Fatal("legacy progress overwrote new contract")
	}
	// A terminal-looking observation cannot close the business control.
	p.Revision = 3
	p.Phase = "completed"
	h.Progress = &p
	if _, e = s.HeartbeatContractExecution(ctx, h, time.Minute); e != nil {
		t.Fatal(e)
	}
	stored, _ = s.Get(ctx, id)
	if stored.Status != "scanning" {
		t.Fatal(stored.Status)
	}
	// Concurrent arrivals still keep the greatest revision.
	var wg sync.WaitGroup
	for _, revision := range []int64{4, 8, 6, 5, 7} {
		wg.Add(1)
		go func(revision int64) {
			defer wg.Done()
			v := p
			v.Revision = revision
			copy := h
			copy.Progress = &v
			if _, err := s.HeartbeatContractExecution(ctx, copy, time.Minute); err != nil {
				t.Error(err)
			}
		}(revision)
	}
	wg.Wait()
	stored, _ = s.Get(ctx, id)
	var accepted contracts.ProgressSnapshotV2
	json.Unmarshal([]byte(stored.ProgressJSON), &accepted)
	if accepted.Revision != 8 {
		t.Fatal(accepted.Revision)
	}
	if ok, e := s.RequestCancel(ctx, id); e != nil || !ok {
		t.Fatal("cancel failed", e)
	}
	reply, e = s.HeartbeatContractExecution(ctx, h, time.Minute)
	if e != nil || reply.Status != "cancel_requested" {
		t.Fatal("cancel response was not delivered to Agent", reply, e)
	}
	done := completedFixture(r, task)
	if e = s.CompleteContractExecution(ctx, done); e != nil {
		t.Fatal(e)
	}
	if _, e = s.HeartbeatContractExecution(ctx, h, time.Minute); !errors.Is(e, ErrLeaseLost) {
		t.Fatal("terminal execution reopened", e)
	}
	history, e := s.ListAttempts(ctx, id)
	if e != nil || history[0].Progress == nil || history[0].Progress.Revision != 8 {
		t.Fatal(history, e)
	}
}
