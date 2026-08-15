package app

import (
	"os"
	"path/filepath"
	"testing"
)

// boundaryRuneConfigContents builds a valid YAML Moltnet config whose byte
// 512 (0-indexed 511/512) lands in the middle of a multibyte UTF-8 rune
// (€, encoded as the three bytes E2 82 AC): the header is padded with
// ASCII filler so the euro sign's first byte sits at offset 510, meaning
// looksLikeTextConfig's 512-byte sample includes exactly two of its three
// bytes. This is the P0-1 field scenario: naively truncating to 512 bytes
// and running utf8.Valid on the result would call this file binary and
// silently resolve the wrong network.
func boundaryRuneConfigContents(networkID string) []byte {
	header := []byte("version: moltnet.v1\nnetwork:\n  id: " + networkID + "\n# ")
	pad := 510 - len(header)
	if pad < 0 {
		pad = 0
	}
	contents := append([]byte{}, header...)
	for i := 0; i < pad; i++ {
		contents = append(contents, 'a')
	}
	contents = append(contents, "€\n"...)
	return contents
}

func TestTextSniffContentsToleratesBoundaryStraddlingRune(t *testing.T) {
	contents := boundaryRuneConfigContents("cwd-net")
	if len(contents) <= 512 {
		t.Fatalf("fixture too short to exercise the 512-byte boundary: %d bytes", len(contents))
	}
	if !textSniffContents(contents) {
		t.Fatal("textSniffContents() = false for a valid text file whose 512th byte splits a multibyte rune")
	}
}

func TestTextSniffContentsStillRejectsRealBinary(t *testing.T) {
	if textSniffContents(binaryLikeCandidateContents()) {
		t.Fatal("textSniffContents() = true for NUL-containing binary content")
	}
}

func TestLooksLikeTextConfigToleratesBoundaryStraddlingRune(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Moltnet")
	if err := os.WriteFile(path, boundaryRuneConfigContents("cwd-net"), 0o600); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	ok, err := looksLikeTextConfig(path)
	if err != nil {
		t.Fatalf("looksLikeTextConfig() error = %v", err)
	}
	if !ok {
		t.Fatal("looksLikeTextConfig() = false for a valid text file whose 512th byte splits a multibyte rune")
	}
}

func TestLooksLikeTextConfigStillRejectsRealBinary(t *testing.T) {
	path := filepath.Join(t.TempDir(), "Moltnet")
	if err := os.WriteFile(path, binaryLikeCandidateContents(), 0o755); err != nil {
		t.Fatalf("write fixture: %v", err)
	}

	ok, err := looksLikeTextConfig(path)
	if err != nil {
		t.Fatalf("looksLikeTextConfig() error = %v", err)
	}
	if ok {
		t.Fatal("looksLikeTextConfig() = true for NUL-containing binary content")
	}
}
