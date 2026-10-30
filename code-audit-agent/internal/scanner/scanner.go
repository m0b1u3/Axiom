package scanner

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"axiom/contracts"
)

const (
	LayoutProgress   = "state/progress.json"
	LayoutEvents     = "logs/events.jsonl"
	LayoutNormalized = "normalized/result.json"
)

type Request struct {
	TokenBudget                *contracts.TokenBudgetAssignment
	DeepResumeContext          *contracts.DeepResumeContext
	DeepResume                 *contracts.DeepResumeAssignment
	SDKParameters              *contracts.SDKParametersAssignment
	SDKIdentity                *contracts.SDKTaskIdentity
	DataDir, AgentID           string
	ComponentScan              *contracts.ComponentScanRequest
	ComponentPlanning          *contracts.ComponentPlanningOperation
	GitTarget                  *contracts.GitDiffTarget
	SDKOperation               *contracts.SDKOperation
	ParentState                *contracts.SDKParentState
	ScanInput                  *contracts.ScanInput
	ObservationContractVersion int
	ResultContractVersion      int
	Job                        *contracts.ScannerJob
	Attempt                    int
	TaskID                     int64
	SourcePath                 string
	OutputPath                 string
	Provider                   string
	Model                      string
	BaseURL                    string
	ScanMode                   string
	// 路径子集扫描：相对 SourcePath 的白名单（透传 --include 逗号分隔）。
	// 空 = 全量扫描。
	IncludePaths         []string
	ScanFocus            []string
	ValidationMode       string
	BusinessContext      string
	DeepMaxDiscoveryRuns int
	DeepStopAfterNoNew   int
	TimeoutMinutes       int
	Workers              int
	Mock                 bool
	ScannerDir           string
	NodePath             string
	// 模型配置解密的 secret 注入（env 名 → 值），只传本次任务所需；
	// 为空则沿用环境变量（兼容旧部署）。
	EnvOverrides map[string]string
	// Server 配置层提供完整 Provider 接入描述：
	// 设置后 scanner 直接用，不再依赖自身硬编码注册表 → 新增 provider 零 scanner 改动。
	CredentialEnvName string
	WireAPI           string
	DisplayName       string
}

type Result struct {
	Full     *contracts.ScanResult `json:"-"`
	Process  contracts.ProcessExit `json:"-"`
	TaskID   int64                 `json:"taskId"`
	Status   string                `json:"status"`
	Findings []Finding             `json:"findings"`
	Error    *ResultError          `json:"error"`
	Coverage json.RawMessage       `json:"coverage"`
}

type ResultError struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type Finding struct {
	Fingerprint string   `json:"fingerprint"`
	Title       string   `json:"title"`
	Severity    string   `json:"severity"`
	Category    string   `json:"category"`
	CWE         []string `json:"cwe"`
	FilePath    string   `json:"filePath"`
	StartLine   *int     `json:"startLine"`
	EndLine     *int     `json:"endLine"`
	Description string   `json:"description"`
	Evidence    string   `json:"evidence"`
	Remediation string   `json:"remediation"`
	Confidence  *float64 `json:"confidence"`
	// 上游 finding 完整 JSON 透传（保留全部 Codex Security 字段）
	Raw json.RawMessage `json:"raw"`
}

type Progress struct {
	Deep           json.RawMessage `json:"deep,omitempty"`
	Worker         json.RawMessage `json:"worker,omitempty"`
	Reconnect      json.RawMessage `json:"reconnect,omitempty"`
	TaskID         int64           `json:"taskId"`
	Phase          string          `json:"phase"`
	FilesCompleted *int            `json:"filesCompleted"`
	FilesTotal     *int            `json:"filesTotal"`
	Activity       string          `json:"activity"`
	CostUSD        *float64        `json:"costUsd"`
	UpdatedAt      string          `json:"updatedAt"`
}

func Run(ctx context.Context, req Request) (*Result, error) {
	var args []string
	if req.Job != nil {
		jobPath, err := prepareJob(ctx, req)
		if err != nil {
			return nil, err
		}
		args = append(entryArguments(req.ScannerDir), "--job", jobPath)
		for _, sidecar := range []struct {
			value      any
			kind       contracts.Kind
			name, flag string
		}{{req.DeepResumeContext, contracts.KindDeepResumeContext, "deep-resume-context", "--deep-resume-context"}, {req.DeepResume, contracts.KindDeepResumeAssignment, "deep-resume", "--deep-resume"}} {
			if (sidecar.name == "deep-resume-context" && req.DeepResumeContext == nil) || (sidecar.name == "deep-resume" && req.DeepResume == nil) {
				continue
			}
			file, e := prepareDeepSidecar(req, sidecar.value, sidecar.kind, sidecar.name)
			if e != nil {
				return nil, e
			}
			args = append(args, sidecar.flag, file)
		}
		if req.TokenBudget != nil {
			caps, e := ProbeContracts(ctx, req.ScannerDir, req.NodePath)
			if e != nil || caps.TokenBudgetVersion != 1 {
				return nil, errors.New("Scanner 不支持按冻结单价控制预算，模型尚未启动")
			}
			file, e := prepareDeepSidecar(req, req.TokenBudget, contracts.KindTokenBudgetAssignment, "token-budget")
			if e != nil {
				return nil, e
			}
			args = append(args, "--token-budget", file)
		}
		if req.SDKParameters != nil {
			file, e := prepareSDKParameters(ctx, req)
			if e != nil {
				return nil, e
			}
			args = append(args, "--sdk-parameters", file)
		}
		if req.SDKIdentity != nil {
			caps, e := ProbeContracts(ctx, req.ScannerDir, req.NodePath)
			if e != nil || caps.IdentityVersion != 1 {
				return nil, errors.New("Scanner does not support SDK identity sessions")
			}
			file, _, e := prepareTaskIdentity(req)
			if e != nil {
				return nil, e
			}
			args = append(args, "--identity", file)
		}
		if req.Mock {
			args = append(args, "--mock")
		}
	} else {
		args = append(entryArguments(req.ScannerDir), []string{
			"--task-id", strconv.FormatInt(req.TaskID, 10),
			"--source", req.SourcePath,
			"--output", req.OutputPath,
			"--provider", req.Provider,
			"--model", req.Model,
			"--mode", defaultMode(req.ScanMode),
			"--timeout", strconv.Itoa(defaultTimeout(req.TimeoutMinutes)),
		}...)
		if req.DeepMaxDiscoveryRuns > 0 {
			args = append(args, "--deep-max-runs", strconv.Itoa(req.DeepMaxDiscoveryRuns))
		}
		if req.DeepStopAfterNoNew > 0 {
			args = append(args, "--deep-stop-no-new", strconv.Itoa(req.DeepStopAfterNoNew))
		}
		if req.Workers > 0 {
			args = append(args, "--workers", strconv.Itoa(req.Workers))
		}
		if req.BaseURL != "" {
			args = append(args, "--base-url", req.BaseURL)
		}
		if req.CredentialEnvName != "" {
			args = append(args, "--credential-env", req.CredentialEnvName)
		}
		if req.WireAPI != "" {
			args = append(args, "--wire-api", req.WireAPI)
		}
		if req.DisplayName != "" {
			args = append(args, "--display-name", req.DisplayName)
		}
		if len(req.IncludePaths) > 0 {
			args = append(args, "--include", strings.Join(req.IncludePaths, ","))
		}
		if len(req.ScanFocus) > 0 {
			args = append(args, "--focus", strings.Join(req.ScanFocus, ","))
		}
		if req.Mock {
			args = append(args, "--mock")
		}
	}
	if err := os.MkdirAll(req.OutputPath, 0o700); err != nil {
		return nil, err
	}

	cmd := exec.CommandContext(ctx, req.NodePath, args...)
	configureProcessGroup(cmd)
	cmd.Dir = req.ScannerDir
	credentialEnv := req.CredentialEnvName
	if req.Job != nil {
		credentialEnv = req.Job.Model.CredentialEnv
	}
	cmd.Env = childEnvForCredential(req.Provider, credentialEnv, req.Mock, req.EnvOverrides)
	if req.Job != nil {
		cmd.Env = jobEnvironment(cmd.Env, credentialEnv)
	}
	if req.Job == nil {
		cmd.Env = append(cmd.Env, "AXIOM_VALIDATION_MODE="+req.ValidationMode, "AXIOM_BUSINESS_CONTEXT="+req.BusinessContext)
	}
	// The SDK writes config.toml/auth and a workbench SQLite database into this directory.
	// A process per attempt plus a private state directory avoids cross-provider races.
	isolated := cmd.Env[:0]
	for _, kv := range cmd.Env {
		if !strings.HasPrefix(kv, "CODEX_SECURITY_STATE_DIR=") {
			isolated = append(isolated, kv)
		}
	}
	state := filepath.Join(req.OutputPath, "runtime", "sdk")
	if req.DeepResume != nil {
		var e error
		state, e = deepState(req.DataDir, req.OutputPath, req.DeepResume.Inspection.Origin, nil)
		if e != nil {
			return nil, e
		}
	}
	cmd.Env = append(isolated, "CODEX_SECURITY_STATE_DIR="+state)
	if req.SDKIdentity != nil {
		_, home, e := prepareTaskIdentity(req)
		if e != nil {
			return nil, e
		}
		originAttempt := req.Attempt
		if req.DeepResume != nil {
			originAttempt = int(req.DeepResume.Inspection.Origin.Attempt)
		}
		root := filepath.Join(home, "executions", fmt.Sprint(req.TaskID)+"-"+fmt.Sprint(originAttempt))
		if e = os.MkdirAll(root, 0700); e != nil {
			return nil, e
		}
		if req.DeepResumeContext == nil && req.DeepResume == nil {
			defer os.RemoveAll(root)
		}
		next := jobEnvironment(cmd.Env, "")
		clean := next[:0]
		for _, kv := range next {
			key, _, _ := strings.Cut(kv, "=")
			if key != "CODEX_SECURITY_STATE_DIR" {
				clean = append(clean, kv)
			}
		}
		cmd.Env = append(clean, "CODEX_SECURITY_STATE_DIR="+filepath.Join(root, "sdk"), "CODEX_HOME="+filepath.Join(home, "sdk", "codex-home"), "AXIOM_SDK_IDENTITY_HOME="+home)
		if req.SDKIdentity.Identity.Kind == "api_key" {
			key, e := os.ReadFile(filepath.Join(home, "selected-api-key"))
			if e != nil || len(key) == 0 {
				return nil, errors.New("SDK API Key session unavailable")
			}
			cmd.Env = append(cmd.Env, "OPENAI_API_KEY="+string(key))
		}
	}
	// Bound the Scanner to this Agent even when the Agent receives SIGKILL and
	// cannot run its context cancellation handler. Never accept ambient values.
	cmd.Env = scannerParentEnvironment(cmd.Env)
	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, err
	}
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		return nil, err
	}
	drainJSON(stdout)
	waitErr := cmd.Wait()
	if req.ResultContractVersion == 1 {
		expected := contracts.ExecutionRef{TaskID: contracts.TaskID(strconv.FormatInt(req.TaskID, 10)), Attempt: int64(req.Attempt)}
		full, readErr := ReadCommitted(req.OutputPath, expected)
		process := ProcessOutcome(cmd.ProcessState, full != nil)
		out := &Result{Full: full, Process: process}
		if readErr != nil {
			return out, fmt.Errorf("%w: %v", ErrInvalidResult, readErr)
		}
		return out, nil
	}

	result, readErr := ReadNormalized(req.OutputPath)
	if readErr != nil {
		if waitErr != nil {
			return nil, fmt.Errorf("scanner exit: %w; also %v", waitErr, readErr)
		}
		return nil, readErr
	}
	return result, nil
}

func ReadNormalized(outputPath string) (*Result, error) {
	raw, err := os.ReadFile(filepath.Join(outputPath, LayoutNormalized))
	if err != nil {
		return nil, err
	}
	raw, err = numericIdentityDocument(raw)
	if err != nil {
		return nil, err
	}
	var result Result
	if err := json.Unmarshal(raw, &result); err != nil {
		return nil, err
	}
	return &result, nil
}

func ReadProgress(outputPath string) (*Progress, error) {
	raw, err := os.ReadFile(filepath.Join(outputPath, LayoutProgress))
	if err != nil {
		return nil, err
	}
	raw, err = numericIdentityDocument(raw)
	if err != nil {
		return nil, err
	}
	var p Progress
	if err := json.Unmarshal(raw, &p); err != nil {
		return nil, err
	}
	return &p, nil
}

// New input uses string identities. Existing result/progress readers accept both
// representations until their versioned contracts are integrated separately.
func numericIdentityDocument(raw []byte) ([]byte, error) {
	var object map[string]json.RawMessage
	if err := json.Unmarshal(raw, &object); err != nil {
		return nil, err
	}
	var text string
	if value, ok := object["taskId"]; ok && len(value) > 0 && value[0] == '"' {
		if err := json.Unmarshal(value, &text); err != nil {
			return nil, err
		}
		id, err := strconv.ParseInt(text, 10, 64)
		if err != nil || id < 1 {
			return nil, fmt.Errorf("invalid scanner task identity")
		}
		object["taskId"], _ = json.Marshal(id)
		return json.Marshal(object)
	}
	return raw, nil
}

func childEnvForCredential(provider, credential string, mock bool, overrides map[string]string) []string {
	env := []string{}
	for _, kv := range os.Environ() {
		key, _, _ := strings.Cut(kv, "=")
		switch key {
		case "AXIOM_DATABASE_URL", "AXIOM_TEST_DATABASE_URL", "AXIOM_AGENT_TOKEN", "AXIOM_MASTER_KEY", "AXIOM_JWT_SECRET", "AXIOM_ADMIN_PASSWORD":
			continue
		}
		env = append(env, kv)
	}
	if mock {
		return env
	}
	target := credential
	filtered := make([]string, 0, len(env)+1)
	for _, kv := range env {
		// 被注入覆盖的同名 key：跳过，稍后追加覆盖值
		if strings.HasPrefix(kv, target+"=") {
			if _, ok := overrides[target]; ok {
				continue
			}
		}
		if stripAmbient(kv, provider) && !strings.HasPrefix(kv, target+"=") {
			continue
		}
		filtered = append(filtered, kv)
	}
	if v, ok := overrides[target]; ok {
		filtered = append(filtered, target+"="+v)
	}
	return filtered
}

func stripAmbient(kv, provider string) bool {
	if provider == "openai" {
		return false
	}
	keys := []string{"OPENAI_API_KEY=", "CODEX_API_KEY=", "CODEX_HOME=", "CHATGPT_ACCESS_TOKEN=", "CHATGPT_ACCOUNT_ID="}
	for _, p := range keys {
		if len(kv) >= len(p) && kv[:len(p)] == p {
			return true
		}
	}
	return false
}

func drainJSON(r io.Reader) {
	// stdout is diagnostic only; the bounded committed file is authoritative.
	// A line-size Scanner can stop reading while Node is still writing and
	// deadlock Wait on otherwise valid large SDK output. Drain in constant memory.
	_, _ = io.Copy(io.Discard, r)
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

func HeartbeatLoop(ctx context.Context, outputPath string, every time.Duration, fn func(*Progress)) {
	t := time.NewTicker(every)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-t.C:
			p, err := ReadProgress(outputPath)
			if err != nil {
				fn(nil)
				continue
			}
			fn(p)
		}
	}
}
