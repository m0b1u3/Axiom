package store

import (
	"axiom/contracts"
	"context"
	"database/sql"
	"encoding/json"
)

type matchingPricingQuery interface {
	QueryRowContext(context.Context, string, ...any) *sql.Row
}

// Accept only the execution's existing frozen price. Never look up the current
// model price or derive new matching fees from legacy progress.
func frozenMatchingEstimate(ctx context.Context, q matchingPricingQuery, raw json.RawMessage, ref contracts.ExecutionRef) (*contracts.TokenCostEstimate, error) {
	if len(raw) == 0 {
		return nil, nil
	}
	estimate, e := contracts.DecodeTokenCostEstimate(raw, &ref)
	if e != nil {
		return nil, ErrInvalidResult
	}
	var frozen string
	if e = q.QueryRowContext(ctx, `SELECT pricing_snapshot_json FROM task_token_costs WHERE task_id=? AND attempt=?`, ref.TaskID, ref.Attempt).Scan(&frozen); e != nil {
		return nil, e
	}
	snapshot, e := contracts.DecodeModelPricingSnapshot([]byte(frozen), &ref)
	if e != nil || !sameJSON(snapshot, estimate.PricingSnapshot) {
		return nil, ErrInvalidResult
	}
	expected := estimateTokens(snapshot, estimate.Usage)
	expected.ComputedAt = estimate.ComputedAt
	if !sameJSON(expected, estimate) {
		return nil, ErrInvalidResult
	}
	return &estimate, nil
}

// Project only data actually saved in this attempt. Old records have no
// matching estimate and retain their original unknown state and original fees.
func savedMatchingEstimate(ref contracts.ExecutionRef, result, progress string) (*contracts.TokenCostEstimate, error) {
	var completion struct {
		Result struct {
			Extensions map[string]json.RawMessage `json:"extensions"`
		} `json:"result"`
	}
	var observation struct {
		Extensions map[string]json.RawMessage `json:"extensions"`
	}
	var raw json.RawMessage
	if result != "" {
		if e := json.Unmarshal([]byte(result), &completion); e != nil {
			return nil, e
		}
		raw = completion.Result.Extensions["componentMatchingEstimate"]
	}
	if len(raw) == 0 && progress != "" {
		if e := json.Unmarshal([]byte(progress), &observation); e != nil {
			return nil, e
		}
		raw = observation.Extensions["componentMatchingEstimate"]
	}
	if len(raw) == 0 {
		return nil, nil
	}
	estimate, e := contracts.DecodeTokenCostEstimate(raw, &ref)
	if e != nil {
		return nil, e
	}
	return &estimate, nil
}
