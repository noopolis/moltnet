package nodeconfig

import (
	"os"
	"path/filepath"
	"testing"
)

// boundaryRuneNodeConfigContents is boundaryRuneConfigContents' MoltnetNode
// counterpart (see internal/app/config_sniff_test.go): a valid YAML
// MoltnetNode config padded so a multibyte UTF-8 rune (€) straddles the
// 512-byte sniff sample boundary — the exact P0-1 field scenario where
// naive truncation-then-utf8.Valid would misclassify valid text as binary.
func boundaryRuneNodeConfigContents(networkID string) []byte {
	header := []byte("version: moltnet.node.v1\nmoltnet:\n  base_url: http://127.0.0.1:8787\n  network_id: " + networkID + "\n# ")
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
	contents := boundaryRuneNodeConfigContents("cwd-net")
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
	path := filepath.Join(t.TempDir(), DefaultPath)
	if err := os.WriteFile(path, boundaryRuneNodeConfigContents("cwd-net"), 0o600); err != nil {
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
	path := filepath.Join(t.TempDir(), DefaultPath)
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
