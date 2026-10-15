package store

import (
	"context"
	"database/sql"
	"embed"
	"fmt"
	_ "github.com/jackc/pgx/v5/stdlib"
	"time"
)

//go:embed migrations/*.sql
var migrations embed.FS

func openPostgres(dsn string) (*Store, error) {
	db, err := sql.Open("pgx", dsn)
	if err != nil {
		return nil, err
	}
	db.SetMaxOpenConns(20)
	db.SetMaxIdleConns(5)
	db.SetConnMaxLifetime(30 * time.Minute)
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	if err = db.PingContext(ctx); err != nil {
		db.Close()
		return nil, fmt.Errorf("connect PostgreSQL: %w", err)
	}
	tx, err := db.BeginTx(ctx, nil)
	if err != nil {
		db.Close()
		return nil, err
	}
	defer tx.Rollback()
	if _, err = tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(740918)`); err != nil {
		db.Close()
		return nil, err
	}
	if _, err = tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS schema_migrations(version TEXT PRIMARY KEY, applied_at TIMESTAMPTZ NOT NULL DEFAULT NOW())`); err != nil {
		db.Close()
		return nil, err
	}
	entries, _ := migrations.ReadDir("migrations")
	for _, entry := range entries {
		var exists bool
		if err = tx.QueryRowContext(ctx, `SELECT EXISTS(SELECT 1 FROM schema_migrations WHERE version=$1)`, entry.Name()).Scan(&exists); err != nil {
			db.Close()
			return nil, err
		}
		if exists {
			continue
		}
		body, _ := migrations.ReadFile("migrations/" + entry.Name())
		if _, err = tx.ExecContext(ctx, string(body)); err != nil {
			db.Close()
			return nil, fmt.Errorf("migration %s: %w", entry.Name(), err)
		}
		if _, err = tx.ExecContext(ctx, `INSERT INTO schema_migrations(version) VALUES($1)`, entry.Name()); err != nil {
			db.Close()
			return nil, err
		}
	}
	if err = tx.Commit(); err != nil {
		db.Close()
		return nil, err
	}
	gate, e := sql.Open("pgx", dsn)
	if e != nil {
		db.Close()
		return nil, e
	}
	gate.SetMaxOpenConns(16)
	gate.SetMaxIdleConns(4)
	return &Store{db: &database{DB: db, postgres: true}, storageGateDB: gate}, nil
}
func boolInt(v bool) int {
	if v {
		return 1
	}
	return 0
}
func (s *Store) IsPostgres() bool { return s.db.postgres }
