package worker

import (
	"testing"
	"time"
)

func TestRenewalWindowSupportsShortLeases(t *testing.T) {
	for _, seconds := range []int{10, 12, 60, 120} {
		lease := time.Duration(seconds) * time.Second
		interval, reserve := renewalTiming(lease)
		if interval <= 0 || reserve <= 0 || interval >= lease-reserve || reserve > 15*time.Second {
			t.Fatalf("unsafe renewal for %s: interval=%s reserve=%s", lease, interval, reserve)
		}
		if seconds == 60 && (interval != 5*time.Second || reserve != 15*time.Second) {
			t.Fatal("normal renewal timing changed")
		}
	}
}
