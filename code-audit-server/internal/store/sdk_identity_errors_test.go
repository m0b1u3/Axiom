package store

import (
	"strings"
	"testing"
)

func TestSDKIdentityPublicErrorIsAllowlisted(t *testing.T) {
	if !strings.Contains(sdkIdentityPublicError("sdk_auth_forbidden"), "403") {
		t.Fatal("safe diagnostic lost")
	}
	for _, raw := range []string{"sdk_auth_forbidden fixture-secret", "token=fixture-secret", "https://auth.openai.com/?code=fixture-secret"} {
		if got := sdkIdentityPublicError(raw); strings.Contains(got, "fixture-secret") || strings.Contains(got, "403") {
			t.Fatal("untrusted diagnostic escaped allowlist")
		}
	}
}
