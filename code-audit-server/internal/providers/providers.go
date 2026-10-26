// Package providers owns the Server model configuration presets. The selected
// connection is frozen into a contract Assignment; Agent and Scanner do not
// load this registry.
package providers

import (
	_ "embed"
	"encoding/json"
	"sort"
	"sync"
)

// Preset 一个模型供应商的接入描述。
// 只描述「接法」（端点建议、密钥环境变量名、wire 协议），
// 型号清单仅作前端表单占位提示 —— 实际 baseUrl/model/apiKey 全部由用户填写。
type Preset struct {
	Key           string   `json:"key"`           // 稳定标识（scan_tasks.provider / ScanTask.provider）
	Label         string   `json:"label"`         // 展示名
	BaseURL       string   `json:"baseUrl"`       // 表单预填建议（用户可改）；空 = SDK 默认（openai）或必填（custom）
	CredentialEnv string   `json:"credentialEnv"` // API Key 注入子进程用的环境变量名
	WireAPI       string   `json:"wireApi"`       // Codex wire_api：responses only
	Icon          string   `json:"icon"`          // 品牌图标名（cc-switch 同款；空 = 前端用通用图标）
	IconColor     string   `json:"iconColor"`     // 品牌色（currentColor 图标需要）
	Website       string   `json:"website"`       // 官网链接（表单预填）
	KeyURL        string   `json:"keyUrl"`        // 「获取 API Key」跳转链接
	ModelsURL     string   `json:"modelsUrl"`     // 拉取模型清单的端点（GET，OpenAI /models 兼容）
	DefaultModel  string   `json:"defaultModel"`  // 表单占位提示（例如 glm-4.6），不是默认值
	ModelExamples string   `json:"modelExamples"` // 模型字段示例文案（前端 hint），如 "qwen-plus、qwen-turbo"
	Models        []string `json:"models"`        // 常见型号提示（前端 datalist），不强制
}

//go:embed providers.json
var registryJSON []byte

var (
	once    sync.Once
	byKey   map[string]Preset
	ordered []Preset
)

func load() {
	once.Do(func() {
		if err := json.Unmarshal(registryJSON, &ordered); err != nil {
			panic("providers: invalid embedded registry: " + err.Error())
		}
		byKey = make(map[string]Preset, len(ordered))
		for _, p := range ordered {
			byKey[p.Key] = p
		}
	})
}

// All 返回全部预设（顺序即 providers.json 顺序）。
func All() []Preset {
	load()
	out := make([]Preset, len(ordered))
	copy(out, ordered)
	return out
}

// Lookup 按 key 查预设。
func Lookup(key string) (Preset, bool) {
	load()
	p, ok := byKey[key]
	return p, ok
}

// Keys 返回已注册 key 列表（排序，测试/日志用）。
func Keys() []string {
	load()
	keys := make([]string, 0, len(byKey))
	for k := range byKey {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	return keys
}

// WireAPI 该 provider 的默认上游协议（responses only）。
func WireAPI(key string) string {
	if p, ok := Lookup(key); ok && p.WireAPI != "" {
		return p.WireAPI
	}
	return "responses"
}

// CredentialEnv 该 provider 的 key 注入环境变量名。
// 未注册 provider 兜底 OPENAI_API_KEY（兼容旧任务/旧部署）。
func CredentialEnv(key string) string {
	if p, ok := Lookup(key); ok && p.CredentialEnv != "" {
		return p.CredentialEnv
	}
	return "OPENAI_API_KEY"
}
