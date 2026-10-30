package store

import (
	"axiom/contracts"
	"axiom/contracts/control"
	"errors"
	"fmt"
	"math"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"
)

func TestBudgetSQLiteImportPreservesLedgerAndHistory(t *testing.T) {
	for _, modern := range []bool{false, true} {
		t.Run(fmt.Sprint("ledger-", modern), func(t *testing.T) {
			target := pgStore(t)
			file := filepath.Join(t.TempDir(), "fixture-import.db")
			source, e := Open(file)
			if e != nil {
				t.Fatal(e)
			}
			t.Cleanup(func() { source.Close() })
			legacy, e := source.InsertPending(t.Context(), Task{ScanNo: "fixture-old-budget", SourcePath: "/fixture/old", Provider: "openai", Model: "fixture"})
			if e != nil {
				t.Fatal(e)
			}
			if _, e = source.RequestCancel(t.Context(), legacy); e != nil {
				t.Fatal(e)
			}
			before := map[int64]contracts.TaskBudgetSnapshot{}
			before[legacy], e = source.BudgetSnapshot(t.Context(), legacy)
			if e != nil {
				t.Fatal(e)
			}
			if modern {
				id, _ := budgetTask(t, source, 5, 4)
				if _, e = source.db.ExecContext(t.Context(), `UPDATE scan_tasks SET status='failed',attempt=2 WHERE id=?`, id); e != nil {
					t.Fatal(e)
				}
				if _, e = source.db.ExecContext(t.Context(), `INSERT INTO task_cost_attempts(task_id,attempt,granted_micros,booked_micros,observed_micros,final_micros,state,source,reason,updated_at,note,updated_by) VALUES(?,1,4000000,1125000,1125000,1125000,'complete','sdk_result','','2026-10-14T01:00:00Z','',''),(?,2,3875000,750000,750000,NULL,'unknown','sdk_progress','final_cost_missing','2026-10-14T02:00:00Z','','')`, id, id); e != nil {
					t.Fatal(e)
				}
				if _, e = source.db.ExecContext(t.Context(), `UPDATE task_budgets SET revision=9 WHERE task_id=?`, id); e != nil {
					t.Fatal(e)
				}
				before[id], e = source.BudgetSnapshot(t.Context(), id)
				if e != nil {
					t.Fatal(e)
				}
			} else {
				if _, e = source.db.ExecContext(t.Context(), `DROP TABLE task_cost_attempts`); e != nil {
					t.Fatal(e)
				}
				if _, e = source.db.ExecContext(t.Context(), `DROP TABLE task_budgets`); e != nil {
					t.Fatal(e)
				}
			}
			if _, e = target.ImportSQLite(t.Context(), file); e != nil {
				t.Fatal(e)
			}
			for id, want := range before {
				got, e := target.BudgetSnapshot(t.Context(), id)
				if e != nil || !reflect.DeepEqual(got, want) {
					t.Fatalf("import changed budget: got %+v want %+v error %v", got, want, e)
				}
			}
		})
	}
}

func TestBudgetKnownZeroAndConservativeFinal(t *testing.T) {
	s := pgStore(t)
	id, limits := budgetTask(t, s, 5, 4)
	task := budgetClaim(t, s, limits)
	costHeartbeat(t, s, task, 1, 0.75)
	zero := 0.0
	if e := s.CompleteContractExecution(t.Context(), costCompletion(t, task, &zero)); e != nil {
		t.Fatal(e)
	}
	b, e := s.BudgetSnapshot(t.Context(), id)
	if e != nil || *b.EstimatedMicros != 750000 || *b.Attempts[0].FinalMicros != 0 || b.Attempts[0].Reason != "final_below_progress" || b.State != "ready" {
		t.Fatal(b, e)
	}
	if e = s.UpdateTotalBudget(t.Context(), id, 0.5, b.Revision); !errors.Is(e, ErrBudgetBelowRecorded) {
		t.Fatal(e)
	}
	// Explicit zero differs from missing final cost, even without prior callbacks.
	s = pgStore(t)
	id, _ = budgetTask(t, s, 5, 4)
	task = budgetClaim(t, s, limits)
	if e = s.CompleteContractExecution(t.Context(), costCompletion(t, task, &zero)); e != nil {
		t.Fatal(e)
	}
	b, e = s.BudgetSnapshot(t.Context(), id)
	if e != nil || *b.EstimatedMicros != 0 || b.Attempts[0].State != "complete" || b.State != "ready" {
		t.Fatal(b, e)
	}
}

func TestBudgetRequiresCapableConsumerAndFreezesActiveGrant(t *testing.T) {
	s := pgStore(t)
	id, limits := budgetTask(t, s, 5, 4)
	queue, e := s.QueueOverview(t.Context(), limits, id)
	if e != nil || len(queue.Tasks) != 1 || queue.Tasks[0].Reason == "budget_exhausted" {
		t.Fatal("an empty ledger blocked a fresh task", queue, e)
	}
	input, output := modernCaps()
	input.MaxCostUSD = false
	if task, e := s.ClaimExecutionWithObservations(t.Context(), "fixture-no-budget", limits, input, output, observationCaps()); e != nil || task != nil {
		t.Fatal(task, e)
	}
	task := budgetClaim(t, s, limits)
	b, e := s.BudgetSnapshot(t.Context(), id)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.UpdateTotalBudget(t.Context(), id, 9, b.Revision); !errors.Is(e, ErrBudgetConflict) {
		t.Fatal("active budget mutation", e)
	}
	if e = s.ReconcileAttemptCost(t.Context(), id, 1, 0, "核对凭据", "fixture-admin", b.Revision); !errors.Is(e, ErrBudgetConflict) {
		t.Fatal("active cost mutation", e)
	}
	if *task.ExecutionMaxCostUSD != 4 {
		t.Fatal(task)
	}
}

func TestBudgetOlderCompatibleScannerPartialCostIsUnknown(t *testing.T) {
	s := pgStore(t)
	id, limits := budgetTask(t, s, 5, 4)
	task := budgetClaim(t, s, limits)
	usd := 0.75
	done := costCompletion(t, task, &usd)
	done.ExecutionStatus = "failed"
	code := int64(2)
	done.Process.Code = &code
	done.Result.Value.Status = "failed"
	done.Result.Value.Error = contracts.Some(contracts.ContractError{Code: "provider_unavailable", Origin: "engine", Message: "Fixture interrupted SDK cost fallback", Retryable: true})
	done.Error = done.Result.Value.Error
	if e := s.CompleteContractExecution(t.Context(), done); e != nil {
		t.Fatal(e)
	}
	b, e := s.BudgetSnapshot(t.Context(), id)
	if e != nil || b.State != "unknown" || *b.EstimatedMicros != 750000 || b.Attempts[0].FinalMicros != nil || b.RetryAllowed {
		t.Fatal(b, e)
	}
}

func budgetTask(t *testing.T, s *Store, total, single float64) (int64, ClaimLimits) {
	t.Helper()
	r := resultFixture(t)
	id, limits := resultTask(t, s, r)
	if _, e := s.db.ExecContext(t.Context(), `UPDATE scan_tasks SET observation_contract_version=1,max_cost_usd=? WHERE id=?`, single, id); e != nil {
		t.Fatal(e)
	}
	n, e := BudgetLimitMicros(total)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.ExecContext(t.Context(), `INSERT INTO task_budgets(task_id,total_micros) VALUES(?,?)`, id, n); e != nil {
		t.Fatal(e)
	}
	return id, limits
}
func budgetClaim(t *testing.T, s *Store, limits ClaimLimits) *Task {
	t.Helper()
	input, output := modernCaps()
	limits.TokenBudgetCapable = true
	if e := s.ReportAgentWithObservations(t.Context(), "fixture-budget-agent", 1, input, output, observationCaps()); e != nil {
		t.Fatal(e)
	}
	version := int64(15)
	build := "Fixtures-Mock-scanner"
	if e := s.ReportAgentRuntime(t.Context(), "fixture-budget-agent", contracts.AgentRuntime{SchemaVersion: 1, AgentBuild: "Fixtures-Mock", ScannerBuild: &build, ControlAPI: 4, State: "ready", MatrixHash: control.MatrixHash, ScannerDescriptionVersion: &version}); e != nil {
		t.Fatal(e)
	}
	task, e := s.ClaimExecutionWithObservations(t.Context(), "fixture-budget-agent", limits, input, output, observationCaps())
	if e != nil || task == nil {
		t.Fatalf("claim %+v %v", task, e)
	}
	return task
}
func costHeartbeat(t *testing.T, s *Store, task *Task, rev int64, usd float64) {
	t.Helper()
	p := progressFixture(t, task, rev)
	p.Cost = &contracts.CostObservation{EstimatedUSD: &usd, Currency: "USD", IsEstimate: true, ObservedAt: contracts.Timestamp(time.Now().UTC().Format(time.RFC3339)), Source: "engine_callback"}
	if _, e := s.HeartbeatContractExecution(t.Context(), contracts.ExecutionHeartbeat{SchemaVersion: 1, Execution: p.Execution, AgentID: contracts.Identifier(task.AgentID), Progress: &p}, time.Minute); e != nil {
		t.Fatal(e)
	}
}
func costCompletion(t *testing.T, task *Task, usd *float64) contracts.ExecutionCompletion {
	t.Helper()
	r := resultFixture(t)
	r.Cost = contracts.Some(&contracts.ScanCost{EstimatedUSD: usd, Currency: "USD", IsEstimate: true})
	return completedFixture(r, task)
}
func TestBudgetExactAmounts(t *testing.T) {
	for _, v := range []float64{math.NaN(), math.Inf(1), 0, -1, 0.0000001, 0.1234567, 9007199255} {
		if _, e := BudgetLimitMicros(v); e == nil {
			t.Fatal("invalid total accepted", v)
		}
	}
	for _, v := range []struct {
		usd   float64
		units int64
	}{{0.000001, 1}, {0.1, 100000}, {1.125, 1125000}, {7.000001, 7000001}} {
		n, e := BudgetLimitMicros(v.usd)
		if e != nil || n != v.units {
			t.Fatal(v, n, e)
		}
	}
	if n, e := moneyMicros(0.00000001, true); e != nil || n != 1 {
		t.Fatal("estimate rounding", n, e)
	}
}
func TestBudgetCumulativeReplayAndFrozenGrant(t *testing.T) {
	s := pgStore(t)
	id, limits := budgetTask(t, s, 5, 4)
	first := budgetClaim(t, s, limits)
	if first.ExecutionMaxCostUSD == nil || *first.ExecutionMaxCostUSD != 4 {
		t.Fatal(first.ExecutionMaxCostUSD)
	}
	costHeartbeat(t, s, first, 2, 1.25)
	costHeartbeat(t, s, first, 2, 1.25)
	costHeartbeat(t, s, first, 1, 9)
	b, e := s.BudgetSnapshot(t.Context(), id)
	if e != nil || *b.EstimatedMicros != 1250000 || *b.ReservedMicros != 2750000 || *b.RemainingMicros != 1000000 {
		t.Fatal(b, e)
	}
	final := 3.125
	done := costCompletion(t, first, &final)
	if e = s.CompleteContractExecution(t.Context(), done); e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteContractExecution(t.Context(), done); e != nil {
		t.Fatal(e)
	}
	b, e = s.BudgetSnapshot(t.Context(), id)
	if e != nil || *b.EstimatedMicros != 3125000 || *b.NextLimitMicros != 1875000 {
		t.Fatal(b, e)
	}
	if raw, e := contracts.EncodeJSON(b); e != nil || contracts.ValidateJSON(contracts.KindTaskBudgetSnapshot, raw, nil) != nil {
		t.Fatal("invalid generated snapshot", e)
	}
	if ok, e := s.Retry(t.Context(), id); e != nil || !ok {
		t.Fatal(ok, e)
	}
	second := budgetClaim(t, s, limits)
	if *second.ExecutionMaxCostUSD != 1.875 {
		t.Fatal("retry got original budget", *second.ExecutionMaxCostUSD)
	}
	if *second.MaxCostUSD != 4 {
		t.Fatal("single-run policy was rewritten")
	}
	costHeartbeat(t, s, second, 1, 0.5)
	final = 1.875
	if e = s.CompleteContractExecution(t.Context(), costCompletion(t, second, &final)); e != nil {
		t.Fatal(e)
	}
	b, _ = s.BudgetSnapshot(t.Context(), id)
	if *b.EstimatedMicros != 5000000 || b.State != "exhausted" || b.RetryAllowed {
		t.Fatal(b)
	}
	if _, e = s.Retry(t.Context(), id); !errors.Is(e, ErrBudgetExhausted) {
		t.Fatal("exhausted retry", e)
	}
	if e = s.UpdateTotalBudget(t.Context(), id, 6, b.Revision); e != nil {
		t.Fatal(e)
	}
	if ok, e := s.Retry(t.Context(), id); e != nil || !ok {
		t.Fatal(ok, e)
	}
	third := budgetClaim(t, s, limits)
	if *third.ExecutionMaxCostUSD != 1 {
		t.Fatal("adjusted grant", third.ExecutionMaxCostUSD)
	}
}
func TestBudgetUnknownDoesNotBecomeZero(t *testing.T) {
	s := pgStore(t)
	id, limits := budgetTask(t, s, 5, 4)
	task := budgetClaim(t, s, limits)
	costHeartbeat(t, s, task, 1, 0.75)
	if e := s.CompleteContractExecution(t.Context(), costCompletion(t, task, nil)); e != nil {
		t.Fatal(e)
	}
	b, e := s.BudgetSnapshot(t.Context(), id)
	if e != nil || b.State != "unknown" || b.RemainingMicros != nil || len(b.UnknownAttempts) != 1 || *b.EstimatedMicros != 750000 {
		t.Fatal(b, e)
	}
	if _, e = s.Retry(t.Context(), id); !errors.Is(e, ErrBudgetUnknown) {
		t.Fatal("unknown retried", e)
	}
	if e = s.ReconcileAttemptCost(t.Context(), id, 1, 0, "fixture evidence explicitly reviewed", "fixture-admin", b.Revision); e == nil {
		t.Fatal("erased observed cost")
	}
	if e = s.ReconcileAttemptCost(t.Context(), id, 1, 1.25, "fixture archived provider estimate reviewed", "fixture-admin", b.Revision); e != nil {
		t.Fatal(e)
	}
	if e = s.ReconcileAttemptCost(t.Context(), id, 1, 1.25, "duplicate fixture", "fixture-admin", b.Revision); !errors.Is(e, ErrBudgetConflict) {
		t.Fatal("stale reconciliation", e)
	}
	b, _ = s.BudgetSnapshot(t.Context(), id)
	if *b.EstimatedMicros != 1250000 || b.Attempts[0].Source != "manual_estimate" || !b.RetryAllowed {
		t.Fatal(b)
	}
}
func TestBudgetInterruptedRunBlocksRecoveryAndLateCost(t *testing.T) {
	s := pgStore(t)
	id, limits := budgetTask(t, s, 5, 4)
	task := budgetClaim(t, s, limits)
	costHeartbeat(t, s, task, 1, 0)
	if _, e := s.db.ExecContext(t.Context(), `UPDATE scan_tasks SET lease_expires_at='2000-01-01T00:00:00Z' WHERE id=?`, id); e != nil {
		t.Fatal(e)
	}
	if _, e := s.db.ExecContext(t.Context(), `UPDATE scan_attempts SET lease_expires_at='2000-01-01T00:00:00Z' WHERE task_id=?`, id); e != nil {
		t.Fatal(e)
	}
	input, output := modernCaps()
	again, e := s.ClaimExecutionWithObservations(t.Context(), "fixture-other-agent", limits, input, output, observationCaps())
	if e != nil || again != nil {
		t.Fatal("unknown recovery started", again, e)
	}
	b, e := s.BudgetSnapshot(t.Context(), id)
	if e != nil || b.State != "unknown" || b.Attempts[0].State != "unknown" {
		t.Fatal(b, e)
	}
	final := 0.5
	if e = s.CompleteContractExecution(t.Context(), costCompletion(t, task, &final)); !errors.Is(e, ErrCompletionConflict) && !errors.Is(e, ErrLeaseLost) {
		t.Fatal("late result changed accounting", e)
	}
	if got, _ := s.Get(t.Context(), id); got.Attempt != 1 || got.Status != "failed" {
		t.Fatal(got)
	}
}
func TestBudgetConcurrentRetriesCannotAllocateTwice(t *testing.T) {
	s := pgStore(t)
	id, limits := budgetTask(t, s, 2, 2)
	task := budgetClaim(t, s, limits)
	final := 0.25
	if e := s.CompleteContractExecution(t.Context(), costCompletion(t, task, &final)); e != nil {
		t.Fatal(e)
	}
	results := make(chan error, 8)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() { defer wg.Done(); _, e := s.Retry(t.Context(), id); results <- e }()
	}
	wg.Wait()
	close(results)
	for e := range results {
		if e != nil {
			t.Fatal(e)
		}
	}
	claims := make(chan *Task, 4)
	errs := make(chan error, 4)
	input, output := modernCaps()
	limits.TokenBudgetCapable = true
	for i := 0; i < 4; i++ {
		agent := fmt.Sprint("fixture-agent-", i)
		if e := s.ReportAgentWithObservations(t.Context(), agent, 1, input, output, observationCaps()); e != nil {
			t.Fatal(e)
		}
		version, build := int64(15), "Fixtures-Mock-scanner"
		if e := s.ReportAgentRuntime(t.Context(), agent, contracts.AgentRuntime{SchemaVersion: 1, AgentBuild: "Fixtures-Mock", ScannerBuild: &build, ScannerDescriptionVersion: &version, ControlAPI: 4, State: "ready", MatrixHash: control.MatrixHash}); e != nil {
			t.Fatal(e)
		}
	}
	for i := 0; i < 4; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			v, e := s.ClaimExecutionWithObservations(t.Context(), fmt.Sprint("fixture-agent-", i), limits, input, output, observationCaps())
			claims <- v
			errs <- e
		}(i)
	}
	wg.Wait()
	close(claims)
	close(errs)
	count := 0
	for task := range claims {
		if task != nil {
			count++
			if *task.ExecutionMaxCostUSD != 1.75 {
				t.Fatal(task.ExecutionMaxCostUSD)
			}
		}
	}
	for e := range errs {
		if e != nil {
			t.Fatal(e)
		}
	}
	if count != 1 {
		t.Fatal("duplicate allocation", count)
	}
}
func TestBudgetHistoricalTaskKeepsPerRunSemantics(t *testing.T) {
	s := pgStore(t)
	r := resultFixture(t)
	id, limits := resultTask(t, s, r)
	input, output := modernCaps()
	task, e := s.ClaimExecutionWithContracts(t.Context(), "fixture-old", limits, input, output)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.CompleteContractExecution(t.Context(), completedFixture(r, task)); e != nil {
		t.Fatal(e)
	}
	b, e := s.BudgetSnapshot(t.Context(), id)
	if e != nil || b.AccountingVersion != 0 || b.EstimatedMicros != nil || !b.RetryAllowed {
		t.Fatal(b, e)
	}
	if ok, e := s.Retry(t.Context(), id); e != nil || !ok {
		t.Fatal(ok, e)
	}
}
