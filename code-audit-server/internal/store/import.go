package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// ImportSQLite copies a consistent read snapshot into an empty PostgreSQL database.
// It preserves ids, timestamps and encrypted keys. It never writes to SQLite.
func (s *Store) ImportSQLite(ctx context.Context, source string) (map[string]int, error) {
	if !s.db.postgres {
		return nil, fmt.Errorf("import target must be PostgreSQL")
	}
	from, err := sql.Open("sqlite", "file:"+source+"?mode=ro")
	if err != nil {
		return nil, err
	}
	defer from.Close()
	read, err := from.BeginTx(ctx, &sql.TxOptions{ReadOnly: true})
	if err != nil {
		return nil, err
	}
	defer read.Rollback()
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer tx.Rollback()
	tables := []string{"users", "projects", "source_snapshots", "model_configs", "scan_tasks", "findings"}
	// Preserve opt-in budget metadata from newer SQLite sources. Old sources
	// have no such tables and retain their historical budget semantics.
	for _, name := range []string{"task_budgets", "task_cost_attempts", "storage_settings", "storage_cleanup_jobs"} {
		var exists int
		if err = read.QueryRowContext(ctx, `SELECT COUNT(*) FROM sqlite_master WHERE type='table' AND name=?`, name).Scan(&exists); err != nil {
			return nil, err
		}
		if exists > 0 {
			tables = append(tables, name)
		}
	}
	counts := map[string]int{}
	for _, table := range tables {
		var n int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil {
			return nil, err
		}
		if n > 0 {
			return nil, fmt.Errorf("target %s is not empty", table)
		}
	}
	for _, table := range tables {
		order := "id"
		if table == "task_budgets" {
			order = "task_id"
		}
		if table == "task_cost_attempts" {
			order = "task_id,attempt"
		}
		rows, err := read.QueryContext(ctx, "SELECT * FROM "+table+" ORDER BY "+order)
		if err != nil {
			return nil, err
		}
		cols, err := rows.Columns()
		if err != nil {
			rows.Close()
			return nil, err
		}
		names := make([]string, len(cols))
		markers := make([]string, len(cols))
		for i, c := range cols {
			names[i] = `"` + strings.ReplaceAll(c, `"`, `""`) + `"`
			markers[i] = "?"
		}
		query := "INSERT INTO " + table + " (" + strings.Join(names, ",") + ") VALUES (" + strings.Join(markers, ",") + ")"
		for rows.Next() {
			values := make([]any, len(cols))
			dest := make([]any, len(cols))
			for i := range values {
				dest[i] = &values[i]
			}
			if err = rows.Scan(dest...); err != nil {
				rows.Close()
				return nil, err
			}
			for i, v := range values {
				if b, ok := v.([]byte); ok {
					values[i] = string(b)
				}
			}
			if _, err = tx.ExecContext(ctx, query, values...); err != nil {
				rows.Close()
				return nil, fmt.Errorf("import %s row %d: %w", table, counts[table]+1, err)
			}
			counts[table]++
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		var n int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM "+table).Scan(&n); err != nil || n != counts[table] {
			return nil, fmt.Errorf("count verification failed for %s", table)
		}
		if table == "task_budgets" || table == "task_cost_attempts" || table == "storage_settings" || table == "storage_cleanup_jobs" {
			continue
		}
		if _, err = tx.ExecContext(ctx, `SELECT setval(pg_get_serial_sequence(?, 'id'),COALESCE((SELECT MAX(id) FROM `+table+`),1),(SELECT COUNT(*)>0 FROM `+table+`))`, table); err != nil {
			return nil, err
		}
	}
	if _, err = tx.ExecContext(ctx, `INSERT INTO scan_attempts(task_id,attempt,agent_id,status,lease_expires_at,started_at,finished_at,output_path,progress_json,error_code,error_message) SELECT id,GREATEST(attempt,1),COALESCE(agent_id,'legacy'),CASE WHEN status='cancel_requested' THEN 'scanning' ELSE status END,lease_expires_at,COALESCE(started_at,created_at),finished_at,COALESCE(output_path,''),COALESCE(progress_json,''),COALESCE(error_code,''),COALESCE(error_message,'') FROM scan_tasks WHERE status<>'pending' ON CONFLICT DO NOTHING`); err != nil {
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `UPDATE scan_tasks t SET attempt=a.latest FROM (SELECT task_id,MAX(attempt) AS latest FROM scan_attempts GROUP BY task_id) a WHERE t.id=a.task_id AND t.attempt<a.latest`); err != nil {
		return nil, err
	}
	if err = tx.Commit(); err != nil {
		return nil, err
	}
	return counts, nil
}
