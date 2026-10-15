package store

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
)

// database keeps parameter binding at the driver boundary; queries remain parameterized.
type database struct {
	*sql.DB
	postgres bool
}
type transaction struct {
	*sql.Tx
	postgres bool
}

func bind(query string, pg bool) string {
	if !pg {
		return query
	}
	var b strings.Builder
	n := 0
	quote := false
	for i := 0; i < len(query); i++ {
		ch := query[i]
		if ch == '\'' {
			b.WriteByte(ch)
			if quote && i+1 < len(query) && query[i+1] == '\'' {
				i++
				b.WriteByte('\'')
				continue
			}
			quote = !quote
			continue
		}
		if ch == '?' && !quote {
			n++
			fmt.Fprintf(&b, "$%d", n)
		} else {
			b.WriteByte(ch)
		}
	}
	return b.String()
}
func (d *database) ExecContext(c context.Context, q string, a ...any) (sql.Result, error) {
	return d.DB.ExecContext(c, bind(q, d.postgres), a...)
}
func (d *database) QueryContext(c context.Context, q string, a ...any) (*sql.Rows, error) {
	return d.DB.QueryContext(c, bind(q, d.postgres), a...)
}
func (d *database) QueryRowContext(c context.Context, q string, a ...any) *sql.Row {
	return d.DB.QueryRowContext(c, bind(q, d.postgres), a...)
}
func (d *database) BeginTx(c context.Context, o *sql.TxOptions) (*transaction, error) {
	tx, e := d.DB.BeginTx(c, o)
	if e != nil {
		return nil, e
	}
	return &transaction{tx, d.postgres}, nil
}
func (t *transaction) ExecContext(c context.Context, q string, a ...any) (sql.Result, error) {
	return t.Tx.ExecContext(c, bind(q, t.postgres), a...)
}
func (t *transaction) QueryRowContext(c context.Context, q string, a ...any) *sql.Row {
	return t.Tx.QueryRowContext(c, bind(q, t.postgres), a...)
}
func (t *transaction) QueryContext(c context.Context, q string, a ...any) (*sql.Rows, error) {
	return t.Tx.QueryContext(c, bind(q, t.postgres), a...)
}
func (t *transaction) PrepareContext(c context.Context, q string) (*sql.Stmt, error) {
	return t.Tx.PrepareContext(c, bind(q, t.postgres))
}
func (s *Store) elapsedSQL() string {
	if s.db.postgres {
		return "EXTRACT(EPOCH FROM (CAST(finished_at AS timestamptz) - CAST(started_at AS timestamptz)))"
	}
	return "(julianday(finished_at) - julianday(started_at)) * 86400"
}
