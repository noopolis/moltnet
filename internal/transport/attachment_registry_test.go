package transport

import (
	"strings"
	"testing"
)

func TestAttachmentRegistryRejectsAnonymousDuplicateAgent(t *testing.T) {
	t.Parallel()

	registry := newAttachmentRegistry()
	release, err := registry.acquire("director", attachmentCredential{})
	if err != nil {
		t.Fatalf("acquire() error = %v", err)
	}

	if _, err := registry.acquire("director", attachmentCredential{}); err == nil ||
		!strings.Contains(err.Error(), "already has an active attachment") {
		t.Fatalf("expected anonymous duplicate rejection, got %v", err)
	}

	release()

	release, err = registry.acquire("director", attachmentCredential{})
	if err != nil {
		t.Fatalf("expected release to allow reacquire, got %v", err)
	}
	release()
}

func TestAttachmentRegistryAllowsSameCredentialAndRejectsDifferentCredential(t *testing.T) {
	t.Parallel()

	registry := newAttachmentRegistry()
	tokenA := attachmentCredential{key: "token:a", authenticated: true}
	tokenB := attachmentCredential{key: "token:b", authenticated: true}

	releaseA, err := registry.acquire("director", tokenA)
	if err != nil {
		t.Fatalf("acquire(tokenA) error = %v", err)
	}
	releaseAAgain, err := registry.acquire("director", tokenA)
	if err != nil {
		t.Fatalf("acquire(tokenA again) error = %v", err)
	}

	if _, err := registry.acquire("director", tokenB); err == nil ||
		!strings.Contains(err.Error(), "different credentials") {
		t.Fatalf("expected credential collision rejection, got %v", err)
	}

	releaseA()
	if _, err := registry.acquire("director", tokenB); err == nil ||
		!strings.Contains(err.Error(), "different credentials") {
		t.Fatalf("expected second active tokenA claim to keep collision, got %v", err)
	}

	releaseAAgain()
	releaseB, err := registry.acquire("director", tokenB)
	if err != nil {
		t.Fatalf("expected release to allow new credential, got %v", err)
	}
	releaseB()
	releaseB()
}
