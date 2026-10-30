package store

import (
	"axiom/contracts"
	"context"
	"fmt"
)

// Read only the existing attempt snapshot. Later model edits cannot change it.
func (s *Store) ExecutionTokenBudget(ctx context.Context, id int64, attempt int, limitUSD float64) (*contracts.TokenBudgetAssignment, error) {
	n, err := BudgetLimitMicros(limitUSD)
	if err != nil {
		return nil, err
	}
	var raw string
	if err = s.db.QueryRowContext(ctx, `SELECT pricing_snapshot_json FROM task_token_costs WHERE task_id=? AND attempt=?`, id, attempt).Scan(&raw); err != nil {
		return nil, err
	}
	execution := contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: int64(attempt)}
	price, err := contracts.DecodeModelPricingSnapshot([]byte(raw), &execution)
	if err != nil {
		return nil, err
	}
	return &contracts.TokenBudgetAssignment{SchemaVersion: 1, Execution: execution, LimitMicros: n, PricingSnapshot: price}, nil
}
