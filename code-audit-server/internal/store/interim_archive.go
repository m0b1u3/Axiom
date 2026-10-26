package store

import (
	"axiom/contracts"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"
)

type InterimArchiveRequest struct {
	ReviewTaskID  int64  `json:"reviewTaskId"`
	ReviewAttempt int    `json:"reviewAttempt"`
	ReportSHA256  string `json:"reportSha256"`
	Title         string `json:"title"`
	Severity      string `json:"severity"`
	Description   string `json:"description"`
}

func (s *Store) ArchiveInterimReview(ctx context.Context, group, actor int64, request InterimArchiveRequest) (*Finding, error) {
	if !s.IsPostgres() {
		return nil, errors.New("此历史存储不支持新增复核归档")
	}
	tx, e := s.db.BeginTx(ctx, nil)
	if e != nil {
		return nil, e
	}
	defer tx.Rollback()
	var id int64
	if e = tx.QueryRowContext(ctx, `SELECT id FROM interim_review_groups WHERE id=? FOR UPDATE`, group).Scan(&id); e != nil {
		return nil, e
	}
	var existing, review int64
	var document string
	e = tx.QueryRowContext(ctx, `SELECT finding_id,review_task_id,document_json FROM interim_review_archives WHERE group_id=?`, group).Scan(&existing, &review, &document)
	if e == nil {
		a, err := contracts.DecodeInterimReviewArchive([]byte(document), nil)
		if err != nil || review != request.ReviewTaskID || int(a.Execution.Attempt) != request.ReviewAttempt || string(a.ReportSHA256) != request.ReportSHA256 {
			return nil, errors.New("该组已由其他复核结论归档，不能覆盖")
		}
		return &Finding{ID: existing, ScanID: review, Fingerprint: a.FindingFingerprint}, nil
	}
	if !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	var inputJSON, inputSHA, source, sourcePath, status string
	var attempt int
	e = tx.QueryRowContext(ctx, `SELECT r.task_id,s.input_json,s.input_sha256,t.source_content_hash,t.source_path,t.status,t.attempt FROM interim_reviews r JOIN interim_review_sources s ON s.id=r.source_id JOIN scan_tasks t ON t.id=r.task_id WHERE r.group_id=? ORDER BY r.task_id DESC LIMIT 1`, group).Scan(&review, &inputJSON, &inputSHA, &source, &sourcePath, &status, &attempt)
	if e != nil {
		return nil, e
	}
	if review != request.ReviewTaskID || attempt != request.ReviewAttempt || status != "completed" {
		return nil, errors.New("请选择该组最新且成功完成的独立复核；失败、取消或旧结论不能归档")
	}
	envelope := contracts.InterimReviewEnvelope{SchemaVersion: 1, InputJSON: inputJSON, InputSHA256: contracts.Digest(inputSHA)}
	input, e := contracts.ReadInterimReview(envelope, source)
	if e != nil {
		return nil, e
	}
	ref := contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(review)), Attempt: int64(attempt)}
	var raw string
	if e = tx.QueryRowContext(ctx, `SELECT result_json FROM scan_attempts WHERE task_id=? AND attempt=? FOR UPDATE`, review, attempt).Scan(&raw); e != nil {
		return nil, e
	}
	full, e := contracts.DecodeExecutionCompletion([]byte(raw), &ref)
	if e != nil || !full.Result.Present || full.Result.Value.Status != "completed" {
		return nil, errors.New("复核的原生结果不可校验")
	}
	op, e := OperationResult(full.Result.Value)
	if e != nil || op == nil || op.Error != nil || op.Validation == nil || op.Validation.Disposition != "reportable" {
		return nil, errors.New("仅 SDK 明确可报告的复核结论可正式归档；待核查、抑制和不适用不能归档")
	}
	if contracts.ValidationReportBodyState(op.Validation.Report) != "body_present" {
		return nil, errors.New("归档已阻止：复核报告报告结构不完整，未识别到独立评估正文（正文可能粘连在标题行），原始响应与标签仅供诊断")
	}
	outcome, e := contracts.DecodeInterimReviewOutcome(full.Result.Value.Extensions.Value["interimReviewOutcome"], &ref)
	if e != nil || string(outcome.InputSHA256) != inputSHA || outcome.SourceContentHash != input.SourceContentHash || string(outcome.ReportSHA256) != request.ReportSHA256 || request.ReportSHA256 != contracts.InterimReviewDigest([]byte(op.Validation.Report)) {
		return nil, errors.New("复核报告或证据哈希已变化，请刷新后重新核对")
	}
	checked := CheckInterimReviewSources(ctx, sourcePath, source, op.Validation.Report)
	if checked.State != "checked" {
		return nil, fmt.Errorf("归档已阻止：%s", checked.Reason)
	}
	now := time.Now().UTC().Format(time.RFC3339Nano)
	fingerprint := fmt.Sprint("interim-review-", group, "-", input.GroupKey)
	archive := contracts.InterimReviewArchive{SchemaVersion: 1, Execution: ref, GroupID: group, SourceContentHash: input.SourceContentHash, InputSHA256: contracts.Digest(inputSHA), ReportSHA256: outcome.ReportSHA256, FindingFingerprint: fingerprint, Members: input.Members, Disposition: "reportable", Assessment: "sdk_reportable", ArchivedAt: contracts.Timestamp(now), ArchivedBy: actor}
	archiveJSON, e := contracts.EncodeJSON(archive)
	if e != nil || contracts.ValidateJSON(contracts.KindInterimReviewArchive, archiveJSON, &ref) != nil {
		return nil, errors.New("归档来源合同无效")
	}
	title, description := strings.TrimSpace(request.Title), strings.TrimSpace(request.Description)
	if title == "" {
		title = input.Members[0].Candidate.Title
	}
	if description == "" {
		description = op.Validation.Report
	}
	description = "平台独立复核归档：SDK 源码复核可报告，未复现。原扫描与 SDK 原始结果保持不变。\n\n" + description
	rawFinding, _ := contracts.EncodeJSON(map[string]any{"archive": archive, "review": op.Validation, "frozenInput": input, "sourceCheck": checked})
	f := contracts.Finding{Fingerprint: fingerprint, Title: title, Severity: contracts.Severity(request.Severity), Description: description, Evidence: contracts.Some(op.Validation.Report), Source: contracts.Some("interim_review"), Raw: contracts.Some(contracts.JSONValue(rawFinding)), Extensions: contracts.Some(contracts.JSONObject{"axiomInterimReviewArchive": archiveJSON})}
	// These nodes record source-review claims; neither reportable nor an intact
	// checkpoint is promoted to runtime reproduction/verification.
	facts := contracts.FindingEvidence{SchemaVersion: 1, Execution: ref, FindingFingerprint: fingerprint, SourceContentHash: &input.SourceContentHash, SDKVersion: "0.5.0", AdapterVersion: 2, Completeness: "complete", Nodes: []contracts.EvidenceNode{}, Edges: []contracts.EvidenceRelation{}, Diagnostics: []contracts.EvidenceDiagnostic{}}
	rootID, validationID := contracts.InterimReviewDigest([]byte(fingerprint+"finding")), contracts.InterimReviewDigest([]byte(fingerprint+"validation"))
	facts.Nodes = append(facts.Nodes, contracts.EvidenceNode{ID: rootID, Kind: "finding", Label: title, State: "inferred", StateReason: "平台归档：SDK 源码复核可报告；未执行应用或复现漏洞。", SourcePointer: "/title", SourceCheck: "not_checked"}, contracts.EvidenceNode{ID: validationID, Kind: "validation", Label: "SDK 可报告 · 未复现", State: "inferred", StateReason: "独立源码复核结论；不作为已运行 PoC 或漏洞复现的证据。", SourcePointer: "/raw/review/report", SourceCheck: "not_checked"})
	facts.Edges = append(facts.Edges, contracts.EvidenceRelation{ID: contracts.InterimReviewDigest([]byte(fingerprint + "review")), From: rootID, To: validationID, Kind: "contains", State: "inferred", Reason: "该归档关联此 SDK 源码复核；不推定已复现。", SourcePointer: "/raw/review/report"})
	for i, m := range input.Members {
		id := contracts.InterimReviewDigest([]byte(fmt.Sprint(fingerprint, ":candidate:", i)))
		pointer := fmt.Sprintf("/raw/frozenInput/members/%d/candidate", i)
		facts.Nodes = append(facts.Nodes, contracts.EvidenceNode{ID: id, Kind: "source", Label: fmt.Sprint("原候选 #", m.Execution.TaskID, "/", m.Execution.Attempt, " · ", m.Candidate.Title), State: "inferred", StateReason: "冻结的原模型候选声明；完整性哈希不等于漏洞验证或复现。", SourcePointer: pointer, SourceCheck: "not_checked"})
		facts.Edges = append(facts.Edges, contracts.EvidenceRelation{ID: contracts.InterimReviewDigest([]byte(id + "edge")), From: rootID, To: id, Kind: "contains", State: "inferred", Reason: "正式归档关联此原执行候选及其保存证据。", SourcePointer: pointer})
	}
	evidenceJSON, _ := contracts.EncodeJSON(facts)
	f.Extensions.Value["axiomEvidence"] = evidenceJSON
	normalized, e := contracts.EncodeJSON(f)
	if e != nil || contracts.ValidateJSON(contracts.KindFinding, normalized, nil) != nil {
		return nil, errors.New("归档标题、严重度或报告超出漏洞合同要求")
	}
	if _, e = contracts.DecodeFindingEvidence(evidenceJSON, &ref); e != nil {
		return nil, e
	}
	e = tx.QueryRowContext(ctx, `INSERT INTO findings(scan_id,fingerprint,title,severity,description,evidence,status,raw,normalized_json,created_at) VALUES(?,?,?,?,?,?,'open',?,?,?) RETURNING id`, review, fingerprint, title, request.Severity, description, op.Validation.Report, string(rawFinding), string(normalized), now).Scan(&id)
	if e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `INSERT INTO interim_review_archives(group_id,review_task_id,review_attempt,finding_id,document_json,archived_at,archived_by) VALUES(?,?,?,?,?,?,?)`, group, review, attempt, id, string(archiveJSON), now, actor); e != nil {
		return nil, e
	}
	if _, e = tx.ExecContext(ctx, `UPDATE scan_tasks SET finding_count=(SELECT COUNT(*) FROM findings WHERE scan_id=?) WHERE id=?`, review, review); e != nil {
		return nil, e
	}
	if e = tx.Commit(); e != nil {
		return nil, e
	}
	return &Finding{ID: id, ScanID: review, Fingerprint: fingerprint, Title: title, Severity: request.Severity, Description: description, Evidence: op.Validation.Report, Status: "open", Normalized: normalized, Raw: rawFinding}, nil
}

// The SDK archive remains byte-identical and its result.findings remains empty.
// Platform review archives are exposed separately for historical investigation.
func (s *Store) interimArchivedFindings(ctx context.Context, id int64, attempt int) ([]Finding, error) {
	if !s.IsPostgres() {
		return nil, nil
	}
	var n int
	if e := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM interim_review_archives WHERE review_task_id=? AND review_attempt=?`, id, attempt).Scan(&n); e != nil {
		return nil, e
	}
	if n == 0 {
		return nil, nil
	}
	return s.ListFindings(ctx, id)
}
