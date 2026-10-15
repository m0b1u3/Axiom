package worker

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"time"
)

type healthStamp struct {
	PID        int       `json:"pid"`
	ReportedAt time.Time `json:"reportedAt"`
}

func writeHealth(path string) error {
	if e := os.MkdirAll(filepath.Dir(path), 0700); e != nil {
		return e
	}
	data, e := json.Marshal(healthStamp{PID: os.Getpid(), ReportedAt: time.Now().UTC()})
	if e != nil {
		return e
	}
	tmp, e := os.CreateTemp(filepath.Dir(path), ".agent-health-")
	if e != nil {
		return e
	}
	defer os.Remove(tmp.Name())
	if _, e = tmp.Write(data); e != nil {
		tmp.Close()
		return e
	}
	if e = tmp.Close(); e != nil {
		return e
	}
	return os.Rename(tmp.Name(), path)
}

func CheckHealth(path string) error {
	data, e := os.ReadFile(path)
	if e != nil {
		return errors.New("Agent has not reported a successful Server presence")
	}
	var stamp healthStamp
	if e = json.Unmarshal(data, &stamp); e != nil || stamp.PID < 1 || stamp.ReportedAt.IsZero() || time.Since(stamp.ReportedAt) < 0 || time.Since(stamp.ReportedAt) > 45*time.Second {
		return errors.New("Agent Server presence is stale")
	}
	return nil
}
