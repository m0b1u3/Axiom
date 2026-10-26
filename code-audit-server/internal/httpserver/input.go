package httpserver

import (
	"axiom/code-audit-server/internal/providers"
	"axiom/code-audit-server/internal/store"
	"axiom/contracts"
	"bytes"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

type createScanRequest struct {
	SDKIdentityID        string               `json:"sdkIdentityId"`
	Operation            string               `json:"operation"`
	ParentTaskID         int64                `json:"parentTaskId"`
	ScanInput            *contracts.ScanInput `json:"scanInput"`
	KnowledgeVersionIDs  []int64              `json:"knowledgeVersionIds"`
	SourcePath           string               `json:"sourcePath"`
	SourceSnapshotID     int64                `json:"sourceSnapshotId"`
	ModelConfigID        int64                `json:"modelConfigId"`
	ProjectID            int64                `json:"projectId"`
	Provider             string               `json:"provider"`
	Model                string               `json:"model"`
	BaseURL              string               `json:"baseUrl"`
	ScanMode             string               `json:"scanMode"`
	IncludePaths         []string             `json:"includePaths"`
	ScanFocus            []string             `json:"scanFocus"`
	ValidationMode       string               `json:"validationMode"`
	BusinessContext      string               `json:"businessContext"`
	TimeoutMinutes       int                  `json:"timeoutMinutes"`
	DeepMaxDiscoveryRuns *int                 `json:"deepMaxDiscoveryRuns"`
	DeepStopAfterNoNew   *int                 `json:"deepStopAfterNoNew"`
	MaxCostUSD           *float64             `json:"maxCostUsd"`
	MaxTotalCostUSD      *float64             `json:"maxTotalCostUsd"`
	Workers              *int                 `json:"workers"`
}

func decodeScanRequest(w http.ResponseWriter, r *http.Request) (createScanRequest, error) {
	var v createScanRequest
	defer r.Body.Close()
	raw, err := io.ReadAll(http.MaxBytesReader(w, r.Body, contracts.MaxDocumentBytes))
	if err != nil {
		return v, errors.New("扫描配置超过大小限制")
	}
	if err = contracts.CheckJSONText(raw); err != nil {
		return v, errors.New("扫描配置不是有效 JSON")
	}
	d := json.NewDecoder(bytes.NewReader(raw))
	d.DisallowUnknownFields()
	if err = d.Decode(&v); err != nil {
		return v, errors.New("扫描配置包含未知字段或字段类型不正确")
	}
	return v, nil
}

func normalizePolicy(v createScanRequest) (contracts.ScanPolicy, error) {
	if v.MaxTotalCostUSD != nil {
		if _, e := store.BudgetLimitMicros(*v.MaxTotalCostUSD); e != nil {
			return contracts.ScanPolicy{}, e
		}
		if v.MaxCostUSD != nil {
			if _, e := store.BudgetLimitMicros(*v.MaxCostUSD); e != nil {
				return contracts.ScanPolicy{}, e
			}
		}
	}
	if v.ScanMode == "" {
		v.ScanMode = string(contracts.DefaultScanMode)
	}
	if v.ValidationMode == "" {
		v.ValidationMode = string(contracts.DefaultValidationMode)
	}
	if v.TimeoutMinutes == 0 {
		v.TimeoutMinutes = contracts.DefaultScanSeconds / 60
	}
	if v.TimeoutMinutes < 1 || v.TimeoutMinutes > 600 {
		return contracts.ScanPolicy{}, errors.New("扫描时限须为 1–600 分钟的整数")
	}
	if v.MaxCostUSD != nil && (math.IsNaN(*v.MaxCostUSD) || math.IsInf(*v.MaxCostUSD, 0) || *v.MaxCostUSD <= 0 || *v.MaxCostUSD > 9007199254740991) {
		return contracts.ScanPolicy{}, errors.New("费用预算须为有限的正数；不设置预算时请留空")
	}
	include, err := normalizeScope(v.IncludePaths)
	if err != nil {
		return contracts.ScanPolicy{}, err
	}
	p := contracts.ScanPolicy{Mode: contracts.ScanMode(v.ScanMode), TimeBudget: contracts.TimeBudget{ScanSeconds: int64(v.TimeoutMinutes) * 60}, IncludePaths: include, Focus: []contracts.Focus{}, Validation: contracts.ValidationPolicy{Mode: contracts.ValidationMode(v.ValidationMode)}}
	if v.ScanMode == "deep" {
		p.TimeBudget.FinalizationGraceSeconds = contracts.DeepFinalizationGraceSeconds
	}
	for _, key := range v.ScanFocus {
		p.Focus = append(p.Focus, contracts.Focus(key))
	}
	if v.ValidationMode == "exploitability" {
		p.Validation.BusinessContext = contracts.Some(v.BusinessContext)
	} else if v.BusinessContext != "" {
		return p, errors.New("业务背景需要启用加强可利用性验证")
	}
	if v.MaxCostUSD != nil {
		p.MaxCostUSD = contracts.Some(*v.MaxCostUSD)
	}
	if v.Workers != nil {
		p.Workers = contracts.Some(int64(*v.Workers))
	}
	if v.DeepMaxDiscoveryRuns != nil {
		p.DeepMaxDiscoveryRuns = contracts.Some(int64(*v.DeepMaxDiscoveryRuns))
	}
	if v.DeepStopAfterNoNew != nil {
		p.DeepStopAfterNoNew = contracts.Some(int64(*v.DeepStopAfterNoNew))
	}
	raw, err := json.Marshal(p)
	if err != nil {
		return p, err
	}
	if err = contracts.ValidateJSON(contracts.KindScanPolicy, raw, nil); err != nil {
		return p, errors.New("扫描模式、验证方式、路径或深度参数不符合要求；Worker 和发现轮数须为正安全整数，且仅适用于深度扫描")
	}
	return p, nil
}

func normalizeScope(values []string) ([]contracts.RelativePath, error) {
	out := []contracts.RelativePath{}
	seen := map[string]bool{}
	for _, raw := range values {
		if raw == "" || strings.ContainsRune(raw, 0) || strings.HasPrefix(raw, "/") || strings.HasPrefix(raw, "\\") || (len(raw) >= 2 && raw[1] == ':') {
			return nil, errors.New("扫描范围必须是源码内的相对路径，不能使用绝对路径")
		}
		normal := strings.ReplaceAll(raw, "\\", "/")
		for _, part := range strings.Split(normal, "/") {
			if part == ".." || part == "." {
				return nil, errors.New("扫描范围不能包含 . 或 .. 路径段")
			}
		}
		normal = path.Clean(normal)
		if seen[normal] {
			return nil, errors.New("扫描范围包含重复路径")
		}
		seen[normal] = true
		out = append(out, contracts.RelativePath(normal))
	}
	return out, nil
}

func resolveModel(v createScanRequest, cfg *store.ModelConfigRow) (contracts.ResolvedModel, error) {
	provider, model, baseURL, wire := v.Provider, v.Model, v.BaseURL, ""
	id := int64(0)
	if cfg != nil {
		provider, model, baseURL, wire, id = cfg.Provider, cfg.Model, cfg.BaseURL, cfg.WireAPI, cfg.ID
	}
	if provider == "" {
		provider = "openai"
	}
	if model == "" {
		model = "gpt-5.6-sol"
	}
	preset, ok := providers.Lookup(provider)
	if !ok {
		return contracts.ResolvedModel{}, errors.New("模型供应商未注册，请通过网页配置受支持的连接")
	}
	if wire == "" {
		wire = preset.WireAPI
	}
	if wire != "responses" {
		return contracts.ResolvedModel{}, errors.New("Chat 协议适配已移除；请更新为支持 Responses 的模型配置，并创建新任务")
	}
	if baseURL == "" && provider != "openai" {
		baseURL = preset.BaseURL
	}
	connection := contracts.ResolvedModelConnectionTypeOpenaiCompatible
	if provider == "openai" && baseURL == "" && wire == "responses" {
		connection = contracts.ResolvedModelConnectionTypeOpenai
	}
	m := contracts.ResolvedModel{ProviderID: contracts.Identifier(provider), ConnectionType: connection, Model: model, WireAPI: contracts.ResolvedModelWireAPI(wire), CredentialEnv: preset.CredentialEnv, ConfigurationRevision: "pending"}
	if baseURL != "" {
		m.BaseURL = contracts.Some(baseURL)
	}
	raw, _ := json.Marshal(m)
	if cfg != nil && cfg.ReasoningEffort != "" {
		raw = append(raw, []byte("\x00reasoningEffort="+cfg.ReasoningEffort)...)
	}
	hash := sha256.Sum256(raw)
	m.ConfigurationRevision = contracts.Identifier(fmt.Sprintf("config-%d-%s", id, hex.EncodeToString(hash[:])[:16]))
	raw, _ = json.Marshal(m)
	if err := contracts.ValidateJSON(contracts.KindResolvedModel, raw, nil); err != nil {
		return m, errors.New("模型连接配置不完整，请检查模型名、接口地址和协议")
	}
	return m, nil
}

func (s *Server) prepareScan(r *http.Request, v createScanRequest) (store.Task, error) {
	if v.Operation != "" && v.Operation != "preflight" {
		return store.Task{}, errors.New("此入口只支持原生预检或扫描")
	}
	if v.SourceSnapshotID < 0 || v.ModelConfigID < 0 || v.ProjectID < 0 {
		return store.Task{}, errors.New("项目、快照和模型配置编号必须是正整数")
	}
	p, err := normalizePolicy(v)
	if err != nil {
		return store.Task{}, err
	}
	var cfg *store.ModelConfigRow
	if v.ModelConfigID > 0 {
		cfg, err = s.Store.GetModelConfig(r.Context(), v.ModelConfigID)
	} else {
		cfg, err = s.Store.GetEnabledModelConfig(r.Context())
		if errors.Is(err, sql.ErrNoRows) {
			err = nil
		}
	}
	if err != nil {
		return store.Task{}, errors.New("模型配置不可用")
	}
	if cfg != nil {
		v.ModelConfigID = cfg.ID
		if cfg.ApiKeyEncrypted == "" && v.SDKIdentityID == "" {
			return store.Task{}, errors.New("此模型需要选择已认证的 SDK 执行身份")
		}
	}
	m, err := resolveModel(v, cfg)
	if err != nil {
		return store.Task{}, err
	}
	t := store.Task{ScanNo: "SCAN-" + strconv.FormatInt(time.Now().UnixNano(), 36), SourcePath: v.SourcePath, SourceSnapshotID: v.SourceSnapshotID, ModelConfigID: v.ModelConfigID, ProjectID: v.ProjectID,
		Provider: string(m.ProviderID), Model: m.Model, BaseURL: m.BaseURL.Value, ScanMode: string(p.Mode), TimeoutMinutes: int(p.TimeBudget.ScanSeconds / 60), MaxCostUSD: v.MaxCostUSD, Workers: v.Workers,
		ValidationMode: string(p.Validation.Mode), BusinessContext: v.BusinessContext, DeepMaxDiscoveryRuns: int(p.DeepMaxDiscoveryRuns.Value), DeepStopAfterNoNew: int(p.DeepStopAfterNoNew.Value)}
	if v.SourceSnapshotID > 0 {
		t.BudgetAccounting = true
		t.MaxTotalCostUSD = v.MaxTotalCostUSD
		snap, e := s.Store.GetSnapshot(r.Context(), v.SourceSnapshotID)
		if e != nil {
			return t, errors.New("代码快照不存在，请重新上传源码")
		}
		t.SourcePath, t.SourceContentHash, t.InputContractVersion = snap.SnapshotPath, snap.ContentHash, int(contracts.Version)
		t.ResultContractVersion = 1
		t.ObservationContractVersion = 1
		absolute, e := filepath.Abs(t.SourcePath)
		if e != nil {
			return t, errors.New("代码快照目录不可用")
		}
		t.SourcePath = absolute
		if t.ProjectID == 0 {
			t.ProjectID = snap.ProjectID
		}
	} else if v.MaxCostUSD != nil || v.MaxTotalCostUSD != nil || v.Workers != nil {
		return t, errors.New("费用预算和 worker 配置需要代码快照，请先在网页上传源码")
	}
	if strings.TrimSpace(t.SourcePath) == "" {
		return t, errors.New("请上传代码快照或提供源码目录")
	}
	if t.ProjectID > 0 {
		if _, e := s.Store.GetProject(r.Context(), t.ProjectID); e != nil {
			return t, errors.New("项目不存在")
		}
	}
	if v.ScanInput != nil || len(v.KnowledgeVersionIDs) > 0 || v.ParentTaskID != 0 {
		if t.SourceSnapshotID <= 0 {
			return t, errors.New("高级输入与知识文档需要不可变代码快照")
		}
		input := v.ScanInput
		if input == nil {
			input = &contracts.ScanInput{SchemaVersion: 1, Knowledge: []contracts.KnowledgeDocument{}}
		}
		if len(input.Knowledge) != 0 {
			return t, errors.New("知识文档只能引用 Web 管理的文档版本，不能直接传入正文")
		}
		if v.ParentTaskID < 0 {
			return t, errors.New("父扫描任务编号无效")
		}
		if v.ParentTaskID > 0 {
			state, parent, e := s.loadParentState(r, v.ParentTaskID)
			if e != nil {
				return t, e
			}
			if parent.SourceSnapshotID != t.SourceSnapshotID || state.SourceHash != t.SourceContentHash {
				return t, errors.New("父扫描必须引用同一份不可变源码快照")
			}
			if input.ParentScanID.Present && input.ParentScanID.Value != state.ScanID {
				return t, errors.New("父 SDK Scan ID 与选定父任务不匹配")
			}
			if input.WorkflowID.Present && parent.ScanInput != nil && parent.ScanInput.WorkflowID.Present && input.WorkflowID.Value == parent.ScanInput.WorkflowID.Value {
				return t, errors.New("新扫描需要使用与父任务不同的 SDK 工作流 ID")
			}
			input.ParentScanID = contracts.Some(state.ScanID)
			t.ParentState = state
		} else if input.ParentScanID.Present {
			return t, errors.New("父 SDK Scan ID 需要通过 Web 选择有效的父扫描任务")
		}
		input.Knowledge, err = s.Store.ResolveKnowledge(r.Context(), v.KnowledgeVersionIDs)
		if err != nil {
			return t, err
		}
		if err = store.ValidateScanInput(input, t.ScanMode, t.ValidationMode); err != nil {
			return t, err
		}
		t.ScanInput = input
		t.InputContractVersion = contracts.ScanInputVersion
	}
	if t.SourceSnapshotID > 0 {
		target, e := s.Store.GitSnapshotTarget(r.Context(), t.SourceSnapshotID)
		if e != nil {
			return t, errors.New("Git 快照元数据不可读取")
		}
		if target != nil {
			if p.Mode != "standard" || len(p.IncludePaths) != 0 {
				return t, errors.New("Git 提交差异只支持标准扫描，不能叠加路径范围")
			}
			t.GitTarget = target
			t.InputContractVersion = contracts.GitDiffInputVersion
			if t.ScanInput == nil {
				t.ScanInput = &contracts.ScanInput{SchemaVersion: 1, Knowledge: []contracts.KnowledgeDocument{}}
			}
		}
	}
	modelJSON, _ := json.Marshal(m)
	policyJSON, _ := json.Marshal(p)
	includeJSON, _ := json.Marshal(p.IncludePaths)
	focusJSON, _ := json.Marshal(p.Focus)
	t.ModelSnapshotJSON, t.PolicySnapshotJSON, t.IncludePaths, t.ScanFocus = string(modelJSON), string(policyJSON), string(includeJSON), string(focusJSON)
	if cfg != nil {
		if cfg.ReasoningEffort != "" {
			t.SDKParameters = &contracts.SDKScanParameters{SchemaVersion: 1, ReasoningEffort: contracts.Some(contracts.ReasoningEffort(cfg.ReasoningEffort))}
		}
		t.CredentialSnapshotEncrypted = cfg.ApiKeyEncrypted
	}
	if v.SDKIdentityID != "" {
		identityID, err := strconv.ParseInt(v.SDKIdentityID, 10, 64)
		if err != nil || identityID < 1 {
			return t, store.ErrSDKIdentity
		}
		if t.SourceSnapshotID <= 0 || m.ProviderID != "openai" || m.ConnectionType != "openai" || (m.BaseURL.Present && m.BaseURL.Value != "https://api.openai.com/v1") || m.WireAPI != "responses" {
			return t, errors.New("SDK 执行身份仅用于不可变源码的原生 OpenAI Responses 模型，请选择配套模型配置")
		}
		identity, e := s.Store.SDKIdentity(r.Context(), identityID)
		if e != nil || identity.State != "authenticated" || identity.Authenticated == nil || !*identity.Authenticated || identity.CleanupPending {
			return t, store.ErrSDKIdentity
		}
		t.SDKIdentity = &identity.Identity
		t.CredentialSnapshotEncrypted = ""
	}
	return t, nil
}
