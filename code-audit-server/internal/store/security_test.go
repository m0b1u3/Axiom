package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

func securityStoreCases(t *testing.T, run func(*testing.T, *Store)) {
	t.Helper()
	t.Run("sqlite", func(t *testing.T) {
		s, e := Open(filepath.Join(t.TempDir(), "security.db"))
		if e != nil {
			t.Fatal(e)
		}
		t.Cleanup(func() { s.Close() })
		run(t, s)
	})
	t.Run("postgres", func(t *testing.T) { run(t, pgStore(t)) })
}
func newStoreSession(t *testing.T, s *Store, u *User, index int, now time.Time) Session {
	t.Helper()
	a := Session{ID: fmt.Sprintf("fixture-session-%d-%d", u.ID, index), TokenHash: fmt.Sprintf("fixture-hash-%d", index), CreatedAt: securityTime(now), ExpiresAt: securityTime(now.Add(time.Hour))}
	if e := s.StartSession(t.Context(), u, u.PasswordHash, "", a); e != nil {
		t.Fatal(e)
	}
	return a
}
func TestSecuritySessionsAndAccountTransitions(t *testing.T) {
	securityStoreCases(t, func(t *testing.T, s *Store) {
		ctx := t.Context()
		u, e := s.CreateAccount(ctx, "fixture-member", "initial-hash", "operator")
		if e != nil {
			t.Fatal(e)
		}
		now := time.Now().Add(-time.Minute)
		var first, last Session
		for i := 0; i < 12; i++ {
			last = newStoreSession(t, s, u, i, now.Add(time.Duration(i)*time.Second))
			if i == 0 {
				first = last
			}
		}
		rows, e := s.ListSessions(ctx, u.ID, last.ID)
		if e != nil || len(rows) != 10 || !rows[0].Current {
			t.Fatalf("active sessions: %d %v", len(rows), e)
		}
		if _, e = s.SessionUser(ctx, first.ID, first.TokenHash, u.ID, u.AuthVersion, time.Now()); !errors.Is(e, ErrAuthenticationChanged) {
			t.Fatalf("evicted session: %v", e)
		}
		if _, e = s.SessionUser(ctx, last.ID, "wrong-hash", u.ID, u.AuthVersion, time.Now()); !errors.Is(e, ErrAuthenticationChanged) {
			t.Fatalf("hash binding: %v", e)
		}
		if _, e = s.SessionUser(ctx, last.ID, last.TokenHash, u.ID, u.AuthVersion, now.Add(2*time.Hour)); !errors.Is(e, ErrAuthenticationChanged) {
			t.Fatalf("expiry: %v", e)
		}
		if e = s.ChangePassword(ctx, u.ID, "stale-hash", "new-hash"); !errors.Is(e, ErrAuthenticationChanged) {
			t.Fatalf("stale password update: %v", e)
		}
		if e = s.UpdateUserPassword(ctx, u.ID, "new-hash"); e != nil {
			t.Fatal(e)
		}
		if _, e = s.SessionUser(ctx, last.ID, last.TokenHash, u.ID, u.AuthVersion, time.Now()); !errors.Is(e, ErrAuthenticationChanged) {
			t.Fatalf("password revocation: %v", e)
		}
		if e = s.StartSession(ctx, u, u.PasswordHash, "", Session{ID: "late-login"}); !errors.Is(e, ErrAuthenticationChanged) {
			t.Fatalf("late login: %v", e)
		}
		u, e = s.GetUser(ctx, u.ID)
		if e != nil {
			t.Fatal(e)
		}
		last = newStoreSession(t, s, u, 99, time.Now())
		if e = s.UpdateAccount(ctx, u.ID, "viewer", true); e != nil {
			t.Fatal(e)
		}
		if _, e = s.SessionUser(ctx, last.ID, last.TokenHash, u.ID, u.AuthVersion, time.Now()); !errors.Is(e, ErrAuthenticationChanged) {
			t.Fatalf("role revocation: %v", e)
		}
		u, _ = s.GetUser(ctx, u.ID)
		last = newStoreSession(t, s, u, 100, time.Now())
		if e = s.UpdateAccount(ctx, u.ID, "viewer", false); e != nil {
			t.Fatal(e)
		}
		if _, e = s.SessionUser(ctx, last.ID, last.TokenHash, u.ID, u.AuthVersion, time.Now()); !errors.Is(e, ErrAuthenticationChanged) {
			t.Fatalf("disabled revocation: %v", e)
		}
		if _, e = s.CreateAccount(ctx, u.Username, "replacement", "admin"); !errors.Is(e, ErrAccountConflict) {
			t.Fatalf("duplicate overwrite: %v", e)
		}
		if got, _ := s.GetUser(ctx, u.ID); got.PasswordHash != "new-hash" || got.Active {
			t.Fatal("duplicate changed original account")
		}
	})
}
func TestSecurityLoginLimitConcurrency(t *testing.T) {
	securityStoreCases(t, func(t *testing.T, s *Store) {
		// SQLite's production store uses one connection; PostgreSQL exercises row locking.
		now := time.Now()
		var wg sync.WaitGroup
		accepted := make(chan bool, 40)
		errs := make(chan error, 40)
		for i := 0; i < 40; i++ {
			wg.Add(1)
			go func() {
				defer wg.Done()
				ok, _, e := s.ReserveLogin(context.Background(), []string{"fixture-peer", "fixture-account"}, []int{100, 20}, now, 15*time.Minute)
				accepted <- ok
				errs <- e
			}()
		}
		wg.Wait()
		close(accepted)
		close(errs)
		count := 0
		for ok := range accepted {
			if ok {
				count++
			}
		}
		for e := range errs {
			if e != nil {
				t.Fatal(e)
			}
		}
		if count != 20 {
			t.Fatalf("accepted %d, want 20", count)
		}
		if ok, retry, e := s.ReserveLogin(t.Context(), []string{"fixture-peer", "fixture-account"}, []int{100, 20}, now, 15*time.Minute); e != nil || ok || retry < 899 {
			t.Fatalf("retry %v %d %v", ok, retry, e)
		}
		if ok, _, e := s.ReserveLogin(t.Context(), []string{"fixture-peer", "fixture-account"}, []int{100, 20}, now.Add(15*time.Minute), 15*time.Minute); e != nil || !ok {
			t.Fatalf("new window %v %v", ok, e)
		}
	})
}
func TestSecurityLastAdministratorConcurrency(t *testing.T) {
	securityStoreCases(t, func(t *testing.T, s *Store) {
		first, e := s.CreateAccount(t.Context(), "fixture-admin-a", "hash", "admin")
		if e != nil {
			t.Fatal(e)
		}
		second, e := s.CreateAccount(t.Context(), "fixture-admin-b", "hash", "admin")
		if e != nil {
			t.Fatal(e)
		}
		results := make(chan error, 2)
		var wg sync.WaitGroup
		for _, u := range []*User{first, second} {
			wg.Add(1)
			go func(id int64) { defer wg.Done(); results <- s.UpdateAccount(context.Background(), id, "viewer", true) }(u.ID)
		}
		wg.Wait()
		close(results)
		success, conflict := 0, 0
		for e := range results {
			if e == nil {
				success++
			} else if errors.Is(e, ErrAccountConflict) {
				conflict++
			} else {
				t.Fatal(e)
			}
		}
		if success != 1 || conflict != 1 {
			t.Fatalf("success=%d conflict=%d", success, conflict)
		}
	})
}
func TestSecurityAuditPagination(t *testing.T) {
	securityStoreCases(t, func(t *testing.T, s *Store) {
		for i := 0; i < 107; i++ {
			id, e := s.BeginAudit(t.Context(), AuditEntry{Action: "fixture.action", Resource: "fixture", Outcome: "started"})
			if e != nil {
				t.Fatal(e)
			}
			if i > 0 {
				if e = s.FinishAudit(t.Context(), id, "succeeded", fmt.Sprint(i)); e != nil {
					t.Fatal(e)
				}
			}
		}
		first, e := s.ListAudits(t.Context(), "")
		if e != nil || len(first) != 100 {
			t.Fatalf("first %d %v", len(first), e)
		}
		last, e := s.ListAudits(t.Context(), first[99].ID)
		if e != nil || len(last) != 7 {
			t.Fatalf("last %d %v", len(last), e)
		}
		seen := map[string]bool{}
		for _, a := range append(first, last...) {
			if seen[a.ID] {
				t.Fatal("duplicate cursor row")
			}
			seen[a.ID] = true
		}
		if last[6].Outcome != "started" || last[6].CompletedAt != "" {
			t.Fatal("unconfirmed audit misrepresented")
		}
	})
}
func TestSecurityOldSQLiteAccountMigration(t *testing.T) {
	filename := filepath.Join(t.TempDir(), "old.db")
	db, e := sql.Open("sqlite", filename)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(schemaSQL); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec(`INSERT INTO users(id,username,password_hash,created_at) VALUES(42,'historical-user','legacy-hash','2025-01-01T00:00:00Z')`); e != nil {
		t.Fatal(e)
	}
	db.Close()
	s, e := Open(filename)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	u, e := s.GetUser(t.Context(), 42)
	if e != nil || u.Username != "historical-user" || u.PasswordHash != "legacy-hash" || u.Role != "operator" || !u.Active || u.AuthVersion != 0 {
		t.Fatalf("legacy account %+v %v", u, e)
	}
	admin, e := s.EnsureAdministrator(t.Context(), u.Username, "must-not-reset")
	if e != nil || admin.ID != 42 || admin.PasswordHash != "legacy-hash" || admin.Role != "admin" {
		t.Fatalf("bootstrap migration %+v %v", admin, e)
	}
}

func TestSecurityOldPostgresRowsSurviveMigration(t *testing.T) {
	s := pgStore(t)
	ctx := t.Context()
	u, e := s.EnsureUser(ctx, "fixture-pre-session-user", "fixture-legacy-hash")
	if e != nil {
		t.Fatal(e)
	}
	project, e := s.CreateProject(ctx, "Fixture pre-session history", u.ID)
	if e != nil {
		t.Fatal(e)
	}
	id, e := s.InsertPending(ctx, Task{ScanNo: "fixture-pre-session-scan", SourcePath: "/fixture", Provider: "openai", Model: "fixture-model", ProjectID: project.ID})
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Claim(ctx, "fixture-history-agent", time.Minute); e != nil {
		t.Fatal(e)
	}
	raw := json.RawMessage(`{"fixtureOnly":true,"historicalEvidence":"preserve"}`)
	if e = s.ReplaceFindings(ctx, id, []Finding{{ScanID: id, Fingerprint: "fixture-old-finding", Title: "Fixture historical finding", Severity: "low", Raw: raw}}); e != nil {
		t.Fatal(e)
	}
	if e = s.Finish(ctx, id, "completed", "", "", 1); e != nil {
		t.Fatal(e)
	}
	// Reconstruct the exact pre-011 table boundary in the disposable database.
	if _, e = s.db.ExecContext(ctx, `DROP TABLE auth_sessions,login_limits,operation_audits;
 ALTER TABLE users DROP COLUMN role,DROP COLUMN active,DROP COLUMN auth_version;
 DELETE FROM schema_migrations WHERE version='011_authentication.sql'`); e != nil {
		t.Fatal(e)
	}
	s.Close()
	upgraded, e := Open(os.Getenv("AXIOM_TEST_DATABASE_URL"))
	if e != nil {
		t.Fatal(e)
	}
	defer upgraded.Close()
	account, e := upgraded.GetUser(ctx, u.ID)
	if e != nil || account.Username != u.Username || account.PasswordHash != u.PasswordHash || account.CreatedAt != u.CreatedAt || account.Role != "operator" || !account.Active {
		t.Fatalf("account changed during SQL upgrade %+v %v", account, e)
	}
	task, e := upgraded.Get(ctx, id)
	if e != nil || task.Status != "completed" || task.ProjectID != project.ID || task.FindingCount != 1 {
		t.Fatalf("task changed %+v %v", task, e)
	}
	findings, e := upgraded.ListFindings(ctx, id)
	if e != nil || len(findings) != 1 || string(findings[0].Raw) != string(raw) {
		t.Fatalf("historical evidence changed %+v %v", findings, e)
	}
	sessions, e := upgraded.ListSessions(ctx, u.ID, "")
	if e != nil || len(sessions) != 0 {
		t.Fatalf("SQL upgrade fabricated sessions: %+v %v", sessions, e)
	}
}
