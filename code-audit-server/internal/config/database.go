package config

import "path/filepath"

func DatabaseSource(getenv func(string) string) string {
	if v := getenv("AXIOM_DATABASE_URL"); v != "" {
		return v
	}
	return LegacyDatabasePath(getenv)
}
func LegacyDatabasePath(getenv func(string) string) string {
	if v := getenv("AXIOM_AGENT_DB"); v != "" {
		return v
	}
	return filepath.Join("..", "code-audit-agent", "data", "agent.db")
}
