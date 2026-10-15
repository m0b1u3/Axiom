package cryptokey

import (
	"testing"
)

func TestRoundTrip(t *testing.T) {
	secret := "sk-test-1234567890"
	enc, err := Encrypt(secret)
	if err != nil {
		t.Fatal(err)
	}
	if enc == secret {
		t.Fatal("ciphertext equals plaintext")
	}
	dec, err := Decrypt(enc)
	if err != nil {
		t.Fatal(err)
	}
	if dec != secret {
		t.Fatalf("roundtrip mismatch: %q", dec)
	}
}

func TestUniqueCiphertext(t *testing.T) {
	a, _ := Encrypt("same")
	b, _ := Encrypt("same")
	if a == b {
		t.Fatal("nonce not randomized")
	}
}

func TestTamperRejected(t *testing.T) {
	enc, _ := Encrypt("abc")
	if _, err := Decrypt("AAAA" + enc[4:]); err == nil {
		t.Fatal("tampered ciphertext should fail GCM auth")
	}
}
