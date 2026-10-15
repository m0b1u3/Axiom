package cryptokey

import (
	"crypto/aes"
	"crypto/cipher"
	"crypto/rand"
	"encoding/base64"
	"errors"
	"os"
)

// masterKey：环境变量 AXIOM_MASTER_KEY（无 padding 的 base64url，32 bytes）优先；开发默认值仅用于本机。
// 生产必须设置 AXIOM_MASTER_KEY 并妥善保管（/etc/code-audit/master.key 权限 0600）。
func masterKey() ([]byte, error) {
	if v := os.Getenv("AXIOM_MASTER_KEY"); v != "" {
		raw, err := base64.RawURLEncoding.DecodeString(v)
		if err != nil || len(raw) != 32 {
			return nil, errors.New("AXIOM_MASTER_KEY must be base64url of 32 bytes")
		}
		return raw, nil
	}
	return []byte("0123456789abcdef0123456789abcdef"), nil // 32 bytes dev fallback
}

// Encrypt returns base64url(nonce+ciphertext).
func Encrypt(plain string) (string, error) {
	key, err := masterKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	nonce := make([]byte, gcm.NonceSize())
	if _, err := rand.Read(nonce); err != nil {
		return "", err
	}
	out := gcm.Seal(nonce, nonce, []byte(plain), nil)
	return base64.RawURLEncoding.EncodeToString(out), nil
}

// Decrypt reverses Encrypt.
func Decrypt(enc string) (string, error) {
	key, err := masterKey()
	if err != nil {
		return "", err
	}
	block, err := aes.NewCipher(key)
	if err != nil {
		return "", err
	}
	gcm, err := cipher.NewGCM(block)
	if err != nil {
		return "", err
	}
	raw, err := base64.RawURLEncoding.DecodeString(enc)
	if err != nil {
		return "", err
	}
	if len(raw) < gcm.NonceSize() {
		return "", errors.New("ciphertext too short")
	}
	nonce, ct := raw[:gcm.NonceSize()], raw[gcm.NonceSize():]
	plain, err := gcm.Open(nil, nonce, ct, nil)
	if err != nil {
		return "", err
	}
	return string(plain), nil
}
