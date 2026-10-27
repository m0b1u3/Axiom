package store

import (
	"axiom/contracts"
	"encoding/json"
	"path/filepath"
	"testing"
	"time"
)

func priceRate(v string) *contracts.TokenUnitPrice { n := contracts.TokenUnitPrice(v); return &n }
func pricingFixture() *contracts.ModelPricing {
	return &contracts.ModelPricing{SchemaVersion: 1, Currency: "USD", Unit: "per_million_tokens", CacheMode: "cache_read", InputPerMillion: priceRate("1"), CacheReadPerMillion: priceRate("0.1"), OutputPerMillion: priceRate("4"), Note: "Fixtures / Mock only"}
}
func tokenFixture(execution contracts.ExecutionRef) contracts.ExecutionTokenUsage {
	return contracts.ExecutionTokenUsage{SchemaVersion: 1, Execution: execution, Source: "sdk_on_usage", Complete: true, InputTokens: pointer(100000), CachedInputTokens: pointer(20000), OutputTokens: pointer(10000), ReasoningOutputTokens: pointer(5000), TotalTokens: pointer(110000)}
}
func TestConfiguredPriceArithmetic(t *testing.T) {
	ref := contracts.ExecutionRef{TaskID: "42", Attempt: 1}
	cases := []struct {
		name   string
		change func(*contracts.ModelPricing, *contracts.ExecutionTokenUsage)
		micros *int64
		state  string
	}{
		{"cache-discount-and-reasoning-not-double-counted", func(*contracts.ModelPricing, *contracts.ExecutionTokenUsage) {}, pointer(122000), "estimated"},
		{"uniform-input-does-not-need-cache-count", func(p *contracts.ModelPricing, u *contracts.ExecutionTokenUsage) {
			p.CacheMode = "input_only"
			u.CachedInputTokens = nil
		}, pointer(140000), "estimated"},
		{"separate-cache-writes", func(p *contracts.ModelPricing, u *contracts.ExecutionTokenUsage) {
			p.CacheMode = "cache_read_write"
			p.CacheWritePerMillion = priceRate("2")
			u.CacheWriteInputTokens = pointer(10000)
		}, pointer(132000), "estimated"},
		{"zero-is-observed", func(p *contracts.ModelPricing, u *contracts.ExecutionTokenUsage) {
			u.InputTokens = pointer(0)
			u.CachedInputTokens = pointer(0)
			u.OutputTokens = pointer(0)
			u.ReasoningOutputTokens = pointer(0)
			u.TotalTokens = pointer(0)
		}, pointer(0), "estimated"},
		{"partial-is-only-a-lower-bound", func(p *contracts.ModelPricing, u *contracts.ExecutionTokenUsage) { u.Complete = false }, pointer(122000), "partial"},
		{"required-price-missing", func(p *contracts.ModelPricing, u *contracts.ExecutionTokenUsage) { p.InputPerMillion = nil }, nil, "unknown"},
		{"required-cache-price-missing-even-for-zero", func(p *contracts.ModelPricing, u *contracts.ExecutionTokenUsage) {
			p.CacheReadPerMillion = nil
			u.CachedInputTokens = pointer(0)
		}, nil, "unknown"},
		{"input-usage-missing", func(p *contracts.ModelPricing, u *contracts.ExecutionTokenUsage) { u.InputTokens = nil }, nil, "unknown"},
		{"cache-read-usage-missing", func(p *contracts.ModelPricing, u *contracts.ExecutionTokenUsage) { u.CachedInputTokens = nil }, nil, "unknown"},
		{"cache-write-usage-missing", func(p *contracts.ModelPricing, u *contracts.ExecutionTokenUsage) {
			p.CacheMode = "cache_read_write"
			p.CacheWritePerMillion = priceRate("2")
		}, nil, "unknown"},
		{"inconsistent-usage", func(p *contracts.ModelPricing, u *contracts.ExecutionTokenUsage) {
			u.CachedInputTokens = pointer(100001)
		}, nil, "unknown"},
		{"foreign-execution", func(p *contracts.ModelPricing, u *contracts.ExecutionTokenUsage) { u.Execution.Attempt++ }, nil, "unknown"},
		{"round-once-upward", func(p *contracts.ModelPricing, u *contracts.ExecutionTokenUsage) {
			p.CacheMode = "input_only"
			p.InputPerMillion = priceRate("0.000001")
			u.InputTokens = pointer(1)
			u.CachedInputTokens = pointer(0)
			u.OutputTokens = pointer(0)
			u.ReasoningOutputTokens = pointer(0)
			u.TotalTokens = pointer(1)
		}, pointer(1), "estimated"},
		{"money-overflow", func(p *contracts.ModelPricing, u *contracts.ExecutionTokenUsage) {
			p.InputPerMillion = priceRate("999999999")
			u.InputTokens = pointer(MaxMoneyMicros - 10000)
			u.TotalTokens = pointer(MaxMoneyMicros)
		}, nil, "unknown"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			p, u := pricingFixture(), tokenFixture(ref)
			tc.change(p, &u)
			snapshot := contracts.ModelPricingSnapshot{SchemaVersion: 1, Execution: ref, Revision: 1, CapturedAt: "2026-10-27T08:00:00Z", Provider: "deepseek", Model: "Fixtures / Mock", Pricing: p}
			got := estimateTokens(snapshot, &u)
			if string(got.Status) != tc.state || (tc.micros == nil) != (got.AmountMicros == nil) || (tc.micros != nil && *tc.micros != *got.AmountMicros) {
				t.Fatalf("got %+v want %s/%v", got, tc.state, tc.micros)
			}
		})
	}
}

func pricedTask(t *testing.T, s *Store, price *contracts.ModelPricing) (int64, ClaimLimits, *ModelConfigRow) {
	t.Helper()
	id, limits := budgetTask(t, s, 20, 10)
	pending, e := s.Get(t.Context(), id)
	if e != nil {
		t.Fatal(e)
	}
	model, e := s.CreateModelConfig(t.Context(), ModelConfigRow{Name: "Fixtures / Mock priced model", Provider: pending.Provider, Model: pending.Model, BaseURL: pending.BaseURL, Pricing: price, ApiKeyEncrypted: "fixture-encrypted-value"})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.db.ExecContext(t.Context(), `UPDATE scan_tasks SET model_config_id=? WHERE id=?`, model.ID, id); e != nil {
		t.Fatal(e)
	}
	return id, limits, model
}
func tokenHeartbeat(t *testing.T, s *Store, task *Task, revision int64, usage contracts.ExecutionTokenUsage) {
	t.Helper()
	p := progressFixture(t, task, revision)
	usage.Execution = p.Execution
	usage.Complete = false
	raw, e := contracts.EncodeJSON(usage)
	if e != nil {
		t.Fatal(e)
	}
	p.Extensions = contracts.Some(contracts.JSONObject{"executionUsage": raw})
	if _, e = s.HeartbeatContractExecution(t.Context(), contracts.ExecutionHeartbeat{SchemaVersion: 1, Execution: p.Execution, AgentID: contracts.Identifier(task.AgentID), Progress: &p}, time.Minute); e != nil {
		t.Fatal(e)
	}
}
func tokenCompletion(t *testing.T, task *Task, usage *contracts.ExecutionTokenUsage) contracts.ExecutionCompletion {
	t.Helper()
	c := costCompletion(t, task, nil)
	if usage != nil {
		copy := *usage
		copy.Execution = c.Execution
		raw, e := contracts.EncodeJSON(copy)
		if e != nil {
			t.Fatal(e)
		}
		if !c.Result.Value.Extensions.Present {
			c.Result.Value.Extensions = contracts.Some(contracts.JSONObject{})
		}
		c.Result.Value.Extensions.Value["executionUsage"] = raw
	}
	return c
}
func requireTokenEstimate(t *testing.T, s *Store, id int64, attempt int) (contracts.TaskBudgetSnapshot, contracts.TokenCostEstimate) {
	t.Helper()
	b, e := s.BudgetSnapshot(t.Context(), id)
	if e != nil {
		t.Fatal(e)
	}
	for _, a := range b.Attempts {
		if a.Execution.Attempt == int64(attempt) && a.TokenEstimate.Present {
			return b, a.TokenEstimate.Value
		}
	}
	t.Fatal("token estimate absent", b)
	return b, contracts.TokenCostEstimate{}
}
func TestConfiguredPriceSnapshotsAndCumulativeAccounting(t *testing.T) {
	s := pgStore(t)
	id, limits, model := pricedTask(t, s, pricingFixture())
	first := budgetClaim(t, s, limits)
	u := tokenFixture(contracts.ExecutionRef{})
	tokenHeartbeat(t, s, first, 1, u)
	tokenHeartbeat(t, s, first, 2, u)
	b, estimate := requireTokenEstimate(t, s, id, 1)
	if estimate.Status != "partial" || *estimate.AmountMicros != 122000 || *b.Attempts[0].BookedMicros != 122000 {
		t.Fatal(b, estimate)
	}
	// A price change must not rewrite this execution. New executions get version 2.
	price := pricingFixture()
	price.InputPerMillion = priceRate("2")
	price.CacheReadPerMillion = priceRate("0.2")
	price.OutputPerMillion = priceRate("8")
	model.Pricing = price
	model.PricingSet = true
	if e := s.UpdateModelConfig(t.Context(), model.ID, *model); e != nil {
		t.Fatal(e)
	}
	completion := tokenCompletion(t, first, &u)
	if e := s.CompleteContractExecution(t.Context(), completion); e != nil {
		t.Fatal(e)
	}
	if e := s.CompleteContractExecution(t.Context(), completion); e != nil {
		t.Fatal("idempotent completion", e)
	}
	b, estimate = requireTokenEstimate(t, s, id, 1)
	if estimate.Status != "estimated" || estimate.PricingSnapshot.Revision != 1 || *estimate.AmountMicros != 122000 || b.Attempts[0].Source != "configured_pricing" || *b.EstimatedMicros != 122000 {
		t.Fatal(b, estimate)
	}
	archived, e := s.AttemptResult(t.Context(), id, 1)
	if e != nil {
		t.Fatal(e)
	}
	if archived.Result.Cost.Value.EstimatedUSD != nil {
		t.Fatal("native SDK cost was rewritten")
	}
	if _, e = s.Retry(t.Context(), id); e != nil {
		t.Fatal(e)
	}
	second := budgetClaim(t, s, limits)
	if e = s.CompleteContractExecution(t.Context(), tokenCompletion(t, second, &u)); e != nil {
		t.Fatal(e)
	}
	b, estimate = requireTokenEstimate(t, s, id, 2)
	if estimate.PricingSnapshot.Revision != 2 || *estimate.AmountMicros != 244000 || *b.EstimatedMicros != 366000 {
		t.Fatal(b, estimate)
	}
	_, old := requireTokenEstimate(t, s, id, 1)
	if old.PricingSnapshot.Revision != 1 || *old.AmountMicros != 122000 {
		t.Fatal("history repriced", old)
	}
}
func TestConfiguredPriceUnknownsAndForeignCurrency(t *testing.T) {
	for _, name := range []string{"missing-price", "missing-terminal-usage", "foreign-currency", "regressed-total", "unknown-cache-write"} {
		t.Run(name, func(t *testing.T) {
			s := pgStore(t)
			price := pricingFixture()
			if name == "missing-price" {
				price.InputPerMillion = nil
			}
			if name == "foreign-currency" {
				price.Currency = "CNY"
			}
			if name == "unknown-cache-write" {
				price.CacheMode = "cache_read_write"
				price.CacheWritePerMillion = priceRate("2")
			}
			id, limits, _ := pricedTask(t, s, price)
			task := budgetClaim(t, s, limits)
			u := tokenFixture(contracts.ExecutionRef{})
			tokenHeartbeat(t, s, task, 1, u)
			terminal := &u
			if name == "missing-terminal-usage" {
				terminal = nil
			}
			if name == "regressed-total" {
				u.InputTokens = pointer(90000)
				u.TotalTokens = pointer(100000)
			}
			if e := s.CompleteContractExecution(t.Context(), tokenCompletion(t, task, terminal)); e != nil {
				t.Fatal(e)
			}
			b, estimate := requireTokenEstimate(t, s, id, 1)
			if b.Attempts[0].State != "unknown" || len(b.UnknownAttempts) != 1 {
				t.Fatal("unknown USD total became settled", b)
			}
			if name == "foreign-currency" {
				if estimate.Currency == nil || *estimate.Currency != "CNY" || estimate.Status != "estimated" || *estimate.AmountMicros != 122000 || b.Attempts[0].BookedMicros != nil {
					t.Fatal("currency was silently converted", b, estimate)
				}
			} else if estimate.Status == "estimated" {
				t.Fatal("incomplete evidence settled", estimate)
			}
		})
	}
}
func TestModelPricingSQLiteHistorySurvivesReopenAndDelete(t *testing.T) {
	file := filepath.Join(t.TempDir(), "pricing.sqlite")
	s, e := Open(file)
	if e != nil {
		t.Fatal(e)
	}
	model, e := s.CreateModelConfig(t.Context(), ModelConfigRow{Name: "Fixtures / Mock", Provider: "deepseek", Model: "fixture", BaseURL: "https://fixture.invalid", Pricing: pricingFixture()})
	if e != nil {
		t.Fatal(e)
	}
	id, e := s.InsertPending(t.Context(), Task{ScanNo: "fixture-pricing-history", SourcePath: "/fixture", Provider: model.Provider, Model: model.Model, BaseURL: model.BaseURL, ModelConfigID: model.ID})
	if e != nil {
		t.Fatal(e)
	}
	tx, e := s.db.BeginTx(t.Context(), nil)
	if e != nil {
		t.Fatal(e)
	}
	if e = s.freezeAttemptPricing(t.Context(), tx, id, 1); e != nil {
		t.Fatal(e)
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
	model.Pricing.InputPerMillion = priceRate("2")
	if e = s.UpdateModelConfig(t.Context(), model.ID, *model); e != nil {
		t.Fatal(e)
	}
	s.Close()
	s, e = Open(file)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	if e = s.DeleteModelConfig(t.Context(), model.ID); e != nil {
		t.Fatal(e)
	}
	var raw string
	if e = s.db.QueryRowContext(t.Context(), `SELECT pricing_snapshot_json FROM task_token_costs WHERE task_id=? AND attempt=1`, id).Scan(&raw); e != nil {
		t.Fatal(e)
	}
	var snapshot contracts.ModelPricingSnapshot
	if e = json.Unmarshal([]byte(raw), &snapshot); e != nil {
		t.Fatal(e)
	}
	if snapshot.Revision != 1 || string(*snapshot.Pricing.InputPerMillion) != "1" {
		t.Fatal("immutable price changed", snapshot)
	}
}
