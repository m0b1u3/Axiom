package worker

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestAgentHealthRequiresRecentSuccessfulPresence(t *testing.T) {
	file := filepath.Join(t.TempDir(), "health.json")
	if CheckHealth(file) == nil {
		t.Fatal("missing presence accepted")
	}
	if e := writeHealth(file); e != nil {
		t.Fatal(e)
	}
	if e := CheckHealth(file); e != nil {
		t.Fatal(e)
	}
	for _, offset := range []time.Duration{-46 * time.Second, time.Minute} {
		data, _ := json.Marshal(healthStamp{PID: os.Getpid(), ReportedAt: time.Now().Add(offset)})
		if e := os.WriteFile(file, data, 0600); e != nil {
			t.Fatal(e)
		}
		if CheckHealth(file) == nil {
			t.Fatal("stale or future presence accepted")
		}
	}
}
