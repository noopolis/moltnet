package app

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
)

// GenerateRandomToken returns a base64url-encoded random value with at least
// bits worth of entropy (rounded up to a whole byte). It backs the relay
// pairing invite's room name and pairing token: the relay's RELAY_TOKEN is
// worker-wide, so this entropy is what keeps one invited friend out of
// another friend's relay room.
func GenerateRandomToken(bits int) (string, error) {
	if bits <= 0 {
		bits = 128
	}

	buf := make([]byte, (bits+7)/8)
	if _, err := rand.Read(buf); err != nil {
		return "", fmt.Errorf("generate random token: %w", err)
	}

	return base64.RawURLEncoding.EncodeToString(buf), nil
}
