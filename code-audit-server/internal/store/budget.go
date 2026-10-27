package store

import (
	"axiom/contracts"
	"context"
	"database/sql"
	_ "embed"
	"errors"
	"fmt"
	"math"
	"math/big"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

//go:embed budget.sql
var budgetSQL string

const MaxMoneyMicros int64 = 9007199254740991

var ErrBudgetPolicy = errors.New("预算须为最多六位小数的正数，最高 9007199254.740991 USD；累计预算需要新版快照任务")
var ErrBudgetUnknown = errors.New("本任务存在费用未知的执行，请先在 Web 核对估算费用，不能按零费用重试")
var ErrBudgetExhausted = errors.New("任务累计预算已用尽，请在 Web 调整总预算后重试")
var ErrCostBelowRecorded = errors.New("人工估算不能低于已上报的费用下界")
var ErrBudgetBelowRecorded = errors.New("总预算不能低于已记录的累计估算费用")
var ErrBudgetConflict = errors.New("预算或任务状态已变化，请重新加载后操作")

func moneyMicros(v float64, roundUp bool) (int64, error) {
	if math.IsNaN(v) || math.IsInf(v, 0) || v < 0 {
		return 0, ErrBudgetPolicy
	}
	r, ok := new(big.Rat).SetString(strconv.FormatFloat(v, 'f', -1, 64))
	if !ok {
		return 0, ErrBudgetPolicy
	}
	r.Mul(r, big.NewRat(1000000, 1))
	n, rem := new(big.Int), new(big.Int)
	n.QuoRem(r.Num(), r.Denom(), rem)
	if rem.Sign() != 0 {
		if !roundUp {
			return 0, ErrBudgetPolicy
		}
		n.Add(n, big.NewInt(1))
	}
	if !n.IsInt64() || n.Int64() > MaxMoneyMicros {
		return 0, ErrBudgetPolicy
	}
	return n.Int64(), nil
}
func BudgetLimitMicros(v float64) (int64, error) {
	n, e := moneyMicros(v, false)
	if e != nil || n < 1 {
		return 0, ErrBudgetPolicy
	}
	return n, nil
}
func dollars(n int64) float64 {
	v := float64(n) / 1000000
	if rounded, e := moneyMicros(v, true); e != nil || rounded > n {
		v = math.Nextafter(v, 0)
	}
	return v
}
func pointer(n int64) *int64 { return &n }
func amount(n *int64) *contracts.MoneyMicros {
	if n == nil {
		return nil
	}
	v := contracts.MoneyMicros(*n)
	return &v
}

type budgetView struct {
	snapshot          contracts.TaskBudgetSnapshot
	total             *int64
	single            *float64
	sum, reserved     int64
	unknown, overflow bool
	status            string
}

// The caller owns the task row lock for mutations. Reads use a repeatable-read
// transaction, never a sum across unrelated points in time.
func (s *Store) readBudget(ctx context.Context, tx *transaction, id int64) (budgetView, error) {
	v := budgetView{}
	out := &v.snapshot
	out.SchemaVersion = 1
	out.TaskID = contracts.TaskID(fmt.Sprint(id))
	out.Currency = "USD"
	out.IsEstimate = true
	out.State = contracts.BudgetStateLegacy
	out.UnknownAttempts = []int64{}
	out.Attempts = []contracts.AttemptCost{}
	if e := tx.QueryRowContext(ctx, `SELECT status,max_cost_usd FROM scan_tasks WHERE id=?`, id).Scan(&v.status, &v.single); e != nil {
		return v, e
	}
	out.SingleLimitUSD = v.single
	e := tx.QueryRowContext(ctx, `SELECT total_micros,revision FROM task_budgets WHERE task_id=?`, id).Scan(&v.total, &out.Revision)
	if errors.Is(e, sql.ErrNoRows) {
		out.Reason = "historical_semantics"
		out.RetryAllowed = IsTerminalStatus(v.status)
		return v, nil
	}
	if e != nil {
		return v, e
	}
	out.AccountingVersion = 1
	out.TotalLimitMicros = amount(v.total)
	q := `SELECT c.attempt,c.granted_micros,c.booked_micros,c.observed_micros,c.final_micros,c.state,c.source,c.progress_revision,c.reason,c.updated_at,c.note,c.updated_by`
	if s.db.postgres {
		q += `,a.status,COALESCE(a.lease_expires_at,'')`
	}
	q += `,COALESCE(tc.estimate_json,'') FROM task_cost_attempts c LEFT JOIN task_token_costs tc ON tc.task_id=c.task_id AND tc.attempt=c.attempt`
	if s.db.postgres {
		q += ` LEFT JOIN scan_attempts a ON a.task_id=c.task_id AND a.attempt=c.attempt`
	}
	q += ` WHERE c.task_id=? ORDER BY c.attempt`
	rows, e := tx.QueryContext(ctx, q, id)
	if e != nil {
		return v, e
	}
	defer rows.Close()
	for rows.Next() {
		var a contracts.AttemptCost
		var index int64
		var grant, booked, observed, final *int64
		var state, source, updated string
		args := []any{&index, &grant, &booked, &observed, &final, &state, &source, &a.ProgressRevision, &a.Reason, &updated, &a.Note, &a.UpdatedBy}
		var attemptStatus, expires sql.NullString
		var estimateRaw string
		if s.db.postgres {
			args = append(args, &attemptStatus, &expires)
		}
		args = append(args, &estimateRaw)
		if e = rows.Scan(args...); e != nil {
			return v, e
		}
		if state == "active" && s.db.postgres && (!attemptStatus.Valid || !IsRunningStatus(attemptStatus.String) || expires.String < time.Now().UTC().Format(time.RFC3339)) {
			state = "unknown"
			a.Reason = "execution_interrupted"
		}
		a.Execution = contracts.ExecutionRef{TaskID: out.TaskID, Attempt: index}
		a.State = contracts.AttemptCostState(state)
		a.Source = contracts.AttemptCostSource(source)
		a.GrantedMicros = amount(grant)
		a.BookedMicros = amount(booked)
		a.ObservedMicros = amount(observed)
		a.FinalMicros = amount(final)
		a.UpdatedAt = contracts.Timestamp(updated)
		if estimateRaw != "" {
			estimate, e := contracts.DecodeTokenCostEstimate([]byte(estimateRaw), &a.Execution)
			if e != nil {
				return v, e
			}
			a.TokenEstimate = contracts.Some(estimate)
		}
		if booked != nil {
			if v.sum > MaxMoneyMicros-*booked {
				v.overflow = true
				v.sum = MaxMoneyMicros
			} else {
				v.sum += *booked
			}
		}
		if state == "unknown" {
			v.unknown = true
			out.UnknownAttempts = append(out.UnknownAttempts, index)
		}
		if state == "active" && grant != nil {
			used := int64(0)
			if booked != nil {
				used = *booked
			}
			remaining := max(0, *grant-used)
			if v.reserved > MaxMoneyMicros-remaining {
				v.overflow = true
				v.reserved = MaxMoneyMicros
			} else {
				v.reserved += remaining
			}
		}
		out.Attempts = append(out.Attempts, a)
	}
	if e = rows.Err(); e != nil {
		return v, e
	}
	out.EstimatedMicros = amount(pointer(v.sum))
	out.ReservedMicros = amount(pointer(v.reserved))
	out.State = contracts.BudgetStateNoTotalLimit
	if v.total != nil {
		out.State = contracts.BudgetStateReady
		left := max(0, *v.total-v.sum-v.reserved)
		out.RemainingMicros = amount(&left)
		if left == 0 {
			out.State = contracts.BudgetStateExhausted
			out.Reason = "total_limit_reached"
		}
		if IsRunningStatus(v.status) && v.status != "pending" {
			out.State = contracts.BudgetStateRunning
		}
		if v.unknown || v.overflow {
			out.State = contracts.BudgetStateUnknown
			out.RemainingMicros = nil
			out.Reason = "unreconciled_cost"
		}
		if out.State == contracts.BudgetStateReady {
			limit := left
			if v.single != nil && *v.single < dollars(limit) {
				n, e := moneyMicros(*v.single, false)
				if e != nil || n < 1 {
					return v, ErrBudgetPolicy
				}
				limit = n
			}
			out.NextLimitMicros = amount(&limit)
		}
	}
	if v.total == nil && v.unknown {
		out.Reason = "partial_cost_without_total_limit"
	}
	out.RetryAllowed = IsTerminalStatus(v.status) && (v.total == nil || out.State == contracts.BudgetStateReady)
	return v, nil
}
func (s *Store) BudgetSnapshot(ctx context.Context, id int64) (contracts.TaskBudgetSnapshot, error) {
	options := &sql.TxOptions{}
	if s.db.postgres {
		options.Isolation = sql.LevelRepeatableRead
		options.ReadOnly = true
	}
	tx, e := s.db.BeginTx(ctx, options)
	if e != nil {
		return contracts.TaskBudgetSnapshot{}, e
	}
	defer tx.Rollback()
	v, e := s.readBudget(ctx, tx, id)
	if e != nil {
		return v.snapshot, e
	}
	return v.snapshot, tx.Commit()
}
func (s *Store) checkBudgetAdmission(ctx context.Context, tx *transaction, id int64) (*int64, error) {
	v, e := s.readBudget(ctx, tx, id)
	if e != nil {
		return nil, e
	}
	if v.total == nil {
		return nil, nil
	}
	if v.unknown || v.overflow {
		return nil, ErrBudgetUnknown
	}
	if v.snapshot.NextLimitMicros == nil {
		// Admission for pending/recovered tasks is calculated independently of UI state.
		left := *v.total - v.sum - v.reserved
		if left <= 0 {
			return nil, ErrBudgetExhausted
		}
		if v.single != nil && *v.single < dollars(left) {
			left, e = moneyMicros(*v.single, false)
			if e != nil || left < 1 {
				return nil, ErrBudgetPolicy
			}
		}
		return &left, nil
	}
	n := int64(*v.snapshot.NextLimitMicros)
	return &n, nil
}
func (s *Store) reserveAttemptBudget(ctx context.Context, tx *transaction, id int64, index int, grant *int64) error {
	if grant == nil {
		var single *float64
		if e := tx.QueryRowContext(ctx, `SELECT max_cost_usd FROM scan_tasks WHERE id=?`, id).Scan(&single); e != nil {
			return e
		}
		if single != nil {
			if n, e := moneyMicros(*single, false); e == nil && n > 0 {
				grant = &n
			}
		}
	}

	_, e := tx.ExecContext(ctx, `INSERT INTO task_cost_attempts(task_id,attempt,granted_micros,state,updated_at) SELECT task_id,?,?,'active',? FROM task_budgets WHERE task_id=?`, index, grant, securityTime(time.Now()), id)
	if e != nil {
		return e
	}
	if e = s.freezeAttemptPricing(ctx, tx, id, index); e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE task_budgets SET revision=revision+1 WHERE task_id=?`, id)
	return e
}
func (s *Store) recordProgressCost(ctx context.Context, tx *transaction, id int64, index int64, p *contracts.ProgressSnapshotV2) error {
	if e := s.recordTokenProgress(ctx, tx, id, index, p); e != nil {
		return e
	}
	// Configured prices own platform accounting; native SDK cost evidence is kept
	// separately and must not be mixed with another provider or currency tariff.
	var raw string
	e := tx.QueryRowContext(ctx, `SELECT pricing_snapshot_json FROM task_token_costs WHERE task_id=? AND attempt=?`, id, index).Scan(&raw)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return e
	}
	if raw != "" {
		snap, e := contracts.DecodeModelPricingSnapshot([]byte(raw), nil)
		if e != nil {
			return e
		}
		if snap.Pricing != nil {
			return nil
		}
	}

	if p == nil || p.Cost == nil {
		return nil
	}
	c := p.Cost
	var n *int64
	reason := ""
	if c.EstimatedUSD != nil && c.Currency == "USD" {
		v, e := moneyMicros(*c.EstimatedUSD, true)
		if e == nil {
			n = &v
		} else {
			reason = "amount_out_of_range"
		}
	} else {
		reason = "cost_unavailable"
	}
	result, e := tx.ExecContext(ctx, `UPDATE task_cost_attempts SET observed_micros=CASE WHEN CAST(? AS BIGINT) IS NULL THEN observed_micros WHEN observed_micros IS NULL OR observed_micros<? THEN ? ELSE observed_micros END,booked_micros=CASE WHEN CAST(? AS BIGINT) IS NULL THEN booked_micros WHEN booked_micros IS NULL OR booked_micros<? THEN ? ELSE booked_micros END,source='sdk_progress',progress_revision=?,reason=?,updated_at=? WHERE task_id=? AND attempt=? AND state='active' AND progress_revision<?`, n, n, n, n, n, n, p.Revision, reason, securityTime(time.Now()), id, index, p.Revision)
	if e != nil {
		return e
	}
	count, _ := result.RowsAffected()
	if count == 0 {
		return nil
	}
	_, e = tx.ExecContext(ctx, `UPDATE task_budgets SET revision=revision+1 WHERE task_id=?`, id)
	return e
}
func (s *Store) recordFinalCost(ctx context.Context, tx *transaction, c Completion) error {
	// Component results can have known per-component lower bounds while matching
	// has no fee API. Book those observations even if the final heartbeat was lost.
	if c.contract != nil && c.contract.Result.Present && c.contract.Result.Value.Extensions.Present {
		if raw := c.contract.Result.Value.Extensions.Value["componentScan"]; raw != nil {
			v, e := contracts.DecodeComponentScanResult(raw, &c.contract.Execution)
			if e != nil {
				return ErrInvalidResult
			}
			lower := v.Snapshot.ObservedMicros
			if _, e = tx.ExecContext(ctx, `UPDATE task_cost_attempts SET observed_micros=CASE WHEN observed_micros IS NULL OR observed_micros<? THEN ? ELSE observed_micros END,booked_micros=CASE WHEN booked_micros IS NULL OR booked_micros<? THEN ? ELSE booked_micros END,source='sdk_progress' WHERE task_id=? AND attempt=?`, lower, lower, lower, lower, c.TaskID, c.Attempt); e != nil {
				return e
			}
		}
	}

	var n *int64
	reason := "final_cost_missing"
	if c.contract != nil && c.contract.Result.Present && c.contract.Result.Value.Cost.Present && c.contract.Result.Value.Cost.Value != nil {
		cost := c.contract.Result.Value.Cost.Value
		if cost.EstimatedUSD != nil && cost.Currency == "USD" {
			v, e := moneyMicros(*cost.EstimatedUSD, true)
			if e == nil {
				n = &v
				reason = ""
			} else {
				reason = "amount_out_of_range"
			}
		}
	}
	// A recovered/failed result may carry a partial estimate from an older
	// compatible Scanner. It is evidence, not a complete execution settlement.
	if n != nil && c.contract.Result.Value.Error.Present && !componentCostSettled(c.contract.Result.Value) {
		if _, e := tx.ExecContext(ctx, `UPDATE task_cost_attempts SET observed_micros=CASE WHEN observed_micros IS NULL OR observed_micros<? THEN ? ELSE observed_micros END,booked_micros=CASE WHEN booked_micros IS NULL OR booked_micros<? THEN ? ELSE booked_micros END,source='sdk_progress' WHERE task_id=? AND attempt=?`, n, n, n, n, c.TaskID, c.Attempt); e != nil {
			return e
		}
		n = nil
		reason = "execution_incomplete"
	}
	configured, e := s.finalTokenEstimate(ctx, tx, c)
	if e != nil {
		return e
	}
	configuredTotal := false
	localPreflight := false
	if c.contract != nil && c.contract.Result.Present && c.contract.Result.Value.Extensions.Present {
		if raw := c.contract.Result.Value.Extensions.Value["sdkOperation"]; len(raw) > 0 {
			op, e := contracts.DecodeSDKOperationResult(raw, &c.contract.Execution)
			localPreflight = e == nil && op.Kind == "preflight"
		}
	}
	if !localPreflight && configured != nil && configured.PricingSnapshot.Pricing != nil {
		n = nil
		reason = "configured_price_unknown"
		if configured.Currency != nil && *configured.Currency != "USD" {
			reason = "configured_price_currency"
		}
		if configured.Status == "estimated" && configured.Currency != nil && *configured.Currency == "USD" && configured.AmountMicros != nil {
			n = configured.AmountMicros
			reason = "configured_price_estimate"
			configuredTotal = true
		}
	}
	state, source := "unknown", "none"
	if c.recovery != nil {
		var previousSource string
		e := tx.QueryRowContext(ctx, `SELECT source FROM task_cost_attempts WHERE task_id=? AND attempt=?`, c.TaskID, c.Attempt).Scan(&previousSource)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if previousSource == "manual_estimate" {
			// Preserve the operator's estimate and its original provenance. A
			// later recovered SDK total can raise, never lower, booked costs.
			if _, e = tx.ExecContext(ctx, `UPDATE task_cost_attempts SET final_micros=COALESCE(CAST(? AS BIGINT),final_micros),reason=CASE WHEN CAST(? AS BIGINT)>booked_micros THEN 'recovered_final_above_manual' ELSE reason END,booked_micros=CASE WHEN CAST(? AS BIGINT)>booked_micros THEN ? ELSE booked_micros END,updated_at=? WHERE task_id=? AND attempt=?`, n, n, n, n, securityTime(time.Now()), c.TaskID, c.Attempt); e != nil {
				return e
			}
			_, e = tx.ExecContext(ctx, `UPDATE task_budgets SET revision=revision+1 WHERE task_id=?`, c.TaskID)
			return e
		}
	}
	if n != nil {
		state, source = "complete", "sdk_result"
		if configuredTotal {
			source = "configured_pricing"
		}
	}
	_, e = tx.ExecContext(ctx, `UPDATE task_cost_attempts SET final_micros=?,booked_micros=CASE WHEN CAST(? AS BIGINT) IS NULL THEN booked_micros WHEN booked_micros IS NULL OR booked_micros<? THEN ? ELSE booked_micros END,state=?,source=CASE WHEN CAST(? AS BIGINT) IS NULL AND observed_micros IS NOT NULL THEN CASE WHEN source='configured_pricing' THEN 'configured_pricing' ELSE 'sdk_progress' END ELSE ? END,reason=CASE WHEN CAST(? AS BIGINT) IS NOT NULL AND observed_micros>? THEN 'final_below_progress' ELSE ? END,updated_at=? WHERE task_id=? AND attempt=?`, n, n, n, n, state, n, source, n, n, reason, securityTime(time.Now()), c.TaskID, c.Attempt)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE task_budgets SET revision=revision+1 WHERE task_id=?`, c.TaskID)
	return e
}
func (s *Store) ReconcileAttemptCost(ctx context.Context, id int64, index int, usd float64, note, actor string, revision int64) error {
	note = strings.TrimSpace(note)
	n, e := moneyMicros(usd, false)
	if e != nil || utf8.RuneCountInString(note) < 4 || len([]rune(note)) > 1024 {
		return ErrBudgetPolicy
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if s.db.postgres {
		var locked int64
		if e = tx.QueryRowContext(ctx, `SELECT id FROM scan_tasks WHERE id=? FOR UPDATE`, id).Scan(&locked); e != nil {
			return e
		}
	}
	v, e := s.readBudget(ctx, tx, id)
	if e != nil {
		return e
	}
	if v.snapshot.AccountingVersion != 1 || v.snapshot.Revision != revision {
		return ErrBudgetConflict
	}
	var target *contracts.AttemptCost
	for i := range v.snapshot.Attempts {
		if v.snapshot.Attempts[i].Execution.Attempt == int64(index) {
			target = &v.snapshot.Attempts[i]
		}
	}
	if target == nil || target.State != contracts.AttemptCostStateUnknown || !IsTerminalStatus(v.status) {
		return ErrBudgetConflict
	}
	if target.BookedMicros != nil && n < int64(*target.BookedMicros) {
		return ErrCostBelowRecorded
	}
	_, e = tx.ExecContext(ctx, `UPDATE task_cost_attempts SET booked_micros=?,state='complete',source='manual_estimate',reason='operator_reconciled',note=?,updated_by=?,updated_at=? WHERE task_id=? AND attempt=?`, n, note, actor, securityTime(time.Now()), id, index)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE task_budgets SET revision=revision+1 WHERE task_id=?`, id)
	if e != nil {
		return e
	}
	return tx.Commit()
}
func (s *Store) UpdateTotalBudget(ctx context.Context, id int64, usd float64, revision int64) error {
	n, e := BudgetLimitMicros(usd)
	if e != nil {
		return e
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return e
	}
	defer tx.Rollback()
	if s.db.postgres {
		var locked int64
		if e = tx.QueryRowContext(ctx, `SELECT id FROM scan_tasks WHERE id=? FOR UPDATE`, id).Scan(&locked); e != nil {
			return e
		}
	}
	var ops int
	if e = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM sdk_operations WHERE task_id=?)+(SELECT COUNT(*) FROM component_operations WHERE task_id=?)`, id, id).Scan(&ops); e != nil {
		return e
	}
	if ops > 0 {
		return ErrBudgetPolicy
	}
	v, e := s.readBudget(ctx, tx, id)
	if e != nil {
		return e
	}
	if v.snapshot.AccountingVersion != 1 || v.snapshot.Revision != revision || (IsRunningStatus(v.status) && v.status != "pending") {
		return ErrBudgetConflict
	}
	if n < v.sum {
		return ErrBudgetBelowRecorded
	}
	_, e = tx.ExecContext(ctx, `UPDATE task_budgets SET total_micros=?,revision=revision+1 WHERE task_id=?`, n, id)
	if e != nil {
		return e
	}
	return tx.Commit()
}

func componentCostSettled(result contracts.ScanResult) bool {
	if !result.Extensions.Present {
		return false
	}
	raw := result.Extensions.Value["componentScan"]
	if raw == nil {
		return false
	}
	v, e := contracts.DecodeComponentScanResult(raw, &result.Execution)
	if e != nil || v.Snapshot.Matching.CostState != "known" {
		return false
	}
	for _, r := range v.Snapshot.Components {
		if r.FinalMicros == nil {
			return false
		}
	}
	return true
}
