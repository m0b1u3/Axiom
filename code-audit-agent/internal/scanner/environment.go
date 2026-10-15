package scanner

import (
	"strings"
)

// A Web job uses its selected credential binding, never another ambient login.
func jobEnvironment(env []string, target string) []string {
	credentials := map[string]bool{"CODEX_API_KEY": true, "CHATGPT_ACCESS_TOKEN": true, "CHATGPT_ACCOUNT_ID": true, "CODEX_HOME": true}
	out := make([]string, 0, len(env))
	for _, entry := range env {
		key, _, _ := strings.Cut(entry, "=")
		if (credentials[key] || strings.HasSuffix(key, "_API_KEY")) && key != target {
			continue
		}
		out = append(out, entry)
	}
	return out
}
