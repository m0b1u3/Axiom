package store

import (
	"axiom/contracts"
	"context"
	"database/sql"
	_ "embed"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	_ "modernc.org/sqlite"
)

//go:embed schema.sql
var schemaSQL string

type Store struct {
	db            *database
	storageGateDB *sql.DB
	storageMutex  sync.RWMutex
}

type Task struct {
	SDKParameters       *contracts.SDKScanParameters          `json:"sdkParameters,omitempty"`
	SDKIdentity         *contracts.SDKIdentityRef             `json:"sdkIdentity,omitempty"`
	ComponentScan       *contracts.ComponentScanRequest       `json:"componentScan,omitempty"`
	ComponentPlanning   *contracts.ComponentPlanningOperation `json:"componentPlanning,omitempty"`
	GitTarget           *contracts.GitDiffTarget              `json:"gitTarget,omitempty"`
	SDKOperationKind    string                                `json:"sdkOperationKind,omitempty"`
	SDKOperation        *contracts.SDKOperation               `json:"sdkOperation,omitempty"`
	SDKOperationOrigin  int64                                 `json:"-"`
	ParentExecution     *contracts.ExecutionRef               `json:"parentExecution,omitempty"`
	ParentState         *contracts.SDKParentState             `json:"-"`
	ScanInput           *contracts.ScanInput                  `json:"scanInput,omitempty"`
	BudgetAccounting    bool                                  `json:"-"`
	MaxTotalCostUSD     *float64                              `json:"-"`
	ExecutionMaxCostUSD *float64                              `json:"-"`
	ID                  int64                                 `json:"id"`
	ScanNo              string                                `json:"scanNo"`
	SourcePath          string                                `json:"sourcePath"`
	Provider            string                                `json:"provider"`
	Model               string                                `json:"model"`
	BaseURL             string                                `json:"baseUrl"`
	ScanMode            string                                `json:"scanMode"`
	// 路径子集扫描白名单（JSON 数组字符串，空串 = 全量）。
	IncludePaths                string                        `json:"includePaths"`
	ScanFocus                   string                        `json:"scanFocus"`
	ValidationMode              string                        `json:"validationMode"`
	BusinessContext             string                        `json:"businessContext"`
	TimeoutMinutes              int                           `json:"timeoutMinutes"`
	DeepMaxDiscoveryRuns        int                           `json:"deepMaxDiscoveryRuns"`
	DeepStopAfterNoNew          int                           `json:"deepStopAfterNoNew"`
	MaxCostUSD                  *float64                      `json:"maxCostUsd"`
	Workers                     *int                          `json:"workers"`
	ObservationContractVersion  int                           `json:"observationContractVersion"`
	Progress                    *contracts.ProgressSnapshotV2 `json:"progress,omitempty"`
	ProgressDiagnosticJSON      string                        `json:"-"`
	ProgressDiagnostic          *contracts.ContractError      `json:"progressDiagnostic,omitempty"`
	ResultContractVersion       int                           `json:"resultContractVersion"`
	InputContractVersion        int                           `json:"inputContractVersion"`
	SourceContentHash           string                        `json:"sourceContentHash,omitempty"`
	ModelSnapshotJSON           string                        `json:"-"`
	PolicySnapshotJSON          string                        `json:"-"`
	CredentialSnapshotEncrypted string                        `json:"-"`
	Status                      string                        `json:"status"`
	CurrentStage                string                        `json:"currentStage"`
	ProgressJSON                string                        `json:"progressJson"`
	AgentID                     string                        `json:"agentId"`
	LeaseExpiresAt              string                        `json:"-"`
	HeartbeatAt                 string                        `json:"-"`
	Attempt                     int                           `json:"attempt"`
	FindingCount                int                           `json:"findingCount"`
	ErrorCode                   string                        `json:"errorCode"`
	ErrorMessage                string                        `json:"errorMessage"`
	OutputPath                  string                        `json:"outputPath"`
	SourceSnapshotID            int64                         `json:"sourceSnapshotId"`
	ModelConfigID               int64                         `json:"modelConfigId"`
	// 项目归属：组织维度（快照/扫描挂到项目下，0 = 未分配）
	ProjectID   int64  `json:"projectId"`
	ProjectName string `json:"projectName"`
	CreatedAt   string `json:"createdAt"`
	StartedAt   string `json:"startedAt"`
	FinishedAt  string `json:"finishedAt"`
	ClaimedFrom string `json:"claimedFrom,omitempty"`
}

type Finding struct {
	Normalized  json.RawMessage `json:"normalized,omitempty"`
	ID          int64           `json:"id"`
	ScanID      int64           `json:"scanId"`
	Fingerprint string          `json:"fingerprint"`
	Title       string          `json:"title"`
	Severity    string          `json:"severity"`
	Category    string          `json:"category"`
	CWE         []string        `json:"cwe"`
	FilePath    string          `json:"filePath"`
	StartLine   *int            `json:"startLine"`
	EndLine     *int            `json:"endLine"`
	Description string          `json:"description"`
	Evidence    string          `json:"evidence"`
	Remediation string          `json:"remediation"`
	Confidence  *float64        `json:"confidence"`
	Status      string          `json:"status"`
	// 上游 Codex Security finding 完整 JSON（未裁剪）；API 以对象输出
	Raw json.RawMessage `json:"raw"`
}

// FindingStatus 手工流转状态集合。
var FindingStatuses = map[string]bool{
	"open":           true,
	"confirmed":      true,
	"false_positive": true,
	"fixed":          true,
}

type ModelConfigRow struct {
	ReasoningEffort string `json:"reasoningEffort,omitempty"`
	SDKIdentityOnly bool   `json:"sdkIdentityOnly,omitempty"`
	ID              int64  `json:"id"`
	Name            string `json:"name"`
	Provider        string `json:"provider"`
	BaseURL         string `json:"baseUrl"`
	Model           string `json:"model"`
	ApiKeyEncrypted string `json:"-"`
	// 掩码提示（如 sk-…wxyz）：仅 listModelConfigs 瞬态填充，不落库不持久化
	ApiKeyHint          string `json:"apiKeyHint,omitempty"`
	Status              string `json:"status"`
	CompatibilityStatus string `json:"compatibilityStatus"`
	Enabled             bool   `json:"enabled"`
	// 备注 / 官网链接：纯展示信息（cc-switch 式表单字段）
	Remark     string `json:"remark"`
	WebsiteURL string `json:"websiteUrl"`
	// 上游协议覆盖：空 = 用注册表预设的 wireApi
	WireAPI   string `json:"wireApi"`
	CreatedAt string `json:"createdAt"`
	UpdatedAt string `json:"updatedAt"`
}

func (s *Store) CreateModelConfig(ctx context.Context, m ModelConfigRow) (*ModelConfigRow, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	var id int64
	err := s.db.QueryRowContext(ctx, `
INSERT INTO model_configs (name, provider, base_url, model, api_key_encrypted, status, compatibility_status, remark, website_url, wire_api, reasoning_effort, created_at, updated_at)
VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		m.Name, m.Provider, m.BaseURL, m.Model, m.ApiKeyEncrypted, defaultIfEmpty(m.Status, "active"),
		defaultIfEmpty(m.CompatibilityStatus, "experimental"), m.Remark, m.WebsiteURL, m.WireAPI, m.ReasoningEffort, now, now).Scan(&id)
	if err != nil {
		return nil, err
	}
	return s.GetModelConfig(ctx, id)
}

func (s *Store) GetModelConfig(ctx context.Context, id int64) (*ModelConfigRow, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, provider, COALESCE(base_url,''), model, COALESCE(api_key_encrypted,''),
       COALESCE(status,'active'), COALESCE(compatibility_status,'experimental'), COALESCE(enabled,0),
       COALESCE(remark,''), COALESCE(website_url,''), COALESCE(wire_api,''), COALESCE(reasoning_effort,''), created_at, updated_at
FROM model_configs WHERE id = ?`, id)
	var m ModelConfigRow
	err := row.Scan(&m.ID, &m.Name, &m.Provider, &m.BaseURL, &m.Model, &m.ApiKeyEncrypted,
		&m.Status, &m.CompatibilityStatus, &m.Enabled, &m.Remark, &m.WebsiteURL, &m.WireAPI, &m.ReasoningEffort, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) ListModelConfigs(ctx context.Context) ([]ModelConfigRow, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, name, provider, COALESCE(base_url,''), model,
       COALESCE(status,'active'), COALESCE(compatibility_status,'experimental'), COALESCE(enabled,0),
       COALESCE(remark,''), COALESCE(website_url,''), COALESCE(wire_api,''), COALESCE(reasoning_effort,''), created_at, updated_at,
       COALESCE(api_key_encrypted,'')
FROM model_configs ORDER BY id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []ModelConfigRow
	for rows.Next() {
		var m ModelConfigRow
		if err := rows.Scan(&m.ID, &m.Name, &m.Provider, &m.BaseURL, &m.Model,
			&m.Status, &m.CompatibilityStatus, &m.Enabled, &m.Remark, &m.WebsiteURL, &m.WireAPI, &m.ReasoningEffort, &m.CreatedAt, &m.UpdatedAt,
			&m.ApiKeyEncrypted); err != nil {
			return nil, err
		}
		out = append(out, m)
	}
	return out, rows.Err()
}

// SetModelConfigEnabled 设置启用状态。启用是独占的：启用一条会清掉其他行。
func (s *Store) SetModelConfigEnabled(ctx context.Context, id int64, enabled bool) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if s.db.postgres {
		if _, err := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(740920)`); err != nil {
			return err
		}
	}
	if enabled {
		if _, err := tx.ExecContext(ctx, `UPDATE model_configs SET enabled = 0 WHERE enabled = 1`); err != nil {
			return err
		}
	}
	res, err := tx.ExecContext(ctx, `UPDATE model_configs SET enabled = ? WHERE id = ?`, boolInt(enabled), id)
	if err != nil {
		return err
	}
	if n, _ := res.RowsAffected(); n == 0 {
		return sql.ErrNoRows
	}
	return tx.Commit()
}

// GetEnabledModelConfig 返回当前启用的模型配置；无则 sql.ErrNoRows。
func (s *Store) GetEnabledModelConfig(ctx context.Context) (*ModelConfigRow, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, provider, COALESCE(base_url,''), model, COALESCE(api_key_encrypted,''),
       COALESCE(status,'active'), COALESCE(compatibility_status,'experimental'), COALESCE(enabled,0),
       COALESCE(remark,''), COALESCE(website_url,''), COALESCE(wire_api,''), COALESCE(reasoning_effort,''), created_at, updated_at
FROM model_configs WHERE enabled = 1 ORDER BY id LIMIT 1`)
	var m ModelConfigRow
	err := row.Scan(&m.ID, &m.Name, &m.Provider, &m.BaseURL, &m.Model, &m.ApiKeyEncrypted,
		&m.Status, &m.CompatibilityStatus, &m.Enabled, &m.Remark, &m.WebsiteURL, &m.WireAPI, &m.ReasoningEffort, &m.CreatedAt, &m.UpdatedAt)
	if err != nil {
		return nil, err
	}
	return &m, nil
}

func (s *Store) UpdateModelConfig(ctx context.Context, id int64, m ModelConfigRow) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
UPDATE model_configs
SET name = ?, provider = ?, base_url = ?, model = ?,
    api_key_encrypted = COALESCE(?, api_key_encrypted),
    status = COALESCE(?, status),
    compatibility_status = COALESCE(?, compatibility_status),
    remark = ?, website_url = ?, wire_api = ?, reasoning_effort = ?,
    updated_at = ?
WHERE id = ?`,
		m.Name, m.Provider, m.BaseURL, m.Model, nullIfEmpty(m.ApiKeyEncrypted),
		nullIfEmpty(m.Status), nullIfEmpty(m.CompatibilityStatus), m.Remark, m.WebsiteURL, m.WireAPI, m.ReasoningEffort, now, id)
	return err
}

func (s *Store) DeleteModelConfig(ctx context.Context, id int64) error {
	_, err := s.db.ExecContext(ctx, `DELETE FROM model_configs WHERE id = ?`, id)
	return err
}

// SetModelConfigCompatibility 回写兼容性验证结果（测试连通性后由 server 调用）。
func (s *Store) SetModelConfigCompatibility(ctx context.Context, id int64, compat string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE model_configs SET compatibility_status = ?, updated_at = ? WHERE id = ?`,
		compat, time.Now().UTC().Format(time.RFC3339), id)
	return err
}

func defaultIfEmpty(v, fallback string) string {
	if v == "" {
		return fallback
	}
	return v
}

type SnapshotRow struct {
	GitDiff          bool   `json:"gitDiff,omitempty"`
	ID               int64  `json:"id"`
	OriginalFilename string `json:"originalFilename"`
	ArchiveSha256    string `json:"archiveSha256"`
	ContentHash      string `json:"contentHash"`
	SnapshotPath     string `json:"snapshotPath"`
	FileCount        int    `json:"fileCount"`
	TotalSize        int64  `json:"totalSize"`
	// 项目归属（0 = 未分配）；ScanCount 由 ListSnapshots 统计填充
	ProjectID int64  `json:"projectId"`
	ScanCount int    `json:"scanCount"`
	CreatedAt string `json:"createdAt"`
}

func Open(path string) (*Store, error) {
	if strings.HasPrefix(path, "postgres://") || strings.HasPrefix(path, "postgresql://") {
		return openPostgres(path)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return nil, err
	}
	db, err := sql.Open("sqlite", path+"?_pragma=busy_timeout(5000)&_pragma=foreign_keys(1)")
	if err != nil {
		return nil, err
	}
	// Serialize SQLite transactions, including read-then-write credential CAS.
	// PostgreSQL retains its independent pool and row/advisory locks.
	db.SetMaxOpenConns(1)
	if _, err := db.Exec(schemaSQL); err != nil {
		_ = db.Close()
		return nil, err
	}
	for _, col := range [][2]string{{"role", "TEXT NOT NULL DEFAULT 'operator' CHECK(role IN ('admin','operator','viewer'))"}, {"active", "INTEGER NOT NULL DEFAULT 1 CHECK(active IN (0,1))"}, {"auth_version", "INTEGER NOT NULL DEFAULT 0"}} {
		if err := ensureColumn(db, "users", col[0], col[1]); err != nil {
			db.Close()
			return nil, err
		}
	}
	if _, err := db.Exec(securitySQL); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(budgetSQL); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(knowledgeSQL); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(componentPlansSQL); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(gitDiffSQL); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(componentScansSQL); err != nil {
		return nil, err
	}
	if _, err := db.Exec(sdkOperationsSQL); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(nativeResultSQL); err != nil {
		db.Close()
		return nil, err
	}
	if _, err := db.Exec(storageSQL); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "model_configs", "reasoning_effort", "TEXT NOT NULL DEFAULT ''"); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "scan_tasks", "sdk_parameters_json", "TEXT NOT NULL DEFAULT ''"); err != nil {
		db.Close()
		return nil, err
	}
	// 轻量迁移：老库补列（CREATE TABLE IF NOT EXISTS 不会加列）
	for _, col := range [][2]string{
		{"max_cost_usd", "REAL CHECK(max_cost_usd > 0 AND max_cost_usd <= 9007199254740991)"},
		{"workers", "INTEGER CHECK(workers BETWEEN 1 AND 9007199254740991)"},
		{"input_contract_version", "INTEGER NOT NULL DEFAULT 0"},
		{"progress_diagnostic_json", "TEXT NOT NULL DEFAULT ''"},
		{"observation_contract_version", "INTEGER NOT NULL DEFAULT 0"},
		{"result_contract_version", "INTEGER NOT NULL DEFAULT 0"},
		{"source_content_hash", "TEXT NOT NULL DEFAULT ''"},
		{"model_snapshot_json", "TEXT"}, {"policy_snapshot_json", "TEXT"},
		{"credential_snapshot_encrypted", "TEXT"},
	} {
		if err := ensureColumn(db, "scan_tasks", col[0], col[1]); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	if err := ensureColumn(db, "scan_tasks", "source_snapshot_id", "INTEGER"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "scan_tasks", "model_config_id", "INTEGER"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "scan_tasks", "include_paths", "TEXT"); err != nil {
		_ = db.Close()
		return nil, err
	}
	for _, column := range []string{"deep_max_discovery_runs", "deep_stop_after_no_new"} {
		if err := ensureColumn(db, "scan_tasks", column, "INTEGER NOT NULL DEFAULT 0"); err != nil {
			return nil, err
		}
	}
	for _, column := range []string{"validation_mode", "business_context"} {
		if err := ensureColumn(db, "scan_tasks", column, "TEXT NOT NULL DEFAULT ''"); err != nil {
			return nil, err
		}
	}
	if err := ensureColumn(db, "scan_tasks", "scan_focus", "TEXT"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "findings", "status", "TEXT NOT NULL DEFAULT 'open'"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "findings", "normalized_json", "TEXT"); err != nil {
		db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "findings", "raw", "TEXT"); err != nil {
		_ = db.Close()
		return nil, err
	}
	// 项目归属链：Project → Snapshot → ScanTask
	if err := ensureColumn(db, "source_snapshots", "project_id", "INTEGER"); err != nil {
		_ = db.Close()
		return nil, err
	}
	if err := ensureColumn(db, "scan_tasks", "project_id", "INTEGER"); err != nil {
		_ = db.Close()
		return nil, err
	}
	// 模型启用开关：独占（最多一行为 1）
	if err := ensureColumn(db, "model_configs", "enabled", "INTEGER NOT NULL DEFAULT 0"); err != nil {
		_ = db.Close()
		return nil, err
	}
	// cc-switch 式表单字段：备注 / 官网链接 / 上游协议覆盖（空 = 用注册表预设）
	for _, col := range [][2]string{
		{"remark", "TEXT NOT NULL DEFAULT ''"},
		{"website_url", "TEXT NOT NULL DEFAULT ''"},
		{"wire_api", "TEXT NOT NULL DEFAULT ''"},
	} {
		if err := ensureColumn(db, "model_configs", col[0], col[1]); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	// 迁移友好：老库没有任何启用行时，自动启用最早一条配置
	var enabledN int
	if err := db.QueryRow(`SELECT COUNT(*) FROM model_configs WHERE enabled = 1`).Scan(&enabledN); err != nil {
		_ = db.Close()
		return nil, err
	}
	if enabledN == 0 {
		if _, err := db.Exec(`UPDATE model_configs SET enabled = 1 WHERE id = (SELECT MIN(id) FROM model_configs)`); err != nil {
			_ = db.Close()
			return nil, err
		}
	}
	return &Store{db: &database{DB: db}}, nil
}

func ensureColumn(db *sql.DB, table, column, decl string) error {
	rows, err := db.Query("PRAGMA table_info(" + table + ")")
	if err != nil {
		return err
	}
	defer rows.Close()
	for rows.Next() {
		var cid int
		var name, typ string
		var notNull int
		var dflt any
		var pk int
		if err := rows.Scan(&cid, &name, &typ, &notNull, &dflt, &pk); err != nil {
			return err
		}
		if name == column {
			return nil
		}
	}
	_, err = db.Exec("ALTER TABLE " + table + " ADD COLUMN " + column + " " + decl)
	return err
}

func (s *Store) Close() error {
	if s.storageGateDB != nil {
		s.storageGateDB.Close()
	}
	return s.db.Close()
}

func (s *Store) InsertPending(ctx context.Context, t Task) (int64, error) {
	release, e := s.storageGate(ctx, false)
	if e != nil {
		return 0, e
	}
	defer release()

	if err := validateTaskInput(t); err != nil {
		return 0, err
	}
	var total *int64
	if t.BudgetAccounting {
		if (t.InputContractVersion != 1 && t.InputContractVersion != contracts.ScanInputVersion && t.InputContractVersion != contracts.SDKOperationInputVersion && t.InputContractVersion != contracts.GitDiffInputVersion && t.InputContractVersion != contracts.ComponentPlanningInputVersion && t.InputContractVersion != contracts.ComponentScanInputVersion) || t.ResultContractVersion != 1 || t.ObservationContractVersion != 1 {
			return 0, ErrBudgetPolicy
		}
		if t.MaxTotalCostUSD != nil {
			n, e := BudgetLimitMicros(*t.MaxTotalCostUSD)
			if e != nil {
				return 0, e
			}
			total = &n
		}
	} else if t.MaxTotalCostUSD != nil {
		return 0, ErrBudgetPolicy
	}
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()
	if t.SDKIdentity != nil {
		if !s.IsPostgres() {
			return 0, ErrSDKIdentity
		}
		if _, e := tx.ExecContext(ctx, `SELECT pg_advisory_xact_lock(740919)`); e != nil {
			return 0, e
		}
	}
	if t.SDKOperation != nil && t.SDKOperationOrigin > 0 {
		var attempt, snapshot int64
		var status, model, sourceHash, sourcePath string
		query := `SELECT attempt,status,COALESCE(model_snapshot_json,''),source_content_hash,source_path,source_snapshot_id FROM scan_tasks WHERE id=?`
		if s.db.postgres {
			query += ` FOR UPDATE`
		}
		if e := tx.QueryRowContext(ctx, query, t.SDKOperationOrigin).Scan(&attempt, &status, &model, &sourceHash, &sourcePath, &snapshot); e != nil {
			return 0, e
		}
		if model != t.ModelSnapshotJSON || sourceHash != t.SourceContentHash || sourcePath != t.SourcePath || snapshot != t.SourceSnapshotID {
			return 0, errors.New("SDK 操作必须使用来源冻结的源码与模型")
		}
		if t.SDKOperation.Kind == contracts.SDKOperationKindValidate {
			if t.SDKOperation.Origin == nil || t.SDKOperation.Origin.TaskID != contracts.TaskID(fmt.Sprint(t.SDKOperationOrigin)) || t.SDKOperation.Origin.Attempt != attempt || !IsTerminalStatus(status) {
				return 0, errors.New("来源执行已变化，请刷新后重新选择漏洞")
			}
			var n int
			if e := tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM findings WHERE id=? AND scan_id=?`, *t.SDKOperation.FindingID, t.SDKOperationOrigin).Scan(&n); e != nil || n != 1 {
				return 0, errors.New("来源漏洞已变化")
			}
		} else if t.ScanInput != nil {
			var original string
			e := tx.QueryRowContext(ctx, `SELECT document_json FROM task_scan_inputs WHERE task_id=?`, t.SDKOperationOrigin).Scan(&original)
			if e != nil && !errors.Is(e, sql.ErrNoRows) {
				return 0, e
			}
			if original == "" && !sameJSON(t.ScanInput, contracts.ScanInput{SchemaVersion: 1, Knowledge: []contracts.KnowledgeDocument{}}) {
				return 0, errors.New("来源没有此扫描输入，不能在预检中新增参数")
			}
			if original != "" {
				old, e := contracts.DecodeScanInput([]byte(original), nil)
				if e != nil || !sameJSON(old, t.ScanInput) {
					return 0, errors.New("预检必须使用来源执行冻结的知识文档与配置")
				}
			}
		}
	}
	now := time.Now().UTC().Format(time.RFC3339)
	var id int64
	err = tx.QueryRowContext(ctx, `
INSERT INTO scan_tasks (
  scan_no, source_path, provider, model, base_url, scan_mode, include_paths, scan_focus, timeout_minutes, deep_max_discovery_runs, deep_stop_after_no_new, validation_mode, business_context,
  status, source_snapshot_id, model_config_id, project_id, created_at,
  max_cost_usd, workers, input_contract_version, result_contract_version, observation_contract_version, source_content_hash, model_snapshot_json, policy_snapshot_json, credential_snapshot_encrypted
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, 'pending', ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?) RETURNING id`,
		t.ScanNo, t.SourcePath, t.Provider, t.Model, nullIfEmpty(t.BaseURL),
		defaultMode(t.ScanMode), nullIfEmpty(t.IncludePaths), nullIfEmpty(t.ScanFocus), defaultTimeout(t.TimeoutMinutes), t.DeepMaxDiscoveryRuns, t.DeepStopAfterNoNew, t.ValidationMode, t.BusinessContext,
		nullIfEmptyZero(t.SourceSnapshotID), nullIfEmptyZero(t.ModelConfigID), nullIfEmptyZero(t.ProjectID), now,
		t.MaxCostUSD, t.Workers, t.InputContractVersion, t.ResultContractVersion, t.ObservationContractVersion, t.SourceContentHash,
		nullIfEmpty(t.ModelSnapshotJSON), nullIfEmpty(t.PolicySnapshotJSON), nullIfEmpty(t.CredentialSnapshotEncrypted),
	).Scan(&id)
	if err != nil {
		return 0, err
	}
	if t.BudgetAccounting {
		if _, err = tx.ExecContext(ctx, `INSERT INTO task_budgets(task_id,total_micros) VALUES(?,?)`, id, total); err != nil {
			return 0, err
		}
	}
	if t.ScanInput != nil {
		if e := s.verifyOperationKnowledge(ctx, tx, t); e != nil {
			return 0, e
		}
		raw, e := contracts.EncodeJSON(t.ScanInput)
		if e != nil {
			return 0, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO task_scan_inputs(task_id,document_json) VALUES(?,?)`, id, string(raw)); e != nil {
			return 0, e
		}
	}
	if t.ParentState != nil {
		raw, e := contracts.EncodeJSON(t.ParentState)
		if e != nil {
			return 0, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO task_parent_states(task_id,parent_task_id,parent_attempt,document_json) VALUES(?,?,?,?)`, id, string(t.ParentState.Execution.TaskID), t.ParentState.Execution.Attempt, string(raw)); e != nil {
			return 0, e
		}
	}
	if e := s.saveSDKParameters(ctx, tx, &t, id); e != nil {
		return 0, e
	}
	if e := s.saveTaskIdentity(ctx, tx, t, id); e != nil {
		return 0, e
	}
	if e := s.saveComponentScan(ctx, tx, t, id); e != nil {
		return 0, e
	}
	if e := s.saveComponentOperation(ctx, tx, t, id); e != nil {
		return 0, e
	}
	if e := s.saveGitTarget(ctx, tx, t, id); e != nil {
		return 0, e
	}
	if t.SDKOperation != nil {
		operation := *t.SDKOperation
		operation.Execution = contracts.ExecutionRef{TaskID: contracts.TaskID(fmt.Sprint(id)), Attempt: 1}
		raw, e := contracts.EncodeJSON(operation)
		if e != nil {
			return 0, e
		}
		if _, e = contracts.DecodeSDKOperation(raw, &operation.Execution); e != nil {
			return 0, e
		}
		if _, e = tx.ExecContext(ctx, `INSERT INTO sdk_operations(task_id,origin_task_id,kind,document_json) VALUES(?,?,?,?)`, id, nullIfEmptyZero(t.SDKOperationOrigin), operation.Kind, string(raw)); e != nil {
			return 0, e
		}
	}
	return id, tx.Commit()
}

func (s *Store) Claim(ctx context.Context, agentID string, lease time.Duration) (*Task, error) {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return nil, err
	}
	defer func() { _ = tx.Rollback() }()

	now := time.Now().UTC()
	row := tx.QueryRowContext(ctx, `
SELECT id, scan_no, source_path, provider, model, COALESCE(base_url,''), scan_mode,
       COALESCE(include_paths,''), COALESCE(scan_focus,''), timeout_minutes, deep_max_discovery_runs, deep_stop_after_no_new, validation_mode, business_context, status, attempt, COALESCE(output_path,''),
       COALESCE(source_snapshot_id,0), COALESCE(model_config_id,0)
FROM scan_tasks
WHERE input_contract_version = 0 AND max_cost_usd IS NULL
   AND (status = 'pending'
   OR (status IN ('claimed','scanning','cancel_requested') AND (lease_expires_at IS NULL OR lease_expires_at < ?)))
ORDER BY id
LIMIT 1`, now.Format(time.RFC3339))

	var t Task
	origStatus := ""
	if err := row.Scan(
		&t.ID, &t.ScanNo, &t.SourcePath, &t.Provider, &t.Model, &t.BaseURL,
		&t.ScanMode, &t.IncludePaths, &t.ScanFocus, &t.TimeoutMinutes, &t.DeepMaxDiscoveryRuns, &t.DeepStopAfterNoNew, &t.ValidationMode, &t.BusinessContext, &t.Status, &t.Attempt, &t.OutputPath,
		&t.SourceSnapshotID, &t.ModelConfigID,
	); err != nil {
		if err == sql.ErrNoRows {
			return nil, nil
		}
		return nil, err
	}
	origStatus = t.Status

	leaseUntil := now.Add(lease).Format(time.RFC3339)
	started := now.Format(time.RFC3339)
	if _, err := tx.ExecContext(ctx, `
UPDATE scan_tasks
SET status = 'claimed',
    agent_id = ?,
    attempt = attempt + 1,
    lease_expires_at = ?,
    heartbeat_at = ?,
    started_at = COALESCE(started_at, ?)
WHERE id = ?`, agentID, leaseUntil, started, started, t.ID); err != nil {
		return nil, err
	}
	if err := tx.Commit(); err != nil {
		return nil, err
	}
	t.Status = "claimed"
	t.ClaimedFrom = origStatus
	t.AgentID = agentID
	t.Attempt++
	t.LeaseExpiresAt = leaseUntil
	t.HeartbeatAt = started
	t.StartedAt = started
	return &t, nil
}

func (s *Store) Heartbeat(ctx context.Context, id int64, agentID string, lease time.Duration, stage, progressJSON string) error {
	until := time.Now().UTC().Add(lease).Format(time.RFC3339)
	now := time.Now().UTC().Format(time.RFC3339)
	// 只有 claimed/scanning 才推回 scanning；cancel_requested 不能被心跳覆盖。
	_, err := s.db.ExecContext(ctx, `
UPDATE scan_tasks
SET heartbeat_at = ?, lease_expires_at = ?, current_stage = ?, progress_json = ?,
    status = CASE WHEN status IN ('claimed','scanning') THEN 'scanning' ELSE status END
WHERE id = ? AND agent_id = ?`, now, until, nullIfEmpty(stage), nullIfEmpty(progressJSON), id, agentID)
	return err
}

// PromoteModelConfigToSupported：扫描完整成功且产出 findings → 将对应模型配置
// 从 experimental 升为 supported（不降级；失败扫描不回调）。
func (s *Store) PromoteModelConfigToSupported(ctx context.Context, configID int64) error {
	if configID <= 0 {
		return nil
	}
	_, err := s.db.ExecContext(ctx, `
UPDATE model_configs
SET compatibility_status = 'supported', updated_at = ?
WHERE id = ? AND compatibility_status = 'experimental'`,
		time.Now().UTC().Format(time.RFC3339), configID)
	return err
}

// DeleteScan 删除终态任务及其全部 occurrence（调用方负责产物目录清理）。
func (s *Store) DeleteScan(ctx context.Context, id int64) error {
	release, e := s.storageGate(ctx, false)
	if e != nil {
		return e
	}
	defer release()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	query := "SELECT status FROM scan_tasks WHERE id=?"
	if s.db.postgres {
		query += " FOR UPDATE"
	}
	var status string
	if err := tx.QueryRowContext(ctx, query, id).Scan(&status); err != nil {
		return err
	}
	var linked int
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM sdk_operations WHERE origin_task_id=?)+(SELECT COUNT(*) FROM component_operations WHERE task_id=?)`, id, id).Scan(&linked); err != nil {
		return err
	}
	if linked > 0 {
		return ErrStorageProtected
	}
	var unknown int
	if err = tx.QueryRowContext(ctx, `SELECT COUNT(*) FROM task_cost_attempts WHERE task_id=? AND state<>'complete'`, id).Scan(&unknown); err != nil {
		return err
	}
	if unknown > 0 {
		return ErrStorageProtected
	}
	if err = s.cleanupBlocksTask(ctx, tx, id); err != nil {
		return err
	}
	if !IsTerminalStatus(status) {
		return fmt.Errorf("only terminal scans can be deleted (current: %s)", status)
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM findings WHERE scan_id = ?`, id); err != nil {
		return err
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM scan_tasks WHERE id = ?`, id); err != nil {
		return err
	}
	return tx.Commit()
}

// RequestCancel：pending 尚未被领取，直接终态 canceled；
// claimed/scanning 置 cancel_requested，由 Agent 停进程后收尾。
func (s *Store) RequestCancel(ctx context.Context, id int64) (bool, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	res, err := s.db.ExecContext(ctx, `
UPDATE scan_tasks
SET status = CASE WHEN status = 'pending' THEN 'canceled' ELSE 'cancel_requested' END,
    finished_at = CASE WHEN status = 'pending' THEN ? ELSE finished_at END,
    error_code = CASE WHEN status = 'pending' THEN 'canceled' ELSE error_code END,
    error_message = CASE WHEN status = 'pending' THEN 'canceled before claim' ELSE error_message END
WHERE id = ? AND status IN ('pending','claimed','scanning')`, now, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	return n > 0, err
}

// Retry 只允许终态任务回到 pending，并清掉上次扫描的 findings（计数已归零）。
func (s *Store) Retry(ctx context.Context, id int64) (bool, error) {
	return s.retryExecution(ctx, id, nil, "")
}

// RetryExecution fences the Web confirmation to the exact observed attempt.
// Retry remains available for older clients with its historical semantics.
func (s *Store) RetryExecution(ctx context.Context, id int64, expected int, status string) (bool, error) {
	return s.retryExecution(ctx, id, &expected, status)
}

func (s *Store) retryExecution(ctx context.Context, id int64, expected *int, status string) (bool, error) {
	return s.retryComponentExecution(ctx, id, expected, status, nil)
}
func (s *Store) RetryComponents(ctx context.Context, id int64, attempt int, status string, indices []int64) (bool, error) {
	if len(indices) == 0 {
		return false, ErrComponentInvalid
	}
	return s.retryComponentExecution(ctx, id, &attempt, status, indices)
}
func (s *Store) retryComponentExecution(ctx context.Context, id int64, expected *int, status string, indices []int64) (bool, error) {
	release, e := s.storageGate(ctx, false)
	if e != nil {
		return false, e
	}
	defer release()

	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return false, err
	}
	defer func() { _ = tx.Rollback() }()
	if s.db.postgres {
		var lock int64
		if err = tx.QueryRowContext(ctx, `SELECT id FROM scan_tasks WHERE id=? FOR UPDATE`, id).Scan(&lock); err != nil {
			return false, err
		}
	}
	if expected != nil {
		var currentAttempt int
		var currentStatus string
		if err = tx.QueryRowContext(ctx, `SELECT attempt,status FROM scan_tasks WHERE id=?`, id).Scan(&currentAttempt, &currentStatus); err != nil {
			return false, err
		}
		if currentAttempt != *expected || currentStatus != status {
			return false, ErrRecoveryConflict
		}
	}
	var componentRequest string
	ce := tx.QueryRowContext(ctx, `SELECT document_json FROM component_scans WHERE task_id=?`, id).Scan(&componentRequest)
	if ce != nil && !errors.Is(ce, sql.ErrNoRows) {
		return false, ce
	}
	if ce == nil {
		if indices == nil || expected == nil {
			return false, errors.New("请在 Web 的组件执行中选择失败或未完成组件重新执行")
		}
		if e := s.prepareComponentRetry(ctx, tx, id, *expected, indices, componentRequest); e != nil {
			return false, e
		}
	} else if indices != nil {
		return false, ErrComponentInvalid
	}
	var operationCount int
	if err = tx.QueryRowContext(ctx, `SELECT (SELECT COUNT(*) FROM sdk_operations WHERE task_id=?)+(SELECT COUNT(*) FROM component_operations WHERE task_id=?)`, id, id).Scan(&operationCount); err != nil {
		return false, err
	}
	if operationCount > 0 {
		return false, errors.New("请在 Web 新建 SDK 操作；独立复核需要重新确认未知费用")
	}
	if _, err = s.checkBudgetAdmission(ctx, tx, id); err != nil {
		return false, err
	}
	if err = s.cleanupBlocksTask(ctx, tx, id); err != nil {
		return false, err
	}
	res, err := tx.ExecContext(ctx, `
	UPDATE scan_tasks
	SET status = 'pending', agent_id = NULL, lease_expires_at = NULL,
	    current_stage = '', progress_json = '', progress_diagnostic_json='', error_code = NULL, error_message = NULL,
	    finding_count = 0, started_at = NULL, finished_at = NULL, output_path = NULL
	WHERE id = ? AND status IN ('completed','partial','failed','canceled','timeout')`, id)
	if err != nil {
		return false, err
	}
	n, err := res.RowsAffected()
	if err != nil {
		return false, err
	}
	if n == 0 {
		return false, nil
	}
	if s.db.postgres {
		if _, err := tx.ExecContext(ctx, `UPDATE scan_tasks SET automatic_retries=0,queued_at=NOW(),heartbeat_at=NULL WHERE id=?`, id); err != nil {
			return false, err
		}
	}
	if _, err := tx.ExecContext(ctx, `DELETE FROM findings WHERE scan_id = ?`, id); err != nil {
		return false, err
	}
	if err := tx.Commit(); err != nil {
		return false, err
	}
	return true, nil
}

func IsRunningStatus(status string) bool {
	return status == "pending" || status == "claimed" || status == "scanning" || status == "cancel_requested"
}

func IsTerminalStatus(status string) bool {
	return status == "completed" || status == "partial" || status == "failed" ||
		status == "canceled" || status == "timeout"
}

func (s *Store) SetOutputPath(ctx context.Context, id int64, outputPath string) error {
	_, err := s.db.ExecContext(ctx, `UPDATE scan_tasks SET output_path = ? WHERE id = ?`, outputPath, id)
	return err
}

func (s *Store) Finish(ctx context.Context, id int64, status, errCode, errMsg string, findingCount int) error {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
UPDATE scan_tasks
SET status = ?, error_code = ?, error_message = ?, finding_count = ?, finished_at = ?,
    lease_expires_at = NULL
WHERE id = ?`, status, nullIfEmpty(errCode), nullIfEmpty(errMsg), findingCount, now, id)
	return err
}

func (s *Store) ReplaceFindings(ctx context.Context, scanID int64, findings []Finding) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer func() { _ = tx.Rollback() }()
	if _, err := tx.ExecContext(ctx, `DELETE FROM findings WHERE scan_id = ?`, scanID); err != nil {
		return err
	}
	now := time.Now().UTC().Format(time.RFC3339)
	stmt, err := tx.PrepareContext(ctx, `
INSERT INTO findings (
  scan_id, fingerprint, title, severity, category, cwe, file_path, start_line, end_line,
  description, evidence, remediation, confidence, raw, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)`)
	if err != nil {
		return err
	}
	defer stmt.Close()
	for _, f := range findings {
		cwe, _ := json.Marshal(f.CWE)
		var rawVal any
		if len(f.Raw) > 0 {
			rawVal = string(f.Raw)
		}
		if _, err := stmt.ExecContext(ctx,
			scanID, f.Fingerprint, f.Title, f.Severity, nullIfEmpty(f.Category), string(cwe),
			nullIfEmpty(f.FilePath), f.StartLine, f.EndLine, f.Description, nullIfEmpty(f.Evidence),
			nullIfEmpty(f.Remediation), f.Confidence, rawVal, now,
		); err != nil {
			return err
		}
	}
	return tx.Commit()
}

func (s *Store) Get(ctx context.Context, id int64) (*Task, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT t.id, t.scan_no, t.source_path, t.provider, t.model, COALESCE(t.base_url,''), t.scan_mode, COALESCE(t.include_paths,''), COALESCE(t.scan_focus,''),
       t.timeout_minutes, t.deep_max_discovery_runs, t.deep_stop_after_no_new, t.validation_mode, t.business_context, t.status, COALESCE(t.current_stage,''), COALESCE(t.progress_json,''), COALESCE(t.agent_id,''),
       COALESCE(t.error_code,''), COALESCE(t.error_message,''), t.finding_count, COALESCE(t.output_path,''),
       COALESCE(t.source_snapshot_id,0), COALESCE(t.model_config_id,0), COALESCE(t.project_id,0), COALESCE(p.name,''),
       t.created_at, COALESCE(t.started_at,''), COALESCE(t.finished_at,''), t.attempt, t.max_cost_usd, t.workers, t.input_contract_version, t.result_contract_version, t.observation_contract_version, t.progress_diagnostic_json, t.source_content_hash, COALESCE(t.model_snapshot_json,''), COALESCE(t.policy_snapshot_json,''), COALESCE(t.credential_snapshot_encrypted,'')
FROM scan_tasks t LEFT JOIN projects p ON p.id = t.project_id
WHERE t.id = ?`, id)
	var t Task
	if err := row.Scan(
		&t.ID, &t.ScanNo, &t.SourcePath, &t.Provider, &t.Model, &t.BaseURL, &t.ScanMode,
		&t.IncludePaths, &t.ScanFocus, &t.TimeoutMinutes, &t.DeepMaxDiscoveryRuns, &t.DeepStopAfterNoNew, &t.ValidationMode, &t.BusinessContext, &t.Status, &t.CurrentStage, &t.ProgressJSON, &t.AgentID,
		&t.ErrorCode, &t.ErrorMessage, &t.FindingCount, &t.OutputPath, &t.SourceSnapshotID,
		&t.ModelConfigID, &t.ProjectID, &t.ProjectName, &t.CreatedAt, &t.StartedAt, &t.FinishedAt, &t.Attempt, &t.MaxCostUSD, &t.Workers, &t.InputContractVersion, &t.ResultContractVersion, &t.ObservationContractVersion, &t.ProgressDiagnosticJSON, &t.SourceContentHash, &t.ModelSnapshotJSON, &t.PolicySnapshotJSON, &t.CredentialSnapshotEncrypted,
	); err != nil {
		return nil, err
	}
	input, e := s.TaskScanInput(ctx, id)
	if e != nil {
		return nil, e
	}
	t.ScanInput = input
	if e := s.loadSDKParameters(ctx, &t); e != nil {
		return nil, e
	}
	t.GitTarget, e = s.TaskGitTarget(ctx, id)
	if e != nil {
		return nil, e
	}
	if e := s.loadTaskIdentity(ctx, &t); e != nil {
		return nil, e
	}
	if e := s.loadComponentScan(ctx, &t); e != nil {
		return nil, e
	}
	if e := s.loadComponentOperation(ctx, &t); e != nil {
		return nil, e
	}
	if e := s.loadSDKOperation(ctx, &t); e != nil {
		return nil, e
	}
	var parentID string
	var parentAttempt int64
	e = s.db.QueryRowContext(ctx, `SELECT parent_task_id,parent_attempt FROM task_parent_states WHERE task_id=?`, id).Scan(&parentID, &parentAttempt)
	if e != nil && !errors.Is(e, sql.ErrNoRows) {
		return nil, e
	}
	if e == nil {
		t.ParentExecution = &contracts.ExecutionRef{TaskID: contracts.TaskID(parentID), Attempt: parentAttempt}
	}
	return &t, nil
}

type User struct {
	Role         string `json:"role"`
	Active       bool   `json:"active"`
	AuthVersion  int64  `json:"-"`
	ID           int64  `json:"id"`
	Username     string `json:"username"`
	PasswordHash string `json:"-"`
	CreatedAt    string `json:"createdAt"`
}

type Project struct {
	ID        int64  `json:"id"`
	Name      string `json:"name"`
	CreatedBy int64  `json:"createdBy"`
	// 组织维度统计（ListProjects 填充）
	SnapshotCount int64  `json:"snapshotCount"`
	ScanCount     int64  `json:"scanCount"`
	CreatedAt     string `json:"createdAt"`
}

func (s *Store) EnsureUser(ctx context.Context, username, passwordHash string) (*User, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	_, err := s.db.ExecContext(ctx, `
INSERT INTO users (username, password_hash, created_at)
VALUES (?, ?, ?)
ON CONFLICT(username) DO NOTHING`, username, passwordHash, now)
	if err != nil {
		return nil, err
	}
	return s.GetUserByName(ctx, username)
}

func (s *Store) GetUserByName(ctx context.Context, username string) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, username, password_hash, created_at, role, active, auth_version FROM users WHERE username = ?`, username)
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt, &u.Role, &u.Active, &u.AuthVersion); err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) GetUser(ctx context.Context, id int64) (*User, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, username, password_hash, created_at, role, active, auth_version FROM users WHERE id = ?`, id)
	var u User
	if err := row.Scan(&u.ID, &u.Username, &u.PasswordHash, &u.CreatedAt, &u.Role, &u.Active, &u.AuthVersion); err != nil {
		return nil, err
	}
	return &u, nil
}

func (s *Store) UpdateUserPassword(ctx context.Context, id int64, passwordHash string) error {
	u, err := s.GetUser(ctx, id)
	if err != nil {
		return err
	}
	return s.ChangePassword(ctx, id, u.PasswordHash, passwordHash)
}

func (s *Store) CreateProject(ctx context.Context, name string, createdBy int64) (*Project, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	var id int64
	err := s.db.QueryRowContext(ctx, `
INSERT INTO projects (name, created_by, created_at) VALUES (?, ?, ?) RETURNING id`, name, createdBy, now).Scan(&id)
	if err != nil {
		return nil, err
	}
	return &Project{ID: id, Name: name, CreatedBy: createdBy, CreatedAt: now}, nil
}

func (s *Store) ListProjects(ctx context.Context) ([]Project, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT p.id, p.name, COALESCE(p.created_by,0),
       (SELECT COUNT(*) FROM source_snapshots ss WHERE ss.project_id = p.id),
       (SELECT COUNT(*) FROM scan_tasks st WHERE st.project_id = p.id AND NOT EXISTS(SELECT 1 FROM sdk_operations op WHERE op.task_id=st.id)),
       p.created_at
FROM projects p ORDER BY p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Project
	for rows.Next() {
		var p Project
		if err := rows.Scan(&p.ID, &p.Name, &p.CreatedBy, &p.SnapshotCount, &p.ScanCount, &p.CreatedAt); err != nil {
			return nil, err
		}
		out = append(out, p)
	}
	return out, rows.Err()
}

func (s *Store) DeleteProject(ctx context.Context, id int64) error {
	// 项目删除不级联：快照/扫描的 project_id 置空保留（资产与历史不丢）。
	if _, err := s.db.ExecContext(ctx, `UPDATE source_snapshots SET project_id = NULL WHERE project_id = ?`, id); err != nil {
		return err
	}
	if _, err := s.db.ExecContext(ctx, `UPDATE scan_tasks SET project_id = NULL WHERE project_id = ?`, id); err != nil {
		return err
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM projects WHERE id = ?`, id)
	return err
}

func (s *Store) GetProject(ctx context.Context, id int64) (*Project, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT id, name, COALESCE(created_by,0), created_at FROM projects WHERE id = ?`, id)
	var p Project
	if err := row.Scan(&p.ID, &p.Name, &p.CreatedBy, &p.CreatedAt); err != nil {
		return nil, err
	}
	return &p, nil
}

// ListScans 全量或按项目过滤（projectID > 0 时只看该项目）。
func (s *Store) ListScans(ctx context.Context, projectID int64) ([]Task, error) {
	return s.listScansQuery(ctx, projectID, "", nil, 0, 0)
}

func (s *Store) listScansQuery(ctx context.Context, projectID int64, condition string, filterArgs []any, limit, offset int) ([]Task, error) {
	query := `
SELECT t.id, t.scan_no, t.source_path, t.provider, t.model, COALESCE(t.base_url,''), t.scan_mode, COALESCE(t.include_paths,''), COALESCE(t.scan_focus,''),
       t.timeout_minutes, t.deep_max_discovery_runs, t.deep_stop_after_no_new, t.validation_mode, t.business_context, t.status, COALESCE(t.current_stage,''), COALESCE(t.progress_json,''), COALESCE(t.agent_id,''),
       COALESCE(t.error_code,''), COALESCE(t.error_message,''), t.finding_count, COALESCE(t.output_path,''),
       COALESCE(t.source_snapshot_id,0), COALESCE(t.model_config_id,0), COALESCE(t.project_id,0), COALESCE(p.name,''),
       t.created_at, COALESCE(t.started_at,''), COALESCE(t.finished_at,''), t.attempt, t.max_cost_usd, t.workers, t.input_contract_version, t.result_contract_version, t.observation_contract_version, t.progress_diagnostic_json, t.source_content_hash, COALESCE(t.model_snapshot_json,''), COALESCE(t.policy_snapshot_json,''), COALESCE(t.credential_snapshot_encrypted,''),COALESCE((SELECT kind FROM sdk_operations op WHERE op.task_id=t.id),(SELECT 'component_'||kind FROM component_operations op WHERE op.task_id=t.id),'')
FROM scan_tasks t LEFT JOIN projects p ON p.id = t.project_id`
	if limit > 0 {
		query = strings.ReplaceAll(query, "COALESCE(t.progress_json,'')", "''")
		query = strings.ReplaceAll(query, "t.business_context", "''")
		query = strings.ReplaceAll(query, "COALESCE(t.error_message,'')", "''")
	}
	args := []any{}
	if projectID > 0 {
		query += ` WHERE t.project_id = ?`
		args = append(args, projectID)
	}
	if condition != "" {
		if projectID > 0 {
			query += " AND "
		} else {
			query += " WHERE "
		}
		query += condition
		args = append(args, filterArgs...)
	}
	query += ` ORDER BY t.id DESC`
	if limit > 0 {
		query += " LIMIT ? OFFSET ?"
		args = append(args, limit, offset)
	}
	rows, err := s.db.QueryContext(ctx, query, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []Task
	for rows.Next() {
		var t Task
		if err := rows.Scan(
			&t.ID, &t.ScanNo, &t.SourcePath, &t.Provider, &t.Model, &t.BaseURL, &t.ScanMode,
			&t.IncludePaths, &t.ScanFocus, &t.TimeoutMinutes, &t.DeepMaxDiscoveryRuns, &t.DeepStopAfterNoNew, &t.ValidationMode, &t.BusinessContext, &t.Status, &t.CurrentStage, &t.ProgressJSON, &t.AgentID,
			&t.ErrorCode, &t.ErrorMessage, &t.FindingCount, &t.OutputPath, &t.SourceSnapshotID,
			&t.ModelConfigID, &t.ProjectID, &t.ProjectName, &t.CreatedAt, &t.StartedAt, &t.FinishedAt, &t.Attempt, &t.MaxCostUSD, &t.Workers, &t.InputContractVersion, &t.ResultContractVersion, &t.ObservationContractVersion, &t.ProgressDiagnosticJSON, &t.SourceContentHash, &t.ModelSnapshotJSON, &t.PolicySnapshotJSON, &t.CredentialSnapshotEncrypted, &t.SDKOperationKind,
		); err != nil {
			return nil, err
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

func (s *Store) ListFindings(ctx context.Context, scanID int64) ([]Finding, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, scan_id, fingerprint, title, severity, COALESCE(category,''), COALESCE(cwe,''),
       COALESCE(file_path,''), start_line, end_line, description, COALESCE(evidence,''),
       COALESCE(remediation,''), confidence, COALESCE(status,'open'), COALESCE(raw,''), COALESCE(normalized_json,'')
FROM findings WHERE scan_id = ? ORDER BY id`, scanID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := []Finding{}
	for rows.Next() {
		var f Finding
		var cwe string
		var start, end sql.NullInt64
		var conf sql.NullFloat64
		var rawStr, normalizedStr string
		if err := rows.Scan(
			&f.ID, &f.ScanID, &f.Fingerprint, &f.Title, &f.Severity, &f.Category, &cwe,
			&f.FilePath, &start, &end, &f.Description, &f.Evidence, &f.Remediation, &conf, &f.Status, &rawStr, &normalizedStr,
		); err != nil {
			return nil, err
		}
		if rawStr != "" {
			f.Raw = json.RawMessage(rawStr)
		}
		if normalizedStr != "" {
			f.Normalized = json.RawMessage(normalizedStr)
		}
		f.CWE = CWEList(cwe)
		if start.Valid {
			v := int(start.Int64)
			f.StartLine = &v
		}
		if end.Valid {
			v := int(end.Int64)
			f.EndLine = &v
		}
		if conf.Valid {
			v := conf.Float64
			f.Confidence = &v
		}
		out = append(out, f)
	}
	return out, rows.Err()
}

// FindingSummary 跨扫描去重后的"当前漏洞视图"：每个 fingerprint 取最近一次出现。
type FindingSummary struct {
	ID              int64           `json:"id"`
	ScanID          int64           `json:"scanId"`
	Fingerprint     string          `json:"fingerprint"`
	Title           string          `json:"title"`
	Severity        string          `json:"severity"`
	Category        string          `json:"category"`
	CWE             []string        `json:"cwe"`
	FilePath        string          `json:"filePath"`
	StartLine       *int            `json:"startLine"`
	EndLine         *int            `json:"endLine"`
	Description     string          `json:"description"`
	Status          string          `json:"status"`
	Raw             json.RawMessage `json:"raw"`
	FirstSeenScanID int64           `json:"firstSeenScanId"`
	FirstSeenAt     string          `json:"firstSeenAt"`
	LastSeenAt      string          `json:"lastSeenAt"`
}

type FindingFilter struct {
	ProjectID        *int64
	Severity         string
	Status           string
	Query            string
	SourceSnapshotID int64
	SourcePath       string
}

func (s *Store) ListFindingSummaries(ctx context.Context, f FindingFilter) ([]FindingSummary, error) {
	where, args := s.findingConditions(f)
	rows, err := s.db.QueryContext(ctx, findingCatalog+"SELECT "+findingSelection+" FROM catalog WHERE "+where+" ORDER BY "+severityOrder+",id DESC LIMIT 300", args...)
	if err != nil {
		return nil, err
	}
	return readFindingSummaries(rows)
}

// SetFindingStatus 更新单条 finding 状态。仅允许约定状态集。
func (s *Store) SetFindingStatus(ctx context.Context, findID int64, status string) (bool, error) {
	if !FindingStatuses[status] {
		return false, nil
	}
	res, err := s.db.ExecContext(ctx, `UPDATE findings SET status = ? WHERE id = ?`, status, findID)
	if err != nil {
		return false, err
	}
	n, _ := res.RowsAffected()
	return n > 0, nil
}

// FindPreviousScan 找同一代码来源（优先快照 id，其次路径）下更早的终态扫描。
func (s *Store) FindPreviousScan(ctx context.Context, afterID, snapshotID int64, sourcePath string) (*Task, error) {
	var row *sql.Row
	if snapshotID > 0 {
		row = s.db.QueryRowContext(ctx, `
SELECT id, scan_no, source_path, provider, model, COALESCE(base_url,''), scan_mode, COALESCE(include_paths,''), COALESCE(scan_focus,''), timeout_minutes, deep_max_discovery_runs, deep_stop_after_no_new, validation_mode, business_context,
       status, COALESCE(current_stage,''), COALESCE(progress_json,''), COALESCE(agent_id,''),
       COALESCE(error_code,''), COALESCE(error_message,''), finding_count, COALESCE(output_path,''),
       COALESCE(source_snapshot_id,0), COALESCE(model_config_id,0), created_at, COALESCE(started_at,''), COALESCE(finished_at,'')
FROM scan_tasks
WHERE NOT EXISTS(SELECT 1 FROM sdk_operations op WHERE op.task_id=scan_tasks.id) AND NOT EXISTS(SELECT 1 FROM component_operations op WHERE op.task_id=scan_tasks.id) AND NOT EXISTS(SELECT 1 FROM component_scans cs WHERE cs.task_id=scan_tasks.id) AND id < ? AND source_snapshot_id = ?
  AND status IN ('completed','partial')
ORDER BY id DESC LIMIT 1`, afterID, snapshotID)
	} else {
		row = s.db.QueryRowContext(ctx, `
SELECT id, scan_no, source_path, provider, model, COALESCE(base_url,''), scan_mode, COALESCE(include_paths,''), COALESCE(scan_focus,''), timeout_minutes, deep_max_discovery_runs, deep_stop_after_no_new, validation_mode, business_context,
       status, COALESCE(current_stage,''), COALESCE(progress_json,''), COALESCE(agent_id,''),
       COALESCE(error_code,''), COALESCE(error_message,''), finding_count, COALESCE(output_path,''),
       COALESCE(source_snapshot_id,0), COALESCE(model_config_id,0), created_at, COALESCE(started_at,''), COALESCE(finished_at,'')
FROM scan_tasks
WHERE NOT EXISTS(SELECT 1 FROM sdk_operations op WHERE op.task_id=scan_tasks.id) AND NOT EXISTS(SELECT 1 FROM component_operations op WHERE op.task_id=scan_tasks.id) AND NOT EXISTS(SELECT 1 FROM component_scans cs WHERE cs.task_id=scan_tasks.id) AND id < ? AND source_path = ?
  AND status IN ('completed','partial')
ORDER BY id DESC LIMIT 1`, afterID, sourcePath)
	}
	var t Task
	if err := row.Scan(
		&t.ID, &t.ScanNo, &t.SourcePath, &t.Provider, &t.Model, &t.BaseURL, &t.ScanMode,
		&t.IncludePaths, &t.ScanFocus, &t.TimeoutMinutes, &t.DeepMaxDiscoveryRuns, &t.DeepStopAfterNoNew, &t.ValidationMode, &t.BusinessContext, &t.Status, &t.CurrentStage, &t.ProgressJSON, &t.AgentID,
		&t.ErrorCode, &t.ErrorMessage, &t.FindingCount, &t.OutputPath, &t.SourceSnapshotID,
		&t.ModelConfigID, &t.CreatedAt, &t.StartedAt, &t.FinishedAt,
	); err != nil {
		return nil, err
	}
	return &t, nil
}

func (s *Store) InsertSnapshot(ctx context.Context, snap SnapshotRow) (*SnapshotRow, error) {
	now := time.Now().UTC().Format(time.RFC3339)
	var id int64
	err := s.db.QueryRowContext(ctx, `
INSERT INTO source_snapshots (
  original_filename, archive_sha256, content_hash, snapshot_path,
  file_count, total_size, project_id, created_at
) VALUES (?, ?, ?, ?, ?, ?, ?, ?)
ON CONFLICT(archive_sha256) DO NOTHING RETURNING id`,
		snap.OriginalFilename, snap.ArchiveSha256, snap.ContentHash, snap.SnapshotPath,
		snap.FileCount, snap.TotalSize, nullIfEmptyZero(snap.ProjectID), now).Scan(&id)
	if err != nil && err != sql.ErrNoRows {
		return nil, err
	}
	if err == sql.ErrNoRows {
		// 同内容已存在 → 返回已入库记录（去重）；
		// 老记录未挂项目而这次指定了项目 → 补挂，资产不重复
		existing, gerr := s.GetSnapshotByArchiveSha256(ctx, snap.ArchiveSha256)
		if gerr != nil {
			return nil, gerr
		}
		if existing.ProjectID == 0 && snap.ProjectID > 0 {
			if _, uerr := s.db.ExecContext(ctx,
				`UPDATE source_snapshots SET project_id = ? WHERE id = ?`, snap.ProjectID, existing.ID); uerr == nil {
				existing.ProjectID = snap.ProjectID
			}
		}
		return existing, nil
	}
	return s.GetSnapshot(ctx, id)
}

const snapshotCols = `id, original_filename, archive_sha256, content_hash, snapshot_path, file_count, total_size, COALESCE(project_id,0), created_at,EXISTS(SELECT 1 FROM git_snapshots g WHERE g.snapshot_id=source_snapshots.id)`

func (s *Store) scanSnapshot(row interface{ Scan(...any) error }, snap *SnapshotRow) error {
	return row.Scan(
		&snap.ID, &snap.OriginalFilename, &snap.ArchiveSha256, &snap.ContentHash,
		&snap.SnapshotPath, &snap.FileCount, &snap.TotalSize, &snap.ProjectID, &snap.CreatedAt, &snap.GitDiff)
}

func (s *Store) GetSnapshotByArchiveSha256(ctx context.Context, sha string) (*SnapshotRow, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+snapshotCols+`
FROM source_snapshots WHERE archive_sha256 = ?`, sha)
	var snap SnapshotRow
	if err := s.scanSnapshot(row, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

func (s *Store) GetSnapshot(ctx context.Context, id int64) (*SnapshotRow, error) {
	row := s.db.QueryRowContext(ctx, `
SELECT `+snapshotCols+`
FROM source_snapshots WHERE id = ?`, id)
	var snap SnapshotRow
	if err := s.scanSnapshot(row, &snap); err != nil {
		return nil, err
	}
	return &snap, nil
}

// ListSnapshots preserves the bounded legacy array API. Paged consumers use ListSnapshotsPage.
func (s *Store) ListSnapshots(ctx context.Context, limit int, projectID int64) ([]SnapshotRow, error) {
	if limit <= 0 || limit > 500 {
		limit = 50
	}
	f := SnapshotFilter{}
	if projectID > 0 {
		f.ProjectID = &projectID
	}
	where, args := s.snapshotConditions(f)
	rows, err := s.db.QueryContext(ctx, snapshotCatalog+"SELECT "+snapshotSelection+" FROM catalog WHERE "+where+" ORDER BY id DESC LIMIT ?", append(args, limit)...)
	if err != nil {
		return nil, err
	}
	return readSnapshots(rows)
}

// DeleteSnapshot 删除快照记录；仍有扫描引用时拒绝（产物链不断）。
func (s *Store) DeleteSnapshot(ctx context.Context, id int64) error {
	release, e := s.storageGate(ctx, false)
	if e != nil {
		return e
	}
	defer release()

	var refs int64
	if err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM scan_tasks WHERE source_snapshot_id = ?`, id).Scan(&refs); err != nil {
		return err
	}
	if refs > 0 {
		return fmt.Errorf("snapshot referenced by %d scan(s)", refs)
	}
	_, err := s.db.ExecContext(ctx, `DELETE FROM source_snapshots WHERE id = ?`, id)
	return err
}

// UpdateSnapshotProject 调整快照项目归属；projectID<=0 表示解除归属。
// 该快照下尚未归属项目的扫描一并继承（不覆盖已明确归属其它项目的任务）。
func (s *Store) UpdateSnapshotProject(ctx context.Context, snapshotID int64, projectID int64) error {
	tx, err := s.db.BeginTx(ctx, nil)
	if err != nil {
		return err
	}
	defer tx.Rollback()
	if projectID > 0 {
		if _, err := tx.ExecContext(ctx,
			`UPDATE source_snapshots SET project_id = ? WHERE id = ?`, projectID, snapshotID); err != nil {
			return err
		}
		if _, err := tx.ExecContext(ctx,
			`UPDATE scan_tasks SET project_id = ?
		 WHERE source_snapshot_id = ? AND (project_id IS NULL OR project_id = 0)`,
			projectID, snapshotID); err != nil {
			return err
		}
	} else {
		if _, err := tx.ExecContext(ctx,
			`UPDATE source_snapshots SET project_id = NULL WHERE id = ?`, snapshotID); err != nil {
			return err
		}
	}
	return tx.Commit()
}

type DashboardStats struct {
	Projects int64 `json:"projects"`
	Scans    int64 `json:"scans"`
	Running  int64 `json:"running"`
	Failed   int64 `json:"failed"`
	Critical int64 `json:"critical"`
	High     int64 `json:"high"`
	Medium   int64 `json:"medium"`
	Low      int64 `json:"low"`
	Info     int64 `json:"info"`
	// 风险视角：open/confirmed 的待处理漏洞
	OpenFindings   int64   `json:"openFindings"`
	CriticalOpen   int64   `json:"criticalOpen"`
	SuccessRate    float64 `json:"successRate"` // 0-100，completed+partial / 终态总扫描
	AvgDurationSec int64   `json:"avgDurationSec"`
}

func (s *Store) DashboardStats(ctx context.Context) (*DashboardStats, error) {
	var stats DashboardStats
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM projects`).Scan(&stats.Projects); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx, `SELECT COUNT(*) FROM scan_tasks WHERE NOT EXISTS(SELECT 1 FROM sdk_operations op WHERE op.task_id=scan_tasks.id) AND NOT EXISTS(SELECT 1 FROM component_operations op WHERE op.task_id=scan_tasks.id)`).Scan(&stats.Scans); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM scan_tasks
WHERE NOT EXISTS(SELECT 1 FROM sdk_operations op WHERE op.task_id=scan_tasks.id) AND NOT EXISTS(SELECT 1 FROM component_operations op WHERE op.task_id=scan_tasks.id) AND status IN ('pending','claimed','scanning','cancel_requested')`).Scan(&stats.Running); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM scan_tasks
WHERE NOT EXISTS(SELECT 1 FROM sdk_operations op WHERE op.task_id=scan_tasks.id) AND NOT EXISTS(SELECT 1 FROM component_operations op WHERE op.task_id=scan_tasks.id) AND status IN ('failed','canceled','timeout')`).Scan(&stats.Failed); err != nil {
		return nil, err
	}
	rows, err := s.db.QueryContext(ctx, `
SELECT f.severity, COUNT(*) FROM findings f
JOIN scan_tasks t ON t.id = f.scan_id
WHERE t.status IN ('completed','partial')
GROUP BY f.severity`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var sev string
		var n int64
		if err := rows.Scan(&sev, &n); err != nil {
			return nil, err
		}
		switch sev {
		case "critical":
			stats.Critical = n
		case "high":
			stats.High = n
		case "medium":
			stats.Medium = n
		case "low":
			stats.Low = n
		default:
			stats.Info = n
		}
	}
	// 待处理漏洞（open/confirmed）
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM findings f
JOIN scan_tasks t ON t.id = f.scan_id
WHERE t.status IN ('completed','partial') AND f.status IN ('open','confirmed')`).Scan(&stats.OpenFindings); err != nil {
		return nil, err
	}
	if err := s.db.QueryRowContext(ctx, `
SELECT COUNT(*) FROM findings f
JOIN scan_tasks t ON t.id = f.scan_id
WHERE t.status IN ('completed','partial') AND f.status IN ('open','confirmed')
  AND f.severity = 'critical'`).Scan(&stats.CriticalOpen); err != nil {
		return nil, err
	}
	// 扫描成功率与平均耗时（终态扫描）
	var totalTerminal, success int64
	if err := s.db.QueryRowContext(ctx, `
SELECT
  COUNT(*),
  COALESCE(SUM(CASE WHEN status IN ('completed','partial') THEN 1 ELSE 0 END), 0)
FROM scan_tasks
WHERE NOT EXISTS(SELECT 1 FROM sdk_operations op WHERE op.task_id=scan_tasks.id) AND NOT EXISTS(SELECT 1 FROM component_operations op WHERE op.task_id=scan_tasks.id) AND status IN ('completed','partial','failed','canceled','timeout')`).Scan(&totalTerminal, &success); err != nil {
		return nil, err
	}
	if totalTerminal > 0 {
		stats.SuccessRate = float64(success) / float64(totalTerminal) * 100
	}
	var avgSec sql.NullFloat64
	if err := s.db.QueryRowContext(ctx, `
SELECT AVG(`+s.elapsedSQL()+`)
FROM scan_tasks
WHERE NOT EXISTS(SELECT 1 FROM sdk_operations op WHERE op.task_id=scan_tasks.id) AND NOT EXISTS(SELECT 1 FROM component_operations op WHERE op.task_id=scan_tasks.id) AND status IN ('completed','partial')
  AND started_at IS NOT NULL AND finished_at IS NOT NULL`).Scan(&avgSec); err != nil {
		return nil, err
	}
	if avgSec.Valid && avgSec.Float64 > 0 {
		stats.AvgDurationSec = int64(avgSec.Float64)
	}
	return &stats, rows.Err()
}

// SeverityCounts 一组扫描/时间桶的严重度计数。
type SeverityCounts struct {
	Critical int64 `json:"critical"`
	High     int64 `json:"high"`
	Medium   int64 `json:"medium"`
	Low      int64 `json:"low"`
	Info     int64 `json:"info"`
}

func (sc *SeverityCounts) add(sev string, n int64) {
	switch sev {
	case "critical":
		sc.Critical += n
	case "high":
		sc.High += n
	case "medium":
		sc.Medium += n
	case "low":
		sc.Low += n
	default:
		sc.Info += n
	}
}

// ScanSeverityMap 每个扫描的严重度计数（概览最近记录表的 Critical/High/… 列）。
func (s *Store) ScanSeverityMap(ctx context.Context, scanIDs []int64) (map[int64]SeverityCounts, error) {
	out := make(map[int64]SeverityCounts, len(scanIDs))
	if len(scanIDs) == 0 {
		return out, nil
	}
	placeholders := make([]string, len(scanIDs))
	args := make([]any, len(scanIDs))
	for i, id := range scanIDs {
		placeholders[i] = "?"
		args[i] = id
	}
	rows, err := s.db.QueryContext(ctx,
		`SELECT scan_id, severity, COUNT(*) FROM findings
		 WHERE scan_id IN (`+strings.Join(placeholders, ",")+`)
		 GROUP BY scan_id, severity`, args...)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var scanID int64
		var sev string
		var n int64
		if err := rows.Scan(&scanID, &sev, &n); err != nil {
			return nil, err
		}
		sc := out[scanID]
		sc.add(sev, n)
		out[scanID] = sc
	}
	return out, rows.Err()
}

// TrendPoint 漏洞趋势图的一个日期桶。
type TrendPoint struct {
	Date string `json:"date"` // YYYY-MM-DD
	SeverityCounts
}

// DashboardTrend 最近 days 个有终态扫描的日期，按日聚合各严重度检出数（升序）。
func (s *Store) DashboardTrend(ctx context.Context, days int) ([]TrendPoint, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT substr(t.finished_at,1,10), f.severity, COUNT(*)
FROM findings f
JOIN scan_tasks t ON t.id = f.scan_id
WHERE t.status IN ('completed','partial') AND t.finished_at IS NOT NULL AND t.finished_at != ''
GROUP BY substr(t.finished_at,1,10), f.severity
ORDER BY substr(t.finished_at,1,10)`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	byDate := map[string]*TrendPoint{}
	var order []string
	for rows.Next() {
		var day, sev string
		var n int64
		if err := rows.Scan(&day, &sev, &n); err != nil {
			return nil, err
		}
		tp, ok := byDate[day]
		if !ok {
			tp = &TrendPoint{Date: day}
			byDate[day] = tp
			order = append(order, day)
		}
		tp.add(sev, n)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	if len(order) > days {
		order = order[len(order)-days:]
	}
	out := make([]TrendPoint, 0, len(order))
	for _, day := range order {
		out = append(out, *byDate[day])
	}
	return out, nil
}

// ProjectRiskCounts 项目风险分布（donut）：按各项目未处理漏洞的最高严重度分级。
type ProjectRiskCounts struct {
	High   int64 `json:"high"`   // 有 open/confirmed 的 critical
	Medium int64 `json:"medium"` // 最高为 high
	Low    int64 `json:"low"`    // 最高为 medium/low/info
	None   int64 `json:"none"`   // 无未处理漏洞
}

func (s *Store) DashboardProjectRisk(ctx context.Context) (*ProjectRiskCounts, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT p.id,
  COALESCE(SUM(CASE WHEN f.status IN ('open','confirmed') AND f.severity='critical' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN f.status IN ('open','confirmed') AND f.severity='high' THEN 1 ELSE 0 END), 0),
  COALESCE(SUM(CASE WHEN f.status IN ('open','confirmed') AND f.severity NOT IN ('critical','high') THEN 1 ELSE 0 END), 0)
FROM projects p
LEFT JOIN scan_tasks t ON t.project_id = p.id AND t.status IN ('completed','partial')
LEFT JOIN findings f ON f.scan_id = t.id
GROUP BY p.id`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out ProjectRiskCounts
	for rows.Next() {
		var id, c, h, rest int64
		if err := rows.Scan(&id, &c, &h, &rest); err != nil {
			return nil, err
		}
		switch {
		case c > 0:
			out.High++
		case h > 0:
			out.Medium++
		case rest > 0:
			out.Low++
		default:
			out.None++
		}
	}
	return &out, rows.Err()
}

// TerminalScanInfo 概览「较上次扫描」对比用的最小信息。
type TerminalScanInfo struct {
	ID           int64  `json:"id"`
	ScanNo       string `json:"scanNo"`
	FindingCount int    `json:"findingCount"`
	Status       string `json:"status"`
	Provider     string `json:"provider"`
	Model        string `json:"model"`
	BaseURL      string `json:"baseUrl"`
	DurationSec  int64  `json:"durationSec"`
}

// TerminalPair 最近两次终态扫描（新→旧），用于「较上次扫描」增量与引擎面板。
func (s *Store) TerminalPair(ctx context.Context) ([]TerminalScanInfo, error) {
	rows, err := s.db.QueryContext(ctx, `
SELECT id, scan_no, finding_count, status, provider, model, COALESCE(base_url, ''),
  CAST(`+s.elapsedSQL()+` AS INTEGER)
FROM scan_tasks
WHERE status IN ('completed','partial') AND finished_at IS NOT NULL AND finished_at != ''
ORDER BY finished_at DESC, id DESC LIMIT 2`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	var out []TerminalScanInfo
	for rows.Next() {
		var t TerminalScanInfo
		var dur sql.NullInt64
		if err := rows.Scan(&t.ID, &t.ScanNo, &t.FindingCount, &t.Status, &t.Provider, &t.Model, &t.BaseURL, &dur); err != nil {
			return nil, err
		}
		if dur.Valid {
			t.DurationSec = dur.Int64
		}
		out = append(out, t)
	}
	return out, rows.Err()
}

// CountScansByModel 某引擎（provider+model）的累计扫描次数（引擎面板用）。
func (s *Store) CountScansByModel(ctx context.Context, provider, model string) (int64, error) {
	var n int64
	err := s.db.QueryRowContext(ctx,
		`SELECT COUNT(*) FROM scan_tasks WHERE provider = ? AND model = ?`, provider, model).Scan(&n)
	return n, err
}

func nullIfEmpty(s string) any {
	if s == "" {
		return nil
	}
	return s
}

func nullIfEmptyZero(n int64) any {
	if n == 0 {
		return nil
	}
	return n
}

func defaultMode(m string) string {
	if m == "" {
		return "standard"
	}
	return m
}

func defaultTimeout(n int) int {
	if n <= 0 {
		return 60
	}
	return n
}

func CWEList(raw string) []string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var out []string
	if err := json.Unmarshal([]byte(raw), &out); err == nil {
		return out
	}
	return []string{raw}
}

func MustScanNo(id int64) string {
	return fmt.Sprintf("SCAN-%d", id)
}
