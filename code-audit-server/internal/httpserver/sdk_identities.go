package httpserver

import (
	"axiom/code-audit-server/internal/cryptokey"
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"encoding/json"
	"io"
	"net/http"
	"strings"
)

func (s *Server) listSDKIdentities(w http.ResponseWriter, r *http.Request, _ *store.User) {
	v, e := s.Store.SDKIdentities(r.Context())
	if e != nil {
		httputil.Error(w, 503, 50300, "SDK 执行身份暂不可读取")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, v)
}
func (s *Server) createSDKIdentity(w http.ResponseWriter, r *http.Request, _ *store.User) {
	var v struct{ Name, AgentID, Kind string }
	if !sdkIdentityJSON(w, r, &v) {
		return
	}
	out, e := s.Store.CreateSDKIdentity(r.Context(), strings.TrimSpace(v.Name), v.AgentID, v.Kind)
	if e != nil {
		httputil.Error(w, 409, 40900, "身份名称、类型或绑定节点无效；请确认服务使用 PostgreSQL")
		return
	}
	httputil.OK(w, out)
}
func (s *Server) sdkIdentityOperations(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "身份编号无效")
		return
	}
	v, e := s.Store.SDKIdentityOperations(r.Context(), id)
	if e != nil {
		httputil.Error(w, 503, 50300, "SDK 身份操作暂不可读取")
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, v)
}
func (s *Server) startSDKIdentityOperation(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "身份编号无效")
		return
	}
	var v struct {
		Generation int64  `json:"generation"`
		Action     string `json:"action"`
		APIKey     string `json:"apiKey"`
	}
	if !sdkIdentityJSON(w, r, &v) {
		return
	}
	encrypted := ""
	if v.Action == "login_api_key" {
		if len(v.APIKey) < 1 || len(v.APIKey) > 8192 || strings.ContainsAny(v.APIKey, "\r\n\x00") {
			httputil.Error(w, 400, 40000, "请输入单行 API Key，最多 8192 字节")
			return
		}
		var e error
		encrypted, e = cryptokey.Encrypt(v.APIKey)
		if e != nil {
			httputil.Error(w, 503, 50300, "身份凭据暂时无法加密保存")
			return
		}
	} else if v.APIKey != "" {
		httputil.Error(w, 400, 40000, "此操作不接受 API Key")
		return
	}
	out, e := s.Store.NewSDKIdentityOperation(r.Context(), id, v.Generation, v.Action, encrypted)
	if e != nil {
		httputil.Error(w, 409, 40900, store.ErrSDKIdentity.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, out)
}

// Identity requests allow the contract's full bounded API Key. The generic
// local-account decoder has a smaller limit and must not truncate this input.
func sdkIdentityJSON(w http.ResponseWriter, r *http.Request, value any) bool {
	defer r.Body.Close()
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, 64*1024))
	d.DisallowUnknownFields()
	var trailing any
	if d.Decode(value) != nil || d.Decode(&trailing) != io.EOF {
		httputil.Error(w, 400, 40000, "SDK 身份请求格式无效或内容过大")
		return false
	}
	return true
}
func (s *Server) cancelSDKIdentityOperation(w http.ResponseWriter, r *http.Request, _ *store.User) {
	id, ok := pathID(r, "id")
	if !ok {
		httputil.Error(w, 400, 40000, "操作编号无效")
		return
	}
	if e := s.Store.CancelSDKIdentityOperation(r.Context(), id); e != nil {
		httputil.Error(w, 409, 40900, "操作已结束或正在退出清理，请刷新状态")
		return
	}
	httputil.OK(w, map[string]bool{"ok": true})
}
func (s *Server) claimSDKIdentity(w http.ResponseWriter, r *http.Request) {
	var v struct {
		AgentID            string `json:"agentId"`
		SDKIdentityVersion int    `json:"sdkIdentityVersion"`
	}
	if !decodeAgent(w, r, &v) {
		return
	}
	ref := contracts.SDKIdentityRef{ID: "1", Name: "probe", AgentID: v.AgentID, Generation: 1, Kind: "chatgpt"}
	raw, _ := contracts.EncodeJSON(ref)
	if v.SDKIdentityVersion != 1 || contracts.ValidateJSON(contracts.KindSDKIdentityRef, raw, nil) != nil {
		executionError(w, 400, "invalid_input", "unsupported SDK identity capability")
		return
	}
	claim, encrypted, e := s.Store.ClaimSDKIdentity(r.Context(), v.AgentID, s.storageClaimLimits(r.Context()))
	if e != nil {
		executionError(w, 503, "provider_unavailable", "SDK identity queue unavailable")
		return
	}
	if claim != nil && encrypted != "" {
		secret, e := cryptokey.Decrypt(encrypted)
		if e != nil {
			executionError(w, 503, "configuration_error", "SDK identity credential unavailable")
			return
		}
		claim.APIKey = contracts.Some(secret)
	}
	w.Header().Set("Cache-Control", "no-store")
	httputil.OK(w, claim)
}
func (s *Server) reportSDKIdentity(w http.ResponseWriter, r *http.Request) {
	defer r.Body.Close()
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 64*1024))
	if e != nil {
		executionError(w, 400, "invalid_input", "identity report too large")
		return
	}
	v, e := contracts.DecodeSDKIdentityReport(raw, nil)
	if e != nil {
		executionError(w, 400, "invalid_input", "invalid identity report")
		return
	}
	status, e := s.Store.ReportSDKIdentity(r.Context(), v, s.QueueLimits.Lease)
	if e != nil {
		executionError(w, 409, "lease_expired", "identity operation lease or identity changed")
		return
	}
	httputil.OK(w, map[string]string{"status": status})
}
