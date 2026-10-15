package auth

import (
	"strings"
	"testing"
	"time"
)

func TestPasswordSaltLegacyMigrationAndSigningKeyIndependence(t *testing.T) {
	password := "fixture-password-中文"
	a, e := NewPasswordHash(password)
	if e != nil {
		t.Fatal(e)
	}
	b, e := NewPasswordHash(password)
	if e != nil || a == b {
		t.Fatal("salt not independent", e)
	}
	for _, secret := range []string{"old-signing-key", "rotated-signing-key"} {
		ok, upgrade := VerifyPassword(a, password, secret)
		if !ok || upgrade {
			t.Fatal("password coupled to signing key")
		}
	}
	old := HashPassword("legacy-key", password)
	ok, upgrade := VerifyPassword(old, password, "legacy-key")
	if !ok || !upgrade {
		t.Fatal("legacy migration unavailable")
	}
	if ok, _ = VerifyPassword(old, password, "wrong-key"); ok {
		t.Fatal("legacy secret bypass")
	}
	for _, value := range []string{"$argon2id$v=19$m=4294967295,t=2,p=1$AAAA$AAAA", "$argon2id$v=19$m=19456,t=999,p=1$AAAA$AAAA", "malformed"} {
		if ok, _ := VerifyPassword(value, password, "legacy-key"); ok {
			t.Fatal("malformed credential accepted")
		}
	}
	if ok, _ := VerifyPassword(a, "wrong-password", "legacy-key"); ok {
		t.Fatal("wrong password accepted")
	}
	if ValidNewPassword("short") || !ValidNewPassword("十二个字符的密码验证用例一二") {
		t.Fatal("new password character policy")
	}
}
func TestSessionClaimsIntegrityExpiryAndLegacyRejection(t *testing.T) {
	sid, e := NewSessionID()
	if e != nil {
		t.Fatal(e)
	}
	now := time.Now().Truncate(time.Second)
	c := SessionClaims{Version: 2, UserID: 9, SessionID: sid, AuthVersion: 3, ExpiresAt: now.Add(time.Hour).Unix()}
	token, e := SessionToken("fixture-key", c)
	if e != nil {
		t.Fatal(e)
	}
	got, e := ParseSession("fixture-key", token, now)
	if e != nil || got != c {
		t.Fatal(got, e)
	}
	for _, v := range []string{token + "x", strings.Replace(token, ".", "..", 1), strings.Repeat("x", 2049)} {
		if _, e = ParseSession("fixture-key", v, now); e == nil {
			t.Fatal("invalid token accepted")
		}
	}
	if _, e = ParseSession("wrong-key", token, now); e == nil {
		t.Fatal("key rotation ignored")
	}
	if _, e = ParseSession("fixture-key", token, time.Unix(c.ExpiresAt, 0)); e == nil {
		t.Fatal("expiry boundary ignored")
	}
	legacy, e := Sign("fixture-key", 9, time.Hour)
	if e != nil {
		t.Fatal(e)
	}
	if _, e = ParseSession("fixture-key", legacy, now); e == nil {
		t.Fatal("legacy bearer bypassed session table")
	}
	if TokenHash(token) == token || len(TokenHash(token)) != 64 {
		t.Fatal("invalid stored token digest")
	}
}
