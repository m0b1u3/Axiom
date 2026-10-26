package store

import "database/sql"

// SQLite retains its legacy schema path; use the same one-time metadata reset
// as the production PostgreSQL migration without replaying PostgreSQL DDL.
func migrateModelCompatibilitySQLite(db *sql.DB) error {
	tx, err := db.Begin()
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if _, err = tx.Exec(`CREATE TABLE IF NOT EXISTS schema_migrations(version TEXT PRIMARY KEY, applied_at TEXT NOT NULL DEFAULT CURRENT_TIMESTAMP)`); err != nil {
		return err
	}
	const version = "027_model_compatibility.sql"
	var exists int
	if err = tx.QueryRow(`SELECT COUNT(*) FROM schema_migrations WHERE version=?`, version).Scan(&exists); err != nil {
		return err
	}
	if exists == 0 {
		body, e := migrations.ReadFile("migrations/" + version)
		if e != nil {
			return e
		}
		if _, err = tx.Exec(string(body)); err != nil {
			return err
		}
		if _, err = tx.Exec(`INSERT INTO schema_migrations(version) VALUES(?)`, version); err != nil {
			return err
		}
	}
	return tx.Commit()
}
