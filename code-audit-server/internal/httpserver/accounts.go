package httpserver

import (
	"axiom/code-audit-server/internal/auth"
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"errors"
	"net/http"
	"regexp"
)

var accountName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,63}$`)

func (s *Server) accounts(w http.ResponseWriter, r *http.Request, u *store.User) {
	v, e := s.Store.ListAccounts(r.Context())
	if e != nil {
		httputil.Error(w, 503, 50300, "账户列表暂不可用")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, v)
}
func (s *Server) createAccount(w http.ResponseWriter, r *http.Request, u *store.User) {
	var body struct {
		Username string `json:"username"`
		Password string `json:"password"`
		Role     string `json:"role"`
	}
	if !smallJSON(w, r, &body) {
		return
	}
	if !accountName.MatchString(body.Username) || !store.ValidRole(body.Role) || !auth.ValidNewPassword(body.Password) {
		httputil.Error(w, 400, 40000, "用户名须为 1–64 个字母、数字、点、横线或下划线；新密码至少 12 个字符，并选择有效角色")
		return
	}
	release, ok := s.hashSlot(w)
	if !ok {
		return
	}
	defer release()
	hash, e := auth.NewPasswordHash(body.Password)
	if e != nil {
		httputil.Error(w, 503, 50300, "账户创建暂不可用")
		return
	}
	next, e := s.Store.CreateAccount(r.Context(), body.Username, hash, body.Role)
	if e != nil {
		if errors.Is(e, store.ErrAccountConflict) {
			httputil.Error(w, 409, 40900, "该用户名已存在")
		} else {
			httputil.Error(w, 503, 50300, "账户创建暂不可用")
		}
		return
	}
	httputil.OK(w, publicUser(next))
}
func (s *Server) updateAccount(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "账户 ID 无效")
		return
	}
	var body struct {
		Role   string `json:"role"`
		Active *bool  `json:"active"`
	}
	if !smallJSON(w, r, &body) {
		return
	}
	if !store.ValidRole(body.Role) || body.Active == nil {
		httputil.Error(w, 400, 40000, "请选择有效角色和账户状态")
		return
	}
	if id == u.ID {
		httputil.Error(w, 409, 40900, "不能修改当前账户的角色或停用当前账户，请由其他管理员操作")
		return
	}
	target, e := s.Store.GetUser(r.Context(), id)
	if e != nil {
		httputil.Error(w, 404, 40400, "账户不存在")
		return
	}
	name := s.AdminUsername
	if name == "" {
		name = "admin"
	}
	if target.Username == name && (body.Role != "admin" || !*body.Active) {
		httputil.Error(w, 409, 40900, "部署管理员账户必须保持启用和管理员角色")
		return
	}
	if e = s.Store.UpdateAccount(r.Context(), id, body.Role, *body.Active); e != nil {
		if errors.Is(e, store.ErrAccountConflict) {
			httputil.Error(w, 409, 40900, "至少需要保留一个启用的管理员账户")
		} else {
			httputil.Error(w, 503, 50300, "账户更新暂不可用")
		}
		return
	}
	target, e = s.Store.GetUser(r.Context(), id)
	if e != nil {
		httputil.Error(w, 503, 50300, "账户更新后读取失败，请重新加载")
		return
	}
	httputil.OK(w, publicUser(target))
}
func (s *Server) operations(w http.ResponseWriter, r *http.Request, u *store.User) {
	cursor := r.URL.Query().Get("before")
	if len(cursor) > 128 {
		httputil.Error(w, 400, 40000, "操作记录游标无效")
		return
	}
	v, e := s.Store.ListAudits(r.Context(), cursor)
	if e != nil {
		httputil.Error(w, 503, 50300, "操作记录暂不可用")
		return
	}
	next := ""
	if len(v) == 100 {
		next = v[len(v)-1].ID
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, map[string]any{"items": v, "nextCursor": next, "limit": 100})
}
