package app

import (
	"encoding/base64"
	"testing"
)

func TestGenerateRandomTokenHasRequestedEntropy(t *testing.T) {
	t.Parallel()

	token, err := GenerateRandomToken(128)
	if err != nil {
		t.Fatalf("GenerateRandomToken() error = %v", err)
	}

	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("decode token %q: %v", token, err)
	}
	if len(decoded) < 16 {
		t.Fatalf("token %q decodes to %d bytes, want at least 16 (128 bits)", token, len(decoded))
	}
}

func TestGenerateRandomTokenDefaultsBitsWhenNonPositive(t *testing.T) {
	t.Parallel()

	token, err := GenerateRandomToken(0)
	if err != nil {
		t.Fatalf("GenerateRandomToken() error = %v", err)
	}
	decoded, err := base64.RawURLEncoding.DecodeString(token)
	if err != nil {
		t.Fatalf("decode token %q: %v", token, err)
	}
	if len(decoded) != 16 {
		t.Fatalf("token %q decodes to %d bytes, want 16 (128 bits default)", token, len(decoded))
	}
}

func TestGenerateRandomTokenIsUnique(t *testing.T) {
	t.Parallel()

	first, err := GenerateRandomToken(128)
	if err != nil {
		t.Fatalf("GenerateRandomToken() error = %v", err)
	}
	second, err := GenerateRandomToken(128)
	if err != nil {
		t.Fatalf("GenerateRandomToken() error = %v", err)
	}
	if first == second {
		t.Fatalf("expected distinct random tokens, got %q twice", first)
	}
}
