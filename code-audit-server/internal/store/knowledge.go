package store

import (
	"axiom/contracts"
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	_ "embed"
	"encoding/hex"
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"
)

//go:embed knowledge.sql
var knowledgeSQL string
var ErrKnowledgeInput = errors.New("知识文档输入无效")
var ErrKnowledgeConflict = errors.New("知识文档已更新或归档，请刷新后再试")

type KnowledgeVersion struct {
	LatestVersion int    `json:"latestVersion"`
	ID            int64  `json:"id"`
	DocumentID    string `json:"documentId"`
	Version       int    `json:"version"`
	Title         string `json:"title"`
	Content       string `json:"content,omitempty"`
	SHA256        string `json:"sha256"`
	CreatedAt     string `json:"createdAt"`
	Archived      bool   `json:"archived"`
}
type KnowledgePage struct {
	Items    []KnowledgeVersion `json:"items"`
	Total    int                `json:"total"`
	Page     int                `json:"page"`
	PageSize int                `json:"pageSize"`
}

func validKnowledge(title, content string) error {
	if strings.TrimSpace(title) == "" || utf8.RuneCountInString(title) > 128 || !utf8.ValidString(title) || strings.ContainsRune(title, 0) {
		return fmt.Errorf("%w: 文档标题须为 1–128 个字符", ErrKnowledgeInput)
	}
	if strings.TrimSpace(content) == "" || len(content) > contracts.KnowledgeDocumentMaxBytes || !utf8.ValidString(content) || strings.ContainsRune(content, 0) {
		return fmt.Errorf("%w: 知识文档须为非空 UTF-8 文本，最多 64 KiB", ErrKnowledgeInput)
	}
	return nil
}
func (s *Store) SaveKnowledge(ctx context.Context, document string, base int, title, content string) (*KnowledgeVersion, error) {
	if e := validKnowledge(title, content); e != nil {
		return nil, e
	}
	release, e := s.storageGate(ctx, false)
	if e != nil {
		return nil, e
	}
	defer release()
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	now := time.Now().UTC().Format(time.RFC3339Nano)
	version := base + 1
	if document == "" {
		if base != 0 {
			return nil, ErrKnowledgeConflict
		}
		b := make([]byte, 16)
		if _, e = rand.Read(b); e != nil {
			return nil, e
		}
		document = hex.EncodeToString(b)
		_, e = tx.ExecContext(ctx, `INSERT INTO knowledge_documents(id,title,version,created_at) VALUES(?,?,1,?)`, document, title, now)
	} else {
		res, err := tx.ExecContext(ctx, `UPDATE knowledge_documents SET title=?,version=version+1 WHERE id=? AND version=? AND archived=0`, title, document, base)
		e = err
		if e == nil {
			n, _ := res.RowsAffected()
			if n != 1 {
				return nil, ErrKnowledgeConflict
			}
		}
	}
	if e != nil {
		return nil, e
	}
	h := sha256.Sum256([]byte(content))
	v := &KnowledgeVersion{DocumentID: document, Version: version, LatestVersion: version, Title: title, Content: content, SHA256: hex.EncodeToString(h[:]), CreatedAt: now}
	e = tx.QueryRowContext(ctx, `INSERT INTO knowledge_versions(document_id,version,title,content,sha256,created_at) VALUES(?,?,?,?,?,?) RETURNING id`, document, version, title, content, v.SHA256, now).Scan(&v.ID)
	if e != nil {
		return nil, e
	}
	return v, tx.Commit()
}
func (s *Store) KnowledgeVersion(ctx context.Context, id int64) (*KnowledgeVersion, error) {
	v := &KnowledgeVersion{}
	var a int
	e := s.db.QueryRowContext(ctx, `SELECT v.id,v.document_id,v.version,v.title,v.content,v.sha256,v.created_at,d.archived,d.version FROM knowledge_versions v JOIN knowledge_documents d ON d.id=v.document_id WHERE v.id=?`, id).Scan(&v.ID, &v.DocumentID, &v.Version, &v.Title, &v.Content, &v.SHA256, &v.CreatedAt, &a, &v.LatestVersion)
	v.Archived = a != 0
	return v, e
}
func (s *Store) ArchiveKnowledge(ctx context.Context, id string, archived bool) error {
	release, e := s.storageGate(ctx, false)
	if e != nil {
		return e
	}
	defer release()
	res, e := s.db.ExecContext(ctx, `UPDATE knowledge_documents SET archived=? WHERE id=?`, boolInt(archived), id)
	if e != nil {
		return e
	}
	n, e := res.RowsAffected()
	if e == nil && n == 0 {
		return sql.ErrNoRows
	}
	return e
}
func (s *Store) ListKnowledge(ctx context.Context, q, document string, archived bool, page int) (KnowledgePage, error) {
	out := KnowledgePage{Items: []KnowledgeVersion{}, PageSize: 50}
	where := "d.archived=? AND v.version=d.version"
	args := []any{boolInt(archived)}
	if document != "" {
		where = "d.id=?"
		args = []any{document}
	}
	if q != "" {
		clause, a := s.catalogSearch([]string{"v.title"}, q)
		where += " AND " + clause
		args = append(args, a...)
	}
	from := ` FROM knowledge_versions v JOIN knowledge_documents d ON d.id=v.document_id WHERE ` + where
	tx, e := s.db.BeginTx(ctx, &sql.TxOptions{Isolation: sql.LevelRepeatableRead, ReadOnly: true})
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	if e = tx.QueryRowContext(ctx, "SELECT COUNT(*)"+from, args...).Scan(&out.Total); e != nil {
		return out, e
	}
	out.Page = pageNumber(page, out.Total, out.PageSize)
	args = append(args, out.PageSize, (out.Page-1)*out.PageSize)
	rows, e := tx.QueryContext(ctx, `SELECT v.id,v.document_id,v.version,v.title,v.sha256,v.created_at,d.archived,d.version`+from+` ORDER BY v.id DESC LIMIT ? OFFSET ?`, args...)
	if e != nil {
		return out, e
	}
	for rows.Next() {
		var v KnowledgeVersion
		var a int
		if e = rows.Scan(&v.ID, &v.DocumentID, &v.Version, &v.Title, &v.SHA256, &v.CreatedAt, &a, &v.LatestVersion); e != nil {
			rows.Close()
			return out, e
		}
		v.Archived = a != 0
		out.Items = append(out.Items, v)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return out, e
	}
	return out, tx.Commit()
}
func (s *Store) ResolveKnowledge(ctx context.Context, ids []int64) ([]contracts.KnowledgeDocument, error) {
	if len(ids) > contracts.KnowledgeMaxDocuments {
		return nil, errors.New("每次扫描最多引用 8 份知识文档")
	}
	out := []contracts.KnowledgeDocument{}
	seen := map[int64]bool{}
	for _, id := range ids {
		if seen[id] || id < 1 {
			return nil, errors.New("知识文档版本无效或重复")
		}
		seen[id] = true
		v, e := s.KnowledgeVersion(ctx, id)
		if e != nil {
			return nil, errors.New("知识文档版本不存在")
		}
		if v.Archived {
			return nil, ErrKnowledgeConflict
		}
		out = append(out, contracts.KnowledgeDocument{VersionID: strconv.FormatInt(id, 10), Title: v.Title, Content: v.Content, SHA256: v.SHA256})
	}
	return out, nil
}
func (s *Store) TaskScanInput(ctx context.Context, id int64) (*contracts.ScanInput, error) {
	var raw string
	e := s.db.QueryRowContext(ctx, `SELECT document_json FROM task_scan_inputs WHERE task_id=?`, id).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	v, e := contracts.DecodeScanInput([]byte(raw), nil)
	return &v, e
}
func (s *Store) TaskParentState(ctx context.Context, id int64) (*contracts.SDKParentState, error) {
	var raw string
	e := s.db.QueryRowContext(ctx, `SELECT document_json FROM task_parent_states WHERE task_id=?`, id).Scan(&raw)
	if errors.Is(e, sql.ErrNoRows) {
		return nil, nil
	}
	if e != nil {
		return nil, e
	}
	v, e := contracts.DecodeSDKParentState([]byte(raw), nil)
	return &v, e
}
func ValidateScanInput(v *contracts.ScanInput, mode, validation string) error {
	if v == nil {
		return nil
	}
	raw, e := contracts.EncodeJSON(v)
	if e != nil {
		return e
	}
	if _, e = contracts.DecodeScanInput(raw, nil); e != nil {
		return errors.New("高级扫描参数格式不正确")
	}
	if v.Subagents.Present && mode != "deep" {
		return errors.New("每个 Worker 的子代理数量仅适用于 Deep")
	}
	if v.ValidationPrompt.Present && (mode == "deep" || validation == "exploitability") {
		return errors.New("自定义验证指令仅适用于 Standard 默认验证；不能与加强验证模板同时使用")
	}
	for _, p := range []contracts.Optional[string]{v.ScanPrompt, v.ValidationPrompt, v.PostScanPrompt} {
		if p.Present && strings.TrimSpace(p.Value) == "" {
			return errors.New("自定义指令不能仅包含空白字符")
		}
	}
	seen := map[string]bool{}
	for _, d := range v.Knowledge {
		if seen[d.VersionID] {
			return errors.New("知识文档版本重复")
		}
		seen[d.VersionID] = true
		if e = validKnowledge(d.Title, d.Content); e != nil {
			return e
		}
		sum := sha256.Sum256([]byte(d.Content))
		if fmt.Sprintf("%x", sum) != d.SHA256 {
			return errors.New("知识文档哈希不匹配")
		}
	}
	return nil
}

// Ordered row locks serialize freezing against document updates and archival.
func (s *Store) verifyKnowledgeReferences(ctx context.Context, tx *transaction, input contracts.ScanInput) error {
	docs := append([]contracts.KnowledgeDocument(nil), input.Knowledge...)
	sort.Slice(docs, func(i, j int) bool { return docs[i].VersionID < docs[j].VersionID })
	for _, d := range docs {
		query := `SELECT v.title,v.content,v.sha256,d.archived FROM knowledge_versions v JOIN knowledge_documents d ON d.id=v.document_id WHERE v.id=?`
		if s.IsPostgres() {
			query += ` FOR UPDATE OF d`
		}
		var title, content, hash string
		var a int
		if e := tx.QueryRowContext(ctx, query, d.VersionID).Scan(&title, &content, &hash, &a); e != nil {
			return errors.New("引用的知识文档版本不可用")
		}
		if a != 0 {
			return ErrKnowledgeConflict
		}
		if title != d.Title || content != d.Content || hash != d.SHA256 {
			return errors.New("引用的知识文档与冻结内容不一致")
		}
	}
	return nil
}
