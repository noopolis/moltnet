package updater

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"strings"
)

func VerifyChecksum(assetName string, archive []byte, manifest []byte) error {
	expected, ok := checksumForAsset(assetName, manifest)
	if !ok {
		return fmt.Errorf("checksum not found for %s", assetName)
	}

	sum := sha256.Sum256(archive)
	actual := hex.EncodeToString(sum[:])
	if !strings.EqualFold(actual, expected) {
		return fmt.Errorf("checksum mismatch for %s", assetName)
	}
	return nil
}

func checksumForAsset(assetName string, manifest []byte) (string, bool) {
	for _, line := range strings.Split(string(manifest), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 2 {
			continue
		}
		name := strings.TrimPrefix(fields[1], "*")
		if name == assetName {
			return strings.ToLower(fields[0]), true
		}
	}
	return "", false
}
