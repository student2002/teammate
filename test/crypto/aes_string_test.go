// aes_string_test.go covers the generic string secret encryption/decryption
// (EncryptString/DecryptString): roundtrip, ciphertext randomness, tamper
// detection, and compatibility with the PAT-named wrappers.
package crypto_test

import (
	"encoding/base64"
	"os"
	"testing"

	"github.com/teammate/server/internal/crypto"
)

// initStringTestKey sets a deterministic 32-byte test key.
func initStringTestKey(t *testing.T) {
	t.Helper()
	os.Setenv("TEAMMATE_ENCRYPTION_KEY_BASE64", base64.StdEncoding.EncodeToString(make([]byte, 32)))
	os.Unsetenv("TEAMMATE_DEV")
	if err := crypto.InitEncryptionKey(); err != nil {
		t.Fatalf("InitEncryptionKey: %v", err)
	}
}

// TestEncryptStringRoundtrip verifies EncryptString/DecryptString restore the
// original plaintext, including empty and multibyte strings.
func TestEncryptStringRoundtrip(t *testing.T) {
	initStringTestKey(t)

	for _, plaintext := range []string{"github_pat_11ABCDEFG0abcdefgh", "", "webhook-secret-线索-🔒", "x"} {
		encrypted, err := crypto.EncryptString(plaintext)
		if err != nil {
			t.Fatalf("EncryptString(%q): %v", plaintext, err)
		}
		if encrypted == plaintext && plaintext != "" {
			t.Fatalf("ciphertext must not equal plaintext")
		}
		decrypted, err := crypto.DecryptString(encrypted)
		if err != nil {
			t.Fatalf("DecryptString(%q): %v", plaintext, err)
		}
		if decrypted != plaintext {
			t.Fatalf("roundtrip: got %q, want %q", decrypted, plaintext)
		}
	}
}

// TestEncryptStringRandomNonce verifies two encryptions of the same plaintext
// produce different ciphertexts (random nonce) and both decrypt correctly.
func TestEncryptStringRandomNonce(t *testing.T) {
	initStringTestKey(t)

	first, err := crypto.EncryptString("same-secret")
	if err != nil {
		t.Fatalf("EncryptString: %v", err)
	}
	second, err := crypto.EncryptString("same-secret")
	if err != nil {
		t.Fatalf("EncryptString: %v", err)
	}
	if first == second {
		t.Fatal("identical plaintexts must produce different ciphertexts")
	}
	for _, encoded := range []string{first, second} {
		decrypted, err := crypto.DecryptString(encoded)
		if err != nil || decrypted != "same-secret" {
			t.Fatalf("decrypt %q: got %q, err %v", encoded, decrypted, err)
		}
	}
}

// TestDecryptStringTampered verifies tampered or malformed ciphertexts fail.
func TestDecryptStringTampered(t *testing.T) {
	initStringTestKey(t)

	encrypted, err := crypto.EncryptString("secret")
	if err != nil {
		t.Fatalf("EncryptString: %v", err)
	}
	raw, err := base64.StdEncoding.DecodeString(encrypted)
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	raw[len(raw)-1] ^= 0xFF
	if _, err := crypto.DecryptString(base64.StdEncoding.EncodeToString(raw)); err == nil {
		t.Fatal("tampered ciphertext must fail")
	}
	if _, err := crypto.DecryptString("not-base64!!!"); err == nil {
		t.Fatal("malformed base64 must fail")
	}
	if _, err := crypto.DecryptString(base64.StdEncoding.EncodeToString([]byte("short"))); err == nil {
		t.Fatal("short ciphertext must fail")
	}
}

// TestPATWrappersMatchGeneric verifies EncryptPAT/DecryptPAT behave identically
// to the generic string functions (same ciphertext family, cross-decryptable).
func TestPATWrappersMatchGeneric(t *testing.T) {
	initStringTestKey(t)

	encrypted, err := crypto.EncryptPAT("cross-check")
	if err != nil {
		t.Fatalf("EncryptPAT: %v", err)
	}
	generic, err := crypto.DecryptString(encrypted)
	if err != nil || generic != "cross-check" {
		t.Fatalf("DecryptString on PAT ciphertext: got %q, err %v", generic, err)
	}
	encrypted2, err := crypto.EncryptString("cross-check")
	if err != nil {
		t.Fatalf("EncryptString: %v", err)
	}
	plain, err := crypto.DecryptPAT(encrypted2)
	if err != nil || plain != "cross-check" {
		t.Fatalf("DecryptPAT on generic ciphertext: got %q, err %v", plain, err)
	}
}
