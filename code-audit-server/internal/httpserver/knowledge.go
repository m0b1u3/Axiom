package httpserver

import (
	"axiom/code-audit-server/internal/httputil"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"bytes"
	"database/sql"
	"encoding/json"
	"errors"
	"io"
	"net/http"
	"strconv"
	"strings"
)

func knowledgeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	defer r.Body.Close()
	raw, e := io.ReadAll(http.MaxBytesReader(w, r.Body, 1024*1024))
	if e != nil || contracts.CheckJSONText(raw) != nil {
		httputil.Error(w, 400, 40000, "知识文档请求不是有效 UTF-8 JSON，或超过大小限制")
		return false
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if e = d.Decode(v); e != nil {
		httputil.Error(w, 400, 40000, "知识文档请求包含未知字段或字段类型错误")
		return false
	}
	return true
}
func knowledgeError(w http.ResponseWriter, e error) {
	if errors.Is(e, store.ErrKnowledgeConflict) {
		httputil.Error(w, 409, 40900, e.Error())
		return
	}
	if errors.Is(e, store.ErrKnowledgeInput) {
		httputil.Error(w, 400, 40000, e.Error())
		return
	}
	if errors.Is(e, sql.ErrNoRows) {
		httputil.Error(w, 404, 40400, "知识文档不存在")
		return
	}
	httputil.Error(w, 503, 50300, "知识文档暂时无法保存，请稍后重试")
}
func (s *Server) listKnowledge(w http.ResponseWriter, r *http.Request, u *store.User) {
	page := 1
	if n := r.URL.Query().Get("page"); n != "" {
		var e error
		page, e = strconv.Atoi(n)
		if e != nil || page < 1 {
			httputil.Error(w, 400, 40000, "页码须为正整数")
			return
		}
	}
	q := strings.TrimSpace(r.URL.Query().Get("q"))
	if len(q) > 512 {
		httputil.Error(w, 400, 40000, "搜索文本过长")
		return
	}
	v, e := s.Store.ListKnowledge(r.Context(), q, r.URL.Query().Get("documentId"), r.URL.Query().Get("archived") == "true", page)
	if e != nil {
		httputil.Error(w, 503, 50300, "知识文档列表暂不可用")
		return
	}
	httputil.OK(w, v)
}
func (s *Server) knowledgeVersion(w http.ResponseWriter, r *http.Request, u *store.User) {
	id, e := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if e != nil || id < 1 {
		httputil.Error(w, 400, 40000, "文档版本编号无效")
		return
	}
	v, e := s.Store.KnowledgeVersion(r.Context(), id)
	if errors.Is(e, sql.ErrNoRows) {
		httputil.Error(w, 404, 40400, "知识文档版本不存在")
		return
	}
	if e != nil {
		httputil.Error(w, 503, 50300, "知识文档正文暂时无法读取")
		return
	}
	httputil.OK(w, v)
}
func (s *Server) saveKnowledge(w http.ResponseWriter, r *http.Request, u *store.User) {
	var v struct {
		Title       string `json:"title"`
		Content     string `json:"content"`
		BaseVersion int    `json:"baseVersion"`
	}
	if !knowledgeBody(w, r, &v) {
		return
	}
	doc, e := s.Store.SaveKnowledge(r.Context(), r.PathValue("id"), v.BaseVersion, v.Title, v.Content)
	if e != nil {
		knowledgeError(w, e)
		return
	}
	httputil.OK(w, doc)
}
func (s *Server) archiveKnowledge(w http.ResponseWriter, r *http.Request, u *store.User) {
	var v struct {
		Archived *bool `json:"archived"`
	}
	if !knowledgeBody(w, r, &v) {
		return
	}
	if v.Archived == nil {
		httputil.Error(w, 400, 40000, "请明确指定文档归档状态")
		return
	}
	if e := s.Store.ArchiveKnowledge(r.Context(), r.PathValue("id"), *v.Archived); e != nil {
		knowledgeError(w, e)
		return
	}
	httputil.OK(w, map[string]bool{"ok": true})
}
