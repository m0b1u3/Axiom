package auth

import (
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/base64"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"unicode/utf8"

	"golang.org/x/crypto/argon2"
)

const PasswordMemory uint32 = 19 * 1024
const PasswordTime uint32 = 2
const PasswordThreads uint8 = 1
const MaxPasswordBytes = 1024

// New passwords use the OWASP minimum Argon2id parameters, with independent
// random salts. Signing-secret rotation cannot alter these password hashes.
func NewPasswordHash(password string) (string, error) {
	if len(password) == 0 || len(password) > MaxPasswordBytes || !utf8.ValidString(password) {
		return "", errors.New("invalid password length")
	}
	salt := make([]byte, 16)
	if _, err := rand.Read(salt); err != nil {
		return "", err
	}
	key := argon2.IDKey([]byte(password), salt, PasswordTime, PasswordMemory, PasswordThreads, 32)
	return fmt.Sprintf("$argon2id$v=19$m=%d,t=%d,p=%d$%s$%s", PasswordMemory, PasswordTime, PasswordThreads, base64.RawStdEncoding.EncodeToString(salt), base64.RawStdEncoding.EncodeToString(key)), nil
}
func ValidNewPassword(password string) bool {
	return utf8.ValidString(password) && utf8.RuneCountInString(password) >= 12 && len(password) <= MaxPasswordBytes
}

// HashPassword is the frozen legacy hash. Only existing account verification
// and migration fixtures use it; newly created credentials use NewPasswordHash.
func HashPassword(secret, password string) string {
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(password))
	return base64.RawURLEncoding.EncodeToString(mac.Sum(nil))
}
func VerifyPassword(encoded, password, legacySecret string) (valid, upgrade bool) {
	if len(password) == 0 || len(password) > MaxPasswordBytes || len(encoded) > 256 || !utf8.ValidString(password) {
		return false, false
	}
	if !strings.HasPrefix(encoded, "$") {
		old, err := base64.RawURLEncoding.DecodeString(encoded)
		if err != nil || len(old) != 32 || legacySecret == "" {
			return false, false
		}
		next, _ := base64.RawURLEncoding.DecodeString(HashPassword(legacySecret, password))
		return subtle.ConstantTimeCompare(old, next) == 1, true
	}
	parts := strings.Split(encoded, "$")
	if len(parts) != 6 || parts[1] != "argon2id" || parts[2] != "v=19" {
		return false, false
	}
	params := strings.Split(parts[3], ",")
	if len(params) != 3 {
		return false, false
	}
	names := []string{"m=", "t=", "p="}
	values := make([]uint64, 3)
	for i, param := range params {
		if !strings.HasPrefix(param, names[i]) {
			return false, false
		}
		v, e := strconv.ParseUint(strings.TrimPrefix(param, names[i]), 10, 32)
		if e != nil {
			return false, false
		}
		values[i] = v
	}
	// A corrupt/imported record must not allocate unbounded KDF work.
	if values[0] < 8 || values[0] > 64*1024 || values[1] < 1 || values[1] > 4 || values[2] < 1 || values[2] > 2 || values[0] < 8*values[2] {
		return false, false
	}
	salt, e := base64.RawStdEncoding.DecodeString(parts[4])
	if e != nil || len(salt) != 16 {
		return false, false
	}
	expected, e := base64.RawStdEncoding.DecodeString(parts[5])
	if e != nil || len(expected) != 32 {
		return false, false
	}
	actual := argon2.IDKey([]byte(password), salt, uint32(values[1]), uint32(values[0]), uint8(values[2]), 32)
	return subtle.ConstantTimeCompare(actual, expected) == 1, values[0] < uint64(PasswordMemory) || values[1] < uint64(PasswordTime)
}
