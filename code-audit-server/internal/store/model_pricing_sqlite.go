package store

import (
	"database/sql"
	"strings"
)

// SQLite has no ALTER CHECK. Copy its small accounting table transactionally,
// preserving every row, before adding the new source label and immutable receipts.
func migrateModelPricingSQLite(db *sql.DB) error {
	for _, c := range [][2]string{{"pricing_json", "TEXT NOT NULL DEFAULT ''"}, {"pricing_revision", "INTEGER NOT NULL DEFAULT 0 CHECK(pricing_revision>=0)"}} {
		if e := ensureColumn(db, "model_configs", c[0], c[1]); e != nil {
			return e
		}
	}
	tx, e := db.Begin()
	if e != nil {
		return e
	}
	defer tx.Rollback()
	var exists int
	if e = tx.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version='028_model_token_pricing.sql'`).Scan(&exists); e != nil {
		return e
	}
	if exists > 0 {
		return tx.Commit()
	}
	ddl := budgetSQL[strings.Index(budgetSQL, "CREATE TABLE IF NOT EXISTS task_cost_attempts"):]
	ddl = strings.Replace(ddl, "CREATE TABLE IF NOT EXISTS task_cost_attempts", "CREATE TABLE task_cost_attempts_pricing", 1)
	if _, e = tx.Exec(ddl); e != nil {
		return e
	}
	if _, e = tx.Exec(`INSERT INTO task_cost_attempts_pricing SELECT * FROM task_cost_attempts; DROP TABLE task_cost_attempts; ALTER TABLE task_cost_attempts_pricing RENAME TO task_cost_attempts;`); e != nil {
		return e
	}
	body, e := migrations.ReadFile("migrations/028_model_token_pricing.sql")
	if e != nil {
		return e
	}
	ddl = string(body)
	ddl = ddl[strings.Index(ddl, "CREATE TABLE task_token_costs"):]
	if _, e = tx.Exec(ddl); e != nil {
		return e
	}
	if _, e = tx.Exec(`INSERT INTO schema_migrations(version) VALUES('028_model_token_pricing.sql')`); e != nil {
		return e
	}
	return tx.Commit()
}
