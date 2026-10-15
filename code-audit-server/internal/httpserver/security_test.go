package httpserver_test

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/httpserver"
	"axiom/code-audit-server/internal/store"
	"database/sql"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func securityFixture(t *testing.T) (*store.Store, *httpserver.Server, *httptest.Server, string) {
	t.Helper()
	s, e := store.Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	t.Cleanup(func() { s.Close() })
	secret := "fixture-auth-signing-key"
	if _, e = s.EnsureAdministrator(t.Context(), "admin", auth.HashPassword(secret, "fixture-admin-password")); e != nil {
		t.Fatal(e)
	}
	server := &httpserver.Server{Store: s, Secret: secret, AgentToken: "fixture-agent-key"}
	api := httptest.NewServer(server.Routes())
	t.Cleanup(api.Close)
	return s, server, api, secret
}
func TestPasswordMigrationSessionsRevocationAndSigningRotation(t *testing.T) {
	s, server, api, secret := securityFixture(t)
	first := login(t, api.URL, "admin", "fixture-admin-password")
	second := login(t, api.URL, "admin", "fixture-admin-password")
	u, e := s.GetUserByName(t.Context(), "admin")
	if e != nil || !strings.HasPrefix(u.PasswordHash, "$argon2id$") {
		t.Fatal("legacy password not migrated", e)
	}
	old, _ := auth.Sign(secret, u.ID, time.Hour)
	for _, token := range []string{old, first} {
		code, _ := getJSON(t, api.URL+"/api/v1/auth/me", token)
		want := 401
		if token == first {
			want = 200
		}
		if code != want {
			t.Fatal("session validation", code, want)
		}
	}
	code, _ := doJSON(t, api.URL+"/api/v1/auth/logout", first, map[string]any{})
	if code != 200 {
		t.Fatal(code)
	}
	if code, _ = getJSON(t, api.URL+"/api/v1/auth/me", first); code != 401 {
		t.Fatal("logout only cleared browser token")
	}
	code, _ = doMethod(t, http.MethodPut, api.URL+"/api/v1/user/password", second, map[string]any{"oldPassword": "fixture-admin-password", "newPassword": "fixture-new-password-123"})
	if code != 200 {
		t.Fatal("password change", code)
	}
	if code, _ = getJSON(t, api.URL+"/api/v1/auth/me", second); code != 401 {
		t.Fatal("password change left old session alive")
	}
	third := login(t, api.URL, "admin", "fixture-new-password-123")
	// A second Server instance shares persistent session state and the same key.
	replica := httptest.NewServer((&httpserver.Server{Store: s, Secret: secret}).Routes())
	defer replica.Close()
	if code, _ = getJSON(t, replica.URL+"/api/v1/auth/me", third); code != 200 {
		t.Fatal("session was process-local")
	}
	rotated := httptest.NewServer((&httpserver.Server{Store: s, Secret: "fixture-rotated-signing-key"}).Routes())
	defer rotated.Close()
	if code, _ = getJSON(t, rotated.URL+"/api/v1/auth/me", third); code != 401 {
		t.Fatal("old signature survived key rotation")
	}
	_ = login(t, rotated.URL, "admin", "fixture-new-password-123")
	_ = server
}
func TestRolesBypassAttemptsSessionOwnershipAndAuditRedaction(t *testing.T) {
	s, _, api, secret := securityFixture(t)
	admin := login(t, api.URL, "admin", "fixture-admin-password")
	for _, role := range []string{"operator", "viewer"} {
		_, e := s.CreateAccount(t.Context(), role, auth.HashPassword(secret, "fixture-user-password"), role)
		if e != nil {
			t.Fatal(e)
		}
	}
	operator := login(t, api.URL, "operator", "fixture-user-password")
	viewer := login(t, api.URL, "viewer", "fixture-user-password")
	for _, v := range []struct {
		method, path, token string
		body                any
		want                int
	}{
		{"GET", "/api/v1/scans", viewer, nil, 200},
		{"POST", "/api/v1/projects", viewer, map[string]any{"name": "forbidden"}, 403},
		{"POST", "/api/v1/projects", operator, map[string]any{"name": "allowed"}, 200},
		{"POST", "/api/v1/model-configs", operator, map[string]any{"name": "forbidden", "apiKey": "fixture-sensitive-key"}, 403},
		{"GET", "/api/v1/users", operator, nil, 403},
		{"POST", "/api/v1/users", viewer, map[string]any{"username": "bypass", "role": "admin", "password": "fixture-sensitive-password"}, 403},
		{"GET", "/api/v1/operations", viewer, nil, 403},
	} {
		code, body := doMethod(t, v.method, api.URL+v.path, v.token, v.body)
		if code != v.want {
			t.Fatal(v.path, code, body)
		}
	}
	code, body := getJSON(t, api.URL+"/api/v1/auth/sessions", admin)
	if code != 200 {
		t.Fatal(code)
	}
	var sessions struct{ Data []store.Session }
	json.Unmarshal(body, &sessions)
	if len(sessions.Data) != 1 {
		t.Fatal(sessions)
	}
	code, _ = doMethod(t, "DELETE", api.URL+"/api/v1/auth/sessions/"+sessions.Data[0].ID, viewer, nil)
	if code != 404 {
		t.Fatal("cross-account session revocation allowed", code)
	}
	target, _ := s.GetUserByName(t.Context(), "operator")
	code, _ = doMethod(t, "PUT", api.URL+"/api/v1/users/"+securityAccountID(target.ID), admin, map[string]any{"role": "viewer", "active": true})
	if code != 200 {
		t.Fatal(code)
	}
	if code, _ = getJSON(t, api.URL+"/api/v1/scans", operator); code != 401 {
		t.Fatal("role change did not revoke sessions")
	}
	code, body = getJSON(t, api.URL+"/api/v1/operations", admin)
	if code != 200 {
		t.Fatal(code)
	}
	for _, private := range []string{"fixture-sensitive-key", "fixture-sensitive-password", admin, viewer, "password_hash", "token_hash"} {
		if strings.Contains(string(body), private) {
			t.Fatal("audit exposed credentials")
		}
	}
	if !strings.Contains(string(body), `"outcome":"denied"`) || !strings.Contains(string(body), `"actorName":"viewer"`) || !strings.Contains(string(body), `"action":"project.create"`) {
		t.Fatal("missing audit evidence", string(body))
	}
}
func TestLoginLimitPersistsAndCannotBeSpoofedByForwardedHeaders(t *testing.T) {
	_, server, api, _ := securityFixture(t)
	for i := 0; i < 21; i++ {
		code, _ := doJSON(t, api.URL+"/api/v1/auth/login", "", map[string]any{"username": "missing-account", "password": "fixture-bad-password"})
		want := 401
		if i == 20 {
			want = 429
		}
		if code != want {
			t.Fatal(i, code, want)
		}
	}
	replica := httptest.NewServer(server.Routes())
	defer replica.Close()
	request, _ := http.NewRequest("POST", replica.URL+"/api/v1/auth/login", strings.NewReader(`{"username":"missing-account","password":"fixture-bad-password"}`))
	request.Header.Set("X-Forwarded-For", "203.0.113.99")
	response, e := http.DefaultClient.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	if response.StatusCode != 429 || response.Header.Get("Retry-After") == "" {
		t.Fatal("limit reset or spoofed", response.StatusCode)
	}
}
func securityAccountID(v int64) string { b, _ := json.Marshal(v); return string(b) }

func TestSecurityViewerCannotMutateOrAccessAdministration(t *testing.T) {
	s, _, api, secret := securityFixture(t)
	if _, e := s.CreateAccount(t.Context(), "fixture-viewer", auth.HashPassword(secret, "fixture-password"), "viewer"); e != nil {
		t.Fatal(e)
	}
	token := login(t, api.URL, "fixture-viewer", "fixture-password")
	for _, c := range []struct{ method, path string }{
		{"PUT", "/storage/policy"}, {"POST", "/storage/cleanup"}, {"POST", "/storage/cleanup/fixture/execute"}, {"POST", "/storage/cleanup/fixture/cancel"}, {"GET", "/storage/cleanup"}, {"POST", "/projects"}, {"DELETE", "/projects/1"}, {"POST", "/scans"}, {"DELETE", "/scans/1"}, {"POST", "/scans/1/cancel"}, {"POST", "/scans/1/retry"},
		{"PUT", "/scans/1/budget"}, {"PUT", "/scans/1/budget/attempts/1"},
		{"POST", "/uploads"}, {"DELETE", "/snapshots/1"}, {"PUT", "/snapshots/1/project"}, {"PUT", "/findings/1/status"}, {"PUT", "/settings/execution"},
		{"POST", "/model-configs"}, {"PUT", "/model-configs/1"}, {"DELETE", "/model-configs/1"}, {"PUT", "/model-configs/1/enable"}, {"POST", "/model-configs/1/test"}, {"POST", "/model-configs/fetch-models"},
		{"GET", "/users"}, {"POST", "/users"}, {"PUT", "/users/1"}, {"GET", "/operations"},
	} {
		t.Run(c.method+c.path, func(t *testing.T) {
			code, _ := doMethod(t, c.method, api.URL+"/api/v1"+c.path, token, map[string]any{"role": "admin", "password": "do-not-log-this-password"})
			if code != 403 {
				t.Fatalf("bypass %d", code)
			}
		})
	}
}

func TestSecurityAuditUnavailablePreventsMutation(t *testing.T) {
	path := filepath.Join(t.TempDir(), "audit-unavailable.db")
	s, e := store.Open(path)
	if e != nil {
		t.Fatal(e)
	}
	defer s.Close()
	secret := "fixture-audit-unavailable"
	if _, e = s.EnsureAdministrator(t.Context(), "admin", auth.HashPassword(secret, "fixture-password")); e != nil {
		t.Fatal(e)
	}
	server := httptest.NewServer((&httpserver.Server{Store: s, Secret: secret}).Routes())
	defer server.Close()
	token := login(t, server.URL, "admin", "fixture-password")
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	if _, e = db.Exec(`DROP TABLE operation_audits`); e != nil {
		t.Fatal(e)
	}
	code, _ := doJSON(t, server.URL+"/api/v1/projects", token, map[string]any{"name": "must-not-be-created"})
	if code != 503 {
		t.Fatalf("missing audit store: %d", code)
	}
	projects, e := s.ListProjects(t.Context())
	if e != nil || len(projects) != 0 {
		t.Fatalf("unlogged mutation committed: %v %v", projects, e)
	}
}

func TestSecurityDatabaseOutageIsUnavailable(t *testing.T) {
	s, _, api, _ := securityFixture(t)
	token := login(t, api.URL, "admin", "fixture-admin-password")
	s.Close()
	if code, _ := getJSON(t, api.URL+"/api/v1/auth/me", token); code != 503 {
		t.Fatalf("DB outage misreported as login expiry: %d", code)
	}
	if code, _ := doJSON(t, api.URL+"/api/v1/auth/login", "", map[string]any{"username": "admin", "password": "fixture-admin-password"}); code != 503 {
		t.Fatalf("DB outage misreported as invalid password: %d", code)
	}
}

func TestSecurityLegacyPasswordSecretRotation(t *testing.T) {
	s, _, _, oldSecret := securityFixture(t)
	if _, e := s.CreateAccount(t.Context(), "fixture-unmigrated", auth.HashPassword(oldSecret, "fixture-legacy-password"), "operator"); e != nil {
		t.Fatal(e)
	}
	rotated := httptest.NewServer((&httpserver.Server{Store: s, Secret: "fixture-new-signing-key", LegacyPasswordSecret: oldSecret}).Routes())
	defer rotated.Close()
	token := login(t, rotated.URL, "fixture-unmigrated", "fixture-legacy-password")
	if code, _ := getJSON(t, rotated.URL+"/api/v1/auth/me", token); code != 200 {
		t.Fatal(code)
	}
	withoutLegacy := httptest.NewServer((&httpserver.Server{Store: s, Secret: "fixture-another-signing-key"}).Routes())
	defer withoutLegacy.Close()
	_ = login(t, withoutLegacy.URL, "fixture-unmigrated", "fixture-legacy-password")
}

func doMethod(t *testing.T, method, url, token string, payload any) (int, string) {
	t.Helper()
	var body *strings.Reader
	if payload == nil {
		body = strings.NewReader("")
	} else {
		raw, e := json.Marshal(payload)
		if e != nil {
			t.Fatal(e)
		}
		body = strings.NewReader(string(raw))
	}
	request, e := http.NewRequest(method, url, body)
	if e != nil {
		t.Fatal(e)
	}
	request.Header.Set("Content-Type", "application/json")
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}
	response, e := http.DefaultClient.Do(request)
	if e != nil {
		t.Fatal(e)
	}
	defer response.Body.Close()
	raw, e := io.ReadAll(response.Body)
	if e != nil {
		t.Fatal(e)
	}
	return response.StatusCode, string(raw)
}
