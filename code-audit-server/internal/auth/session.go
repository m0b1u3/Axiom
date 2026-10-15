package auth

import (
	"bytes"
	"crypto/hmac"
	"crypto/rand"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"errors"
	"io"
	"strings"
	"time"
)

type SessionClaims struct {
	Version     int    `json:"v"`
	UserID      int64  `json:"uid"`
	SessionID   string `json:"sid"`
	AuthVersion int64  `json:"av"`
	ExpiresAt   int64  `json:"exp"`
}

func NewSessionID() (string, error) {
	value := make([]byte, 32)
	_, e := rand.Read(value)
	return base64.RawURLEncoding.EncodeToString(value), e
}
func SessionToken(secret string, c SessionClaims) (string, error) {
	if !validClaims(c) || secret == "" {
		return "", errors.New("invalid session claims")
	}
	payload, e := json.Marshal(c)
	if e != nil {
		return "", e
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	return base64.RawURLEncoding.EncodeToString(payload) + "." + base64.RawURLEncoding.EncodeToString(mac.Sum(nil)), nil
}
func ParseSession(secret, token string, now time.Time) (SessionClaims, error) {
	var c SessionClaims
	if len(token) > 2048 || secret == "" {
		return c, errors.New("invalid session token")
	}
	parts := strings.Split(token, ".")
	if len(parts) != 2 {
		return c, errors.New("invalid session token")
	}
	payload, e := base64.RawURLEncoding.DecodeString(parts[0])
	if e != nil {
		return c, e
	}
	signature, e := base64.RawURLEncoding.DecodeString(parts[1])
	if e != nil {
		return c, e
	}
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write(payload)
	if !hmac.Equal(signature, mac.Sum(nil)) {
		return c, errors.New("invalid session signature")
	}
	decoder := json.NewDecoder(bytes.NewReader(payload))
	decoder.DisallowUnknownFields()
	if e = decoder.Decode(&c); e != nil {
		return c, e
	}
	var extra any
	if decoder.Decode(&extra) != io.EOF {
		return c, errors.New("invalid session payload")
	}
	if !validClaims(c) || now.Unix() >= c.ExpiresAt {
		return c, errors.New("expired or obsolete session")
	}
	return c, nil
}
func validClaims(c SessionClaims) bool {
	id, e := base64.RawURLEncoding.DecodeString(c.SessionID)
	return c.Version == 2 && c.UserID > 0 && c.AuthVersion >= 0 && c.ExpiresAt > 0 && e == nil && len(id) == 32
}
func TokenHash(token string) string {
	v := sha256.Sum256([]byte(token))
	return hex.EncodeToString(v[:])
}
