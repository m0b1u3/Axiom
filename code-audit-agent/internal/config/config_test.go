package config

import (
	"testing"
	"time"
)

func TestPollIntervalCannotSpinOnNonPositiveDuration(t *testing.T) {
	for _, value := range []string{"0", "-1", "-5ms", "bad"} {
		t.Setenv("AXIOM_POLL_INTERVAL", value)
		if FromEnv().PollInterval != 2*time.Second {
			t.Fatal("invalid poll interval accepted", value)
		}
	}
	t.Setenv("AXIOM_POLL_INTERVAL", "500ms")
	if FromEnv().PollInterval != 500*time.Millisecond {
		t.Fatal("valid interval lost")
	}
}
