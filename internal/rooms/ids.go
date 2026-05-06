package rooms

import (
	"crypto/rand"
	"crypto/sha256"
	"fmt"
	"strings"
	"sync/atomic"
	"time"
)

var fallbackIDCounter atomic.Uint64

func newPrefixedID(prefix string) string {
	random, err := randomUUID()
	if err != nil {
		random = fallbackID()
	}
	return strings.TrimSpace(prefix) + "_" + random
}

func deterministicPrefixedID(prefix string, seed string) string {
	sum := sha256.Sum256([]byte(strings.TrimSpace(seed)))
	var bytes [16]byte
	copy(bytes[:], sum[:16])
	return strings.TrimSpace(prefix) + "_" + formatUUID(bytes)
}

func randomUUID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", err
	}
	return formatUUID(bytes), nil
}

func formatUUID(bytes [16]byte) string {
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80
	return fmt.Sprintf(
		"%x-%x-%x-%x-%x",
		bytes[0:4],
		bytes[4:6],
		bytes[6:8],
		bytes[8:10],
		bytes[10:16],
	)
}

func fallbackID() string {
	return fmt.Sprintf("fallback-%d-%d", time.Now().UnixNano(), fallbackIDCounter.Add(1))
}
