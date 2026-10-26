package httpserver

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"context"
	"crypto/hmac"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"log"
	"net"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"
)

type securityState struct {
	once      sync.Once
	dummy     string
	initErr   error
	hashSlots chan struct{}
}
type claimsKey struct{}
type routePolicy struct{ permission, action, resource string }

var routePolicies = map[string]routePolicy{
	"GET /api/v1/diagnostics/structured-output":           {"read", "", ""},
	"GET /api/v1/diagnostics/structured-output/records":   {"admin", "", ""},
	"POST /api/v1/diagnostics/structured-output/records":  {"admin", "diagnostic.offline", "diagnostic"},
	"GET /api/v1/sdk-identities":                          {"read", "", ""},
	"POST /api/v1/sdk-identities":                         {"admin", "sdk-identity.create", "sdk-identity"},
	"GET /api/v1/sdk-identities/{id}/operations":          {"admin", "", ""},
	"POST /api/v1/sdk-identities/{id}/operations":         {"admin", "sdk-identity.operation", "sdk-identity"},
	"POST /api/v1/sdk-identity-operations/{id}/cancel":    {"admin", "sdk-identity.cancel", "sdk-identity"},
	"POST /api/v1/component-plans/{id}/scans":             {"operate", "component-scan.create", "component-plan"},
	"GET /api/v1/scans/{id}/component-executions":         {"read", "", ""},
	"POST /api/v1/scans/{id}/component-executions/retry":  {"operate", "component-scan.retry", "scan"},
	"GET /api/v1/component-plans":                         {"read", "", ""},
	"POST /api/v1/component-plans":                        {"operate", "component-plan.create", "component-plan"},
	"GET /api/v1/component-plans/{id}":                    {"read", "", ""},
	"PUT /api/v1/component-plans/{id}":                    {"operate", "component-plan.edit", "component-plan"},
	"POST /api/v1/component-plans/{id}/operations":        {"operate", "component-plan.operation", "component-plan"},
	"POST /api/v1/component-plans/{id}/apply-suggestion":  {"operate", "component-plan.apply", "component-plan"},
	"POST /api/v1/component-plans/{id}/freeze":            {"operate", "component-plan.freeze", "component-plan"},
	"GET /api/v1/component-plans/{id}/versions":           {"read", "", ""},
	"GET /api/v1/component-plans/{id}/versions/{version}": {"read", "", ""},

	"GET /api/v1/snapshots/{id}/git-diff":                          {"read", "", ""},
	"GET /api/v1/snapshots/{id}/git-diff/files/{file}":             {"read", "", ""},
	"GET /api/v1/scans/{id}/deep-resume":                           {"read", "", ""},
	"POST /api/v1/scans/{id}/deep-resume/check":                    {"operate", "scan.resume_check", "scan"},
	"POST /api/v1/scans/{id}/deep-resume":                          {"operate", "scan.resume_deep", "scan"},
	"GET /api/v1/scans/{id}/recovery":                              {"read", "", ""},
	"POST /api/v1/scans/{id}/recovery":                             {"operate", "scan.recovery", "scan"},
	"POST /api/v1/scans/{id}/interim-reviews":                      {"operate", "interim-review.create", "scan"},
	"GET /api/v1/scans/{id}/attempts/{attempt}/interim-reviews":    {"read", "", ""},
	"GET /api/v1/interim-review-groups/{group}":                    {"read", "", ""},
	"POST /api/v1/interim-review-groups/{group}/retry":             {"operate", "interim-review.retry", "interim-review"},
	"POST /api/v1/interim-review-groups/{group}/archive":           {"operate", "interim-review.archive", "interim-review"},
	"GET /api/v1/findings/interim":                                 {"read", "", ""},
	"GET /api/v1/scans/{id}/attempts/{attempt}/interim":            {"read", "", ""},
	"GET /api/v1/scans/{id}/attempts/{attempt}/interim/{artifact}": {"read", "", ""},
	"GET /api/v1/scans/{id}/attempts/{attempt}/sdk-sessions":       {"read", "", ""},
	"POST /api/v1/scans/{id}/sdk-operations":                       {"operate", "sdk.operation.create", "scan"},
	"GET /api/v1/scans/{id}/sdk-operations":                        {"read", "", ""},
	"GET /api/v1/scans/{id}/parent-state":                          {"read", "", ""},
	"GET /api/v1/knowledge":                                        {"read", "", ""},
	"GET /api/v1/knowledge/versions/{id}":                          {"read", "", ""},
	"POST /api/v1/knowledge":                                       {"operate", "knowledge.create", "knowledge"},
	"POST /api/v1/knowledge/{id}/versions":                         {"operate", "knowledge.version", "knowledge"},
	"PUT /api/v1/knowledge/{id}/archive":                           {"operate", "knowledge.archive", "knowledge"},
	"POST /api/v1/storage/cleanup/{job}/cancel":                    {"admin", "storage.cancel", "storage"},
	"GET /api/v1/storage":                                          {"read", "", ""},
	"PUT /api/v1/storage/policy":                                   {"admin", "storage.policy", "storage"},
	"GET /api/v1/storage/cleanup":                                  {"admin", "", ""},
	"POST /api/v1/storage/cleanup":                                 {"admin", "storage.preview", "storage"},
	"GET /api/v1/storage/cleanup/{job}":                            {"admin", "", ""},
	"POST /api/v1/storage/cleanup/{job}/execute":                   {"admin", "storage.execute", "storage"},
	"GET /api/v1/auth/me":                                          {"read", "", ""},
	"POST /api/v1/auth/logout":                                     {"read", "session.logout", "session"},
	"GET /api/v1/auth/sessions":                                    {"read", "", ""},
	"DELETE /api/v1/auth/sessions/{sessionId}":                     {"read", "session.revoke", "session"},
	"PUT /api/v1/user/password":                                    {"read", "user.password", "account"},
	"GET /api/v1/users":                                            {"admin", "", "account"},
	"POST /api/v1/users":                                           {"admin", "user.create", "account"},
	"PUT /api/v1/users/{id}":                                       {"admin", "user.update", "account"},
	"GET /api/v1/operations":                                       {"admin", "", "operation"},
	"GET /api/v1/executions/overview":                              {"read", "", ""},
	"GET /api/v1/settings/execution":                               {"read", "", ""},
	"PUT /api/v1/settings/execution":                               {"admin", "execution.settings", "execution"},
	"GET /api/v1/projects":                                         {"read", "", ""},
	"GET /api/v1/projects/{id}":                                    {"read", "", ""},
	"POST /api/v1/projects":                                        {"operate", "project.create", "project"},
	"DELETE /api/v1/projects/{id}":                                 {"admin", "project.delete", "project"},
	"POST /api/v1/scans":                                           {"operate", "scan.create", "scan"},
	"GET /api/v1/scans":                                            {"read", "", ""},
	"GET /api/v1/scans/{id}":                                       {"read", "", ""},
	"GET /api/v1/scans/{id}/budget":                                {"read", "", ""},
	"PUT /api/v1/scans/{id}/budget":                                {"operate", "budget.update", "scan"},
	"PUT /api/v1/scans/{id}/budget/attempts/{attempt}":             {"operate", "cost.reconcile", "scan"},
	"GET /api/v1/scans/{id}/findings":                              {"read", "", ""},
	"GET /api/v1/scans/{id}/logs":                                  {"read", "", ""},
	"GET /api/v1/scans/{id}/checkpoints":                           {"read", "checkpoint.download", "scan"},
	"GET /api/v1/scans/{id}/report":                                {"read", "report.html", "scan"},
	"GET /api/v1/scans/{id}/report.pdf":                            {"read", "report.pdf", "scan"},
	"POST /api/v1/scans/{id}/cancel":                               {"operate", "scan.cancel", "scan"},
	"POST /api/v1/scans/{id}/retry":                                {"operate", "scan.retry", "scan"},
	"DELETE /api/v1/scans/{id}":                                    {"admin", "scan.delete", "scan"},
	"GET /api/v1/scans/{id}/coverage":                              {"read", "", ""},
	"GET /api/v1/scans/{id}/attempts":                              {"read", "", ""},
	"GET /api/v1/scans/{id}/attempts/{attempt}/result":             {"read", "", ""},
	"GET /api/v1/scans/{id}/attempts/{attempt}/evidence-graph":     {"read", "", ""},
	"GET /api/v1/scans/{id}/attempts/{attempt}/artifacts":          {"read", "artifact.download", "scan"},
	"GET /api/v1/scans/{id}/attempts/{attempt}/sdk-cost-estimate":  {"read", "", ""},
	"GET /api/v1/scans/{id}/attempts/{attempt}/sdk-history-links":  {"read", "", ""},
	"GET /api/v1/scans/{id}/compare":                               {"read", "", ""},
	"POST /api/v1/git-snapshots":                                   {"operate", "snapshot.upload", "snapshot"},
	"POST /api/v1/uploads":                                         {"operate", "snapshot.upload", "snapshot"},
	"GET /api/v1/snapshots":                                        {"read", "", ""},
	"DELETE /api/v1/snapshots/{id}":                                {"admin", "snapshot.delete", "snapshot"},
	"PUT /api/v1/snapshots/{id}/project":                           {"operate", "snapshot.project", "snapshot"},
	"PUT /api/v1/findings/{id}/status":                             {"operate", "finding.status", "finding"},
	"GET /api/v1/findings":                                         {"read", "", ""},
	"GET /api/v1/dashboard":                                        {"read", "", ""},
	"GET /api/v1/model-configs":                                    {"read", "", ""},
	"GET /api/v1/model-providers":                                  {"read", "", ""},
	"POST /api/v1/model-configs":                                   {"admin", "model.create", "model"},
	"POST /api/v1/model-configs/fetch-models":                      {"admin", "model.fetch", "model"},
	"PUT /api/v1/model-configs/{id}":                               {"admin", "model.update", "model"},
	"PUT /api/v1/model-configs/{id}/enable":                        {"admin", "model.enable", "model"},
	"DELETE /api/v1/model-configs/{id}":                            {"admin", "model.delete", "model"},
	"POST /api/v1/model-configs/{id}/test":                         {"admin", "model.test", "model"},
}

func allowed(role, permission string) bool {
	if !store.ValidRole(role) {
		return false
	}
	switch permission {
	case "read":
		return true
	case "operate":
		return role == "operator" || role == "admin"
	case "admin":
		return role == "admin"
	}
	return false
}
func permissions(role string) []string {
	out := []string{}
	for _, p := range []string{"read", "operate", "admin"} {
		if allowed(role, p) {
			out = append(out, p)
		}
	}
	return out
}
func publicUser(u *store.User) map[string]any {
	return map[string]any{"id": u.ID, "username": u.Username, "role": u.Role, "active": u.Active, "permissions": permissions(u.Role)}
}
func (s *Server) initSecurity() error {
	s.security.once.Do(func() {
		s.security.hashSlots = make(chan struct{}, 4)
		s.security.dummy, s.security.initErr = auth.NewPasswordHash("server-only-login-timing-padding")
	})
	return s.security.initErr
}
func (s *Server) hashSlot(w http.ResponseWriter) (func(), bool) {
	if s.initSecurity() != nil {
		httputil.Error(w, 503, 50300, "账户认证暂不可用")
		return nil, false
	}
	select {
	case s.security.hashSlots <- struct{}{}:
		return func() { <-s.security.hashSlots }, true
	default:
		w.Header().Set("Retry-After", "5")
		httputil.Error(w, 429, 42900, "登录或密码操作过于频繁，请稍后重试")
		return nil, false
	}
}
func smallJSON(w http.ResponseWriter, r *http.Request, out any) bool {
	defer r.Body.Close()
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 4096))
	d.DisallowUnknownFields()
	if d.Decode(out) != nil {
		httputil.Error(w, 400, 40000, "账户请求格式无效")
		return false
	}
	var v any
	if d.Decode(&v) != io.EOF {
		httputil.Error(w, 400, 40000, "账户请求格式无效")
		return false
	}
	return true
}
func (s *Server) bucket(value string) string {
	mac := hmac.New(sha256.New, []byte(s.Secret))
	mac.Write([]byte(value))
	return hex.EncodeToString(mac.Sum(nil))
}
func peerAddress(r *http.Request) string {
	ip, _, e := net.SplitHostPort(r.RemoteAddr)
	if e != nil {
		ip = r.RemoteAddr
	}
	if net.ParseIP(ip) == nil {
		return "unknown-peer"
	}
	return ip
}
func (s *Server) login(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Cache-Control", "no-store")
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !smallJSON(w, r, &body) {
		return
	}
	if body.Username == "" || len(body.Username) > 128 || len(body.Password) == 0 || len(body.Password) > auth.MaxPasswordBytes {
		httputil.Error(w, 400, 40000, "请输入有效的用户名和密码")
		return
	}
	peer, account := s.bucket("peer:"+peerAddress(r)), s.bucket("account:"+body.Username)
	ok, retry, e := s.Store.ReserveLogin(r.Context(), []string{peer, account}, []int{100, 20}, time.Now(), 15*time.Minute)
	if e != nil {
		httputil.Error(w, 503, 50300, "账户认证暂不可用")
		return
	}
	if !ok {
		w.Header().Set("Retry-After", strconv.Itoa(retry))
		httputil.Error(w, 429, 42900, "登录尝试过多，请稍后重试")
		return
	}
	release, ok := s.hashSlot(w)
	if !ok {
		return
	}
	defer release()
	u, e := s.Store.GetUserByName(r.Context(), body.Username)
	if e != nil { // Database errors must not be disguised as invalid credentials.
		if !errors.Is(e, sql.ErrNoRows) {
			httputil.Error(w, 503, 50300, "账户认证暂不可用")
			return
		}
	}
	encoded := s.security.dummy
	if u != nil {
		encoded = u.PasswordHash
	}
	legacy := s.LegacyPasswordSecret
	if legacy == "" {
		legacy = s.Secret
	}
	valid, upgrade := auth.VerifyPassword(encoded, body.Password, legacy)
	if u != nil && !strings.HasPrefix(encoded, "$") {
		auth.VerifyPassword(s.security.dummy, body.Password, "")
	}
	if !valid || u == nil || !u.Active {
		id, err := s.Store.BeginAudit(r.Context(), store.AuditEntry{Action: "session.login", Resource: "session", Outcome: "denied"})
		if err != nil {
			httputil.Error(w, 503, 50300, "账户认证暂不可用")
			return
		}
		_ = s.Store.FinishAudit(r.Context(), id, "denied", "")
		httputil.Error(w, 401, 40100, "用户名或密码无效")
		return
	}
	newHash := ""
	if upgrade {
		newHash, e = auth.NewPasswordHash(body.Password)
		if e != nil {
			httputil.Error(w, 503, 50300, "账户认证暂不可用")
			return
		}
	}
	sid, e := auth.NewSessionID()
	if e != nil {
		httputil.Error(w, 503, 50300, "账户认证暂不可用")
		return
	}
	now := time.Now()
	expires := now.Add(24 * time.Hour).Truncate(time.Second)
	token, e := auth.SessionToken(s.Secret, auth.SessionClaims{Version: 2, UserID: u.ID, SessionID: sid, AuthVersion: u.AuthVersion, ExpiresAt: expires.Unix()})
	if e != nil {
		httputil.Error(w, 503, 50300, "账户认证暂不可用")
		return
	}
	e = s.Store.StartSession(r.Context(), u, u.PasswordHash, newHash, store.Session{ID: sid, TokenHash: auth.TokenHash(token), CreatedAt: now.UTC().Format("2006-01-02T15:04:05.000000Z"), ExpiresAt: expires.UTC().Format("2006-01-02T15:04:05.000000Z")})
	if e != nil {
		if errors.Is(e, store.ErrAuthenticationChanged) {
			httputil.Error(w, 401, 40100, "账户状态已变化，请重新登录")
		} else {
			httputil.Error(w, 503, 50300, "登录会话暂不可用")
		}
		return
	}
	_ = s.Store.ClearLoginBucket(r.Context(), account)
	httputil.OK(w, map[string]any{"token": token, "user": publicUser(u)})
}
func (s *Server) me(w http.ResponseWriter, r *http.Request, u *store.User) {
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, publicUser(u))
}
func (s *Server) changePassword(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		OldPassword string `json:"oldPassword"`
		NewPassword string `json:"newPassword"`
	}
	if !smallJSON(w, r, &body) {
		return
	}
	if !auth.ValidNewPassword(body.NewPassword) {
		httputil.Error(w, 400, 40001, "新密码至少 12 个字符，最多 1024 字节")
		return
	}
	release, ok := s.hashSlot(w)
	if !ok {
		return
	}
	defer release()
	legacy := s.LegacyPasswordSecret
	if legacy == "" {
		legacy = s.Secret
	}
	valid, _ := auth.VerifyPassword(u.PasswordHash, body.OldPassword, legacy)
	if !valid {
		httputil.Error(w, 400, 40002, "原密码无效")
		return
	}
	if body.NewPassword == body.OldPassword {
		httputil.Error(w, 400, 40003, "新密码不能与原密码相同")
		return
	}
	hash, e := auth.NewPasswordHash(body.NewPassword)
	if e != nil {
		httputil.Error(w, 503, 50300, "密码更新暂不可用")
		return
	}
	if e = s.Store.ChangePassword(r.Context(), u.ID, u.PasswordHash, hash); e != nil {
		httputil.Error(w, 409, 40900, "账户状态已变化，请重新登录后重试")
		return
	}
	httputil.OK(w, map[string]any{"id": u.ID, "sessionsRevoked": true})
}
func (s *Server) logout(w http.ResponseWriter, r *http.Request, u *store.User) {
	c := r.Context().Value(claimsKey{}).(auth.SessionClaims)
	if e := s.Store.RevokeSession(r.Context(), u.ID, c.SessionID); e != nil {
		httputil.Error(w, 503, 50300, "会话撤销失败，请重试退出")
		return
	}
	httputil.OK(w, map[string]bool{"revoked": true})
}
func (s *Server) sessions(w http.ResponseWriter, r *http.Request, u *store.User) {
	c := r.Context().Value(claimsKey{}).(auth.SessionClaims)
	v, e := s.Store.ListSessions(r.Context(), u.ID, c.SessionID)
	if e != nil {
		httputil.Error(w, 503, 50300, "登录会话暂不可用")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, v)
}
func (s *Server) revokeSession(w http.ResponseWriter, r *http.Request, u *store.User) {
	id := r.PathValue("sessionId")
	if e := s.Store.RevokeSession(r.Context(), u.ID, id); e != nil {
		if errors.Is(e, store.ErrAuthenticationChanged) {
			httputil.Error(w, 404, 40400, "该登录会话不存在或已撤销")
		} else {
			httputil.Error(w, 503, 50300, "会话撤销暂不可用，请重试")
		}
		return
	}
	httputil.OK(w, map[string]bool{"revoked": true})
}

// The wrapper captures at most an ID-bearing JSON prefix in memory. Request
// bodies, tokens, password hashes, API keys, query strings and errors are never logged.
type auditWriter struct {
	http.ResponseWriter
	status  int
	written bool
	prefix  []byte
}

func (w *auditWriter) WriteHeader(code int) {
	if w.written {
		return
	}
	w.written = true
	w.status = code
	w.ResponseWriter.WriteHeader(code)
}
func (w *auditWriter) Write(p []byte) (int, error) {
	if !w.written {
		w.WriteHeader(200)
	}
	if len(w.prefix) < 8192 {
		n := min(len(p), 8192-len(w.prefix))
		w.prefix = append(w.prefix, p[:n]...)
	}
	return w.ResponseWriter.Write(p)
}
func (s *Server) withAuth(next func(http.ResponseWriter, *http.Request, *store.User)) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		c, e := auth.ParseSession(s.Secret, auth.Bearer(r), time.Now())
		if e != nil {
			httputil.Error(w, 401, 40100, "登录已失效，请重新登录")
			return
		}
		u, e := s.Store.SessionUser(r.Context(), c.SessionID, auth.TokenHash(auth.Bearer(r)), c.UserID, c.AuthVersion, time.Now())
		if e != nil {
			if errors.Is(e, store.ErrAuthenticationChanged) {
				httputil.Error(w, 401, 40100, "登录已失效，请重新登录")
			} else {
				httputil.Error(w, 503, 50300, "登录会话暂不可用，请重试")
			}
			return
		}
		p, known := routePolicies[r.Pattern]
		if !known {
			httputil.Error(w, 403, 40300, "该操作尚未配置访问权限")
			return
		}
		resourceID := r.PathValue("id")
		if resourceID == "" {
			resourceID = r.PathValue("group")
		}
		if p.resource == "session" {
			resourceID = r.PathValue("sessionId")
			if resourceID == "" {
				resourceID = c.SessionID
			}
		}
		if p.resource == "account" && resourceID == "" && p.action == "user.password" {
			resourceID = strconv.FormatInt(u.ID, 10)
		}
		if len(resourceID) > 128 {
			resourceID = resourceID[:128]
		}
		action := p.action
		if !allowed(u.Role, p.permission) {
			if action == "" {
				action = "access.read"
			}
			id, err := s.Store.BeginAudit(r.Context(), store.AuditEntry{ActorID: &u.ID, ActorName: u.Username, Action: action, Resource: p.resource, ResourceID: resourceID, Outcome: "denied"})
			if err != nil {
				httputil.Error(w, 503, 50300, "权限校验暂不可用")
				return
			}
			_ = s.Store.FinishAudit(r.Context(), id, "denied", resourceID)
			httputil.Error(w, 403, 40300, "当前账户没有执行该操作的权限")
			return
		}
		r = r.WithContext(context.WithValue(r.Context(), claimsKey{}, c))
		if action == "" {
			next(w, r, u)
			return
		}
		auditID, e := s.Store.BeginAudit(r.Context(), store.AuditEntry{ActorID: &u.ID, ActorName: u.Username, Action: action, Resource: p.resource, ResourceID: resourceID, Outcome: "started"})
		if e != nil {
			httputil.Error(w, 503, 50300, "操作记录暂不可用，请重试")
			return
		}
		capture := &auditWriter{ResponseWriter: w, status: 200}
		next(capture, r, u)
		if resourceID == "" && capture.status < 400 {
			var v struct {
				Data struct {
					ID json.Number `json:"id"`
				}
			}
			if json.Unmarshal(capture.prefix, &v) == nil {
				resourceID = string(v.Data.ID)
			}
		}
		outcome := "succeeded"
		if capture.status >= 400 {
			outcome = "failed"
		}
		if capture.status == 403 || capture.status == 401 {
			outcome = "denied"
		}
		ctx, stop := context.WithTimeout(context.WithoutCancel(r.Context()), 2*time.Second)
		defer stop()
		if e = s.Store.FinishAudit(ctx, auditID, outcome, resourceID); e != nil {
			log.Printf("operation audit finalization failed: action=%s", action)
		}
	}
}
