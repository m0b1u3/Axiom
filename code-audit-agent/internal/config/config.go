package config

import (
	"os"
	"path/filepath"
	"strconv"
	"time"
)

type Config struct {
	AgentID      string
	ServerURL    string
	AgentToken   string
	Concurrency  int
	ScannerDir   string
	NodePath     string
	DataDir      string
	PollInterval time.Duration
	Lease        time.Duration
	Once         bool
	HealthFile   string
}

func FromEnv() Config {
	scanner := getenv("AXIOM_SCANNER_DIR", filepath.Join("..", "code-audit-scanner"))
	data := getenv("AXIOM_DATA_DIR", filepath.Join(".", "data"))
	return Config{
		HealthFile:   os.Getenv("AXIOM_AGENT_HEALTH_FILE"),
		ServerURL:    getenv("AXIOM_SERVER_URL", "http://127.0.0.1:8080"),
		AgentToken:   os.Getenv("AXIOM_AGENT_TOKEN"),
		Concurrency:  positiveEnv("AXIOM_AGENT_CONCURRENCY", 2),
		AgentID:      getenv("AXIOM_AGENT_ID", "agent-local"),
		ScannerDir:   scanner,
		NodePath:     getenv("AXIOM_NODE", "node"),
		DataDir:      data,
		PollInterval: durationEnv("AXIOM_POLL_INTERVAL", 2*time.Second),
		Lease:        durationEnv("AXIOM_LEASE", 60*time.Second),
	}
}

func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}

func durationEnv(key string, fallback time.Duration) time.Duration {
	v := os.Getenv(key)
	if v == "" {
		return fallback
	}
	if n, err := strconv.Atoi(v); err == nil && n > 0 {
		return time.Duration(n) * time.Second
	}
	d, err := time.ParseDuration(v)
	if err != nil || d <= 0 {
		return fallback
	}
	return d
}

func positiveEnv(key string, fallback int) int {
	n, err := strconv.Atoi(os.Getenv(key))
	if err != nil || n < 1 || n > 64 {
		return fallback
	}
	return n
}
