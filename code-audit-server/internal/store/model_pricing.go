package store

import (
	"axiom/contracts"
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"math/big"
	"strings"
	"time"
)

var ErrModelPricing = errors.New("模型单价无效：使用三位大写币种、每百万 Token 单价，金额为非负十进制字符串，最多六位小数")

func pricingJSON(pricing *contracts.ModelPricing) (string, error) {
	if pricing == nil {
		return "", nil
	}
	raw, e := contracts.EncodeJSON(pricing)
	if e != nil || contracts.ValidateJSON(contracts.KindModelPricing, raw, nil) != nil {
		return "", ErrModelPricing
	}
	return string(raw), nil
}

// ParseModelPricing distinguishes an omitted API property from an explicit null.
func ParseModelPricing(raw json.RawMessage) (*contracts.ModelPricing, error) {
	if len(raw) == 0 || string(raw) == "null" {
		return nil, nil
	}
	value, e := contracts.DecodeModelPricing(raw, nil)
	if e != nil {
		return nil, ErrModelPricing
	}
	return &value, nil
}

func decodePricing(raw string) (*contracts.ModelPricing, error) {
	if raw == "" {
		return nil, nil
	}
	return ParseModelPricing(json.RawMessage(raw))
}

// Calculate with decimal rationals; round once, upward, to a currency millionth.
func estimateTokens(snapshot contracts.ModelPricingSnapshot, usage *contracts.ExecutionTokenUsage) contracts.TokenCostEstimate {
	v := contracts.TokenCostEstimate{SchemaVersion: 1, Execution: snapshot.Execution, Method: "configured_token_prices", PricingSnapshot: snapshot, Usage: usage, Status: "unknown", Reason: "price_missing", ComputedAt: contracts.Timestamp(time.Now().UTC().Format(time.RFC3339Nano))}
	p := snapshot.Pricing
	if p == nil {
		if snapshot.Reason != "" {
			v.Reason = snapshot.Reason
		}
		return v
	}
	v.Currency = &p.Currency
	if p.InputPerMillion == nil || p.OutputPerMillion == nil || (p.CacheMode != "input_only" && p.CacheReadPerMillion == nil) || (p.CacheMode == "cache_read_write" && p.CacheWritePerMillion == nil) {
		return v
	}
	if usage == nil {
		v.Reason = "usage_missing"
		return v
	}
	raw, e := contracts.EncodeJSON(usage)
	if e != nil || contracts.ValidateJSON(contracts.KindExecutionTokenUsage, raw, &snapshot.Execution) != nil || usage.InputTokens == nil || usage.OutputTokens == nil {
		v.Reason = "usage_missing"
		return v
	}
	input, output := *usage.InputTokens, *usage.OutputTokens
	if usage.TotalTokens != nil && (input > MaxMoneyMicros-output || *usage.TotalTokens != input+output) {
		v.Reason = "usage_invalid"
		return v
	}
	if usage.ReasoningOutputTokens != nil && *usage.ReasoningOutputTokens > output {
		v.Reason = "usage_invalid"
		return v
	}
	cached, write := int64(0), int64(0)
	if p.CacheMode != "input_only" {
		if usage.CachedInputTokens == nil {
			v.Reason = "cache_read_usage_missing"
			return v
		}
		cached = *usage.CachedInputTokens
	}
	if p.CacheMode == "cache_read_write" {
		if usage.CacheWriteInputTokens == nil {
			v.Reason = "cache_write_usage_missing"
			return v
		}
		write = *usage.CacheWriteInputTokens
	}
	if cached > input || write > input-cached {
		v.Reason = "usage_invalid"
		return v
	}
	total := new(big.Rat)
	add := func(tokens int64, rate *contracts.TokenUnitPrice) bool {
		r, ok := new(big.Rat).SetString(string(*rate))
		if !ok {
			return false
		}
		total.Add(total, new(big.Rat).Mul(r, new(big.Rat).SetInt64(tokens)))
		return true
	}
	if !add(input-cached-write, p.InputPerMillion) || !add(output, p.OutputPerMillion) {
		v.Reason = "price_invalid"
		return v
	}
	if p.CacheMode != "input_only" && !add(cached, p.CacheReadPerMillion) {
		v.Reason = "price_invalid"
		return v
	}
	if p.CacheMode == "cache_read_write" && !add(write, p.CacheWritePerMillion) {
		v.Reason = "price_invalid"
		return v
	}
	// Per-million token rates and conversion to currency micros cancel. Reasoning
	// is already included in outputTokens and is never added a second time.
	amount, remainder := new(big.Int), new(big.Int)
	amount.QuoRem(total.Num(), total.Denom(), remainder)
	if remainder.Sign() > 0 {
		amount.Add(amount, big.NewInt(1))
	}
	if !amount.IsInt64() || amount.Sign() < 0 || amount.Int64() > MaxMoneyMicros {
		v.Reason = "amount_out_of_range"
		return v
	}
	n := amount.Int64()
	v.AmountMicros = &n
	v.Status = "estimated"
	v.Reason = ""
	if !usage.Complete {
		v.Status = "partial"
		v.Reason = "usage_incomplete"
	}
	return v
}

func (s *Store) freezeAttemptPricing(ctx context.Context, tx *transaction, id int64, index int) error {
	execution := contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: int64(index)}
	snap := contracts.ModelPricingSnapshot{SchemaVersion: 1, Execution: execution, CapturedAt: contracts.Timestamp(time.Now().UTC().Format(time.RFC3339Nano)), Reason: "price_missing"}
	var configID int64
	var endpoint string
	if e := tx.QueryRowContext(ctx, `SELECT COALESCE(model_config_id,0),provider,model,COALESCE(base_url,'') FROM scan_tasks WHERE id=?`, id).Scan(&configID, &snap.Provider, &snap.Model, &endpoint); e != nil {
		return e
	}
	if configID > 0 {
		snap.ModelConfigID = &configID
		var provider, model, base, raw string
		query := `SELECT provider,model,COALESCE(base_url,''),pricing_json,pricing_revision FROM model_configs WHERE id=?`
		if s.db.postgres {
			query += ` FOR SHARE`
		}
		e := tx.QueryRowContext(ctx, query, configID).Scan(&provider, &model, &base, &raw, &snap.Revision)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return e
		}
		if errors.Is(e, sql.ErrNoRows) {
			snap.Reason = "model_config_missing"
		} else if provider != snap.Provider || model != snap.Model || strings.TrimRight(base, "/") != strings.TrimRight(endpoint, "/") {
			snap.Reason = "model_price_binding_mismatch"
		} else {
			snap.Pricing, e = decodePricing(raw)
			if e != nil {
				return e
			}
			if snap.Pricing != nil {
				snap.Reason = ""
			}
		}
	}
	raw, e := contracts.EncodeJSON(snap)
	if e != nil {
		return e
	}
	estimate, e := contracts.EncodeJSON(estimateTokens(snap, nil))
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `INSERT INTO task_token_costs(task_id,attempt,pricing_snapshot_json,estimate_json) VALUES(?,?,?,?)`, id, index, string(raw), string(estimate))
	return e
}

func (s *Store) updateTokenEstimate(ctx context.Context, tx *transaction, id, index int64, usage *contracts.ExecutionTokenUsage, revision int64, final bool) (*contracts.TokenCostEstimate, error) {
	var snapshotRaw, previousUsage string
	var previous int64
	e := tx.QueryRowContext(ctx, `SELECT pricing_snapshot_json,progress_revision,usage_json FROM task_token_costs WHERE task_id=? AND attempt=?`, id, index).Scan(&snapshotRaw, &previous, &previousUsage)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	snapshot, e := contracts.DecodeModelPricingSnapshot([]byte(snapshotRaw), &contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: index})
	if e != nil {
		return nil, e
	}
	if !final && revision <= previous {
		return nil, nil
	}
	regressed := false
	if usage != nil && previousUsage != "" {
		prior, e := contracts.DecodeExecutionTokenUsage([]byte(previousUsage), &snapshot.Execution)
		if e != nil {
			return nil, e
		}
		for _, pair := range [][2]*int64{{prior.InputTokens, usage.InputTokens}, {prior.OutputTokens, usage.OutputTokens}, {prior.CachedInputTokens, usage.CachedInputTokens}, {prior.CacheWriteInputTokens, usage.CacheWriteInputTokens}} {
			if pair[0] != nil && (pair[1] == nil || *pair[1] < *pair[0]) {
				regressed = true
			}
		}
		if regressed {
			prior.Complete = false
			usage = &prior
		}
	}
	estimate := estimateTokens(snapshot, usage)
	if regressed && estimate.AmountMicros != nil {
		estimate.Status = "partial"
		estimate.Reason = "usage_regressed"
	}
	encoded, e := contracts.EncodeJSON(estimate)
	if e != nil {
		return nil, e
	}
	if e = contracts.ValidateJSON(contracts.KindTokenCostEstimate, encoded, &snapshot.Execution); e != nil {
		return nil, e
	}
	usageRaw := ""
	if usage != nil {
		b, e := contracts.EncodeJSON(usage)
		if e != nil {
			return nil, e
		}
		usageRaw = string(b)
	}
	_, e = tx.ExecContext(ctx, `UPDATE task_token_costs SET usage_json=?,estimate_json=?,progress_revision=CASE WHEN progress_revision<? THEN ? ELSE progress_revision END WHERE task_id=? AND attempt=?`, usageRaw, string(encoded), revision, revision, id, index)
	return &estimate, e
}

func executionUsage(progress *contracts.ProgressSnapshotV2) *contracts.ExecutionTokenUsage {
	if progress == nil || !progress.Extensions.Present {
		return nil
	}
	raw := progress.Extensions.Value["executionUsage"]
	if len(raw) > 0 {
		value, e := contracts.DecodeExecutionTokenUsage(raw, &progress.Execution)
		if e == nil {
			return &value
		}
		return nil
	}
	raw = progress.Extensions.Value["sdkUsage"]
	if len(raw) == 0 {
		return nil
	}
	usage, e := contracts.DecodeSDKUsage(raw, &progress.Execution)
	if e != nil || usage.ComponentIndex.Present {
		return nil
	}
	return &contracts.ExecutionTokenUsage{SchemaVersion: 1, Execution: usage.Execution, Source: "sdk_on_usage", InputTokens: usage.InputTokens, CachedInputTokens: usage.CachedInputTokens, CacheWriteInputTokens: usage.CacheWriteInputTokens, OutputTokens: usage.OutputTokens, ReasoningOutputTokens: usage.ReasoningOutputTokens, TotalTokens: usage.TotalTokens}
}

func (s *Store) recordTokenProgress(ctx context.Context, tx *transaction, id, index int64, p *contracts.ProgressSnapshotV2) error {
	if p == nil {
		return nil
	}
	usage := executionUsage(p)
	if usage == nil {
		return nil
	}
	usage.Complete = false
	estimate, e := s.updateTokenEstimate(ctx, tx, id, index, usage, p.Revision, false)
	if e != nil || estimate == nil {
		return e
	}
	if estimate.Currency == nil || *estimate.Currency != "USD" || estimate.AmountMicros == nil {
		return nil
	}
	n := *estimate.AmountMicros
	_, e = tx.ExecContext(ctx, `UPDATE task_cost_attempts SET observed_micros=CASE WHEN observed_micros IS NULL OR observed_micros<? THEN ? ELSE observed_micros END,booked_micros=CASE WHEN booked_micros IS NULL OR booked_micros<? THEN ? ELSE booked_micros END,source='configured_pricing',reason='configured_price_progress',updated_at=? WHERE task_id=? AND attempt=? AND state='active'`, n, n, n, n, securityTime(time.Now()), id, index)
	if e != nil {
		return e
	}
	_, e = tx.ExecContext(ctx, `UPDATE task_budgets SET revision=revision+1 WHERE task_id=?`, id)
	return e
}

func (s *Store) finalTokenEstimate(ctx context.Context, tx *transaction, c Completion) (*contracts.TokenCostEstimate, error) {
	var usage *contracts.ExecutionTokenUsage
	if c.contract != nil && c.contract.Result.Present && c.contract.Result.Value.Extensions.Present {
		raw := c.contract.Result.Value.Extensions.Value["executionUsage"]
		if len(raw) > 0 {
			value, e := contracts.DecodeExecutionTokenUsage(raw, &c.contract.Execution)
			if e != nil {
				return nil, ErrInvalidResult
			}
			usage = &value
		}
	}
	// A lost terminal receipt cannot turn the last progress subtotal into a total.
	if usage == nil {
		var raw string
		e := tx.QueryRowContext(ctx, `SELECT usage_json FROM task_token_costs WHERE task_id=? AND attempt=?`, c.TaskID, c.Attempt).Scan(&raw)
		if e != nil && !errors.Is(e, sql.ErrNoRows) {
			return nil, e
		}
		if raw != "" {
			value, e := contracts.DecodeExecutionTokenUsage([]byte(raw), nil)
			if e != nil {
				return nil, e
			}
			usage = &value
			usage.Complete = false
		}
	}
	if usage != nil && (c.contract == nil || !c.contract.Result.Present || c.contract.Result.Value.Status != "completed" || c.contract.Result.Value.Error.Present) {
		usage.Complete = false
	}
	return s.updateTokenEstimate(ctx, tx, c.TaskID, int64(c.Attempt), usage, 0, true)
}
