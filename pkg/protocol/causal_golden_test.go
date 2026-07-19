package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"os"
	"strings"
	"testing"
)

type goldenCase struct {
	Accept       bool   `json:"accept"`
	CanonicalHex string `json:"canonical_hex"`
	Name         string `json:"name"`
	RawJSONL     string `json:"raw_jsonl"`
	SHA256       string `json:"sha256"`
}

func TestCausalContractGoldens(t *testing.T) {
	manifestPath := "../../../stele/src/contracts/goldens/causal-contract.v1.json"
	rawManifest, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read golden manifest: %v", err)
	}

	var decoded []map[string]any
	if err := json.Unmarshal(rawManifest, &decoded); err != nil {
		t.Fatalf("unmarshal manifest: %v", err)
	}

	required := []string{"accept", "canonical_hex", "name", "raw_jsonl", "sha256"}
	cases := make([]goldenCase, 0, len(decoded))
	seen := map[string]struct{}{}
	for _, raw := range decoded {
		if len(raw) != len(required) {
			t.Fatalf("manifest case has unexpected shape: %v", raw)
		}
		for _, key := range required {
			if _, ok := raw[key]; !ok {
				t.Fatalf("manifest case missing key %q: %v", key, raw)
			}
		}

		name, ok := raw["name"].(string)
		if !ok || name == "" {
			t.Fatalf("invalid manifest name: %v", raw["name"])
		}
		if _, duplicate := seen[name]; duplicate {
			t.Fatalf("duplicate manifest name %q", name)
		}
		seen[name] = struct{}{}

		accept, ok := raw["accept"].(bool)
		if !ok {
			t.Fatalf("invalid accept flag for %q", name)
		}
		canonicalHex, _ := raw["canonical_hex"].(string)
		rawJSONL, ok := raw["raw_jsonl"].(string)
		if !ok || rawJSONL == "" {
			t.Fatalf("invalid raw_jsonl for %q", name)
		}
		shaHex, _ := raw["sha256"].(string)

		if accept {
			if canonicalHex == "" {
				t.Fatalf("accepted case %q requires canonical_hex", name)
			}
			if _, err := hex.DecodeString(canonicalHex); err != nil {
				t.Fatalf("accepted case %q has invalid canonical_hex: %v", name, err)
			}
			if !isHexSHA(shaHex) {
				t.Fatalf("accepted case %q has invalid sha256: %q", name, shaHex)
			}
		} else {
			if canonicalHex != "" || shaHex != "" {
				t.Fatalf("rejected case %q must use null oracle values", name)
			}
		}

		cases = append(cases, goldenCase{
			Accept:       accept,
			Name:         name,
			CanonicalHex: canonicalHex,
			RawJSONL:     rawJSONL,
			SHA256:       shaHex,
		})
	}

	var accepted, rejected int
	for _, entry := range cases {
		result := ParseCausalBundle(entry.RawJSONL)
		if entry.Accept {
			accepted += 1
			if len(result.Errors) != 0 {
				t.Fatalf("accepted case %q has validation errors: %#v", entry.Name, result.Errors)
			}
			lines := strings.Split(entry.RawJSONL, "\n")
			canonical := make([]string, 0, len(lines))
			for _, line := range lines {
				trimmed := trimJsonWhitespace(line)
				if trimmed == "" {
					continue
				}
				parsed, err := ParseCanonicalJSON(trimmed)
				if err != nil {
					t.Fatalf("accepted case %q parse failed: %v", entry.Name, err)
				}
				lineBytes, err := CanonicalJSONString(parsed)
				if err != nil {
					t.Fatalf("accepted case %q canonicalize failed: %v", entry.Name, err)
				}
				canonical = append(canonical, lineBytes)
			}
			if len(canonical) == 0 {
				t.Fatalf("accepted case %q has no parseable lines", entry.Name)
			}
			joined := strings.Join(canonical, "\n")
			gotHex := hex.EncodeToString([]byte(joined))
			if gotHex != entry.CanonicalHex {
				t.Fatalf("accepted case %q canonical hex mismatch", entry.Name)
			}
			gotSHA := sha256.Sum256([]byte(joined))
			if hex.EncodeToString(gotSHA[:]) != entry.SHA256 {
				t.Fatalf("accepted case %q sha256 mismatch", entry.Name)
			}
			continue
		}

		rejected += 1
		if len(result.Errors) == 0 {
			t.Fatalf("rejected case %q should have validation errors", entry.Name)
		}
	}

	if accepted == 0 || rejected == 0 {
		t.Fatalf("manifest must contain both accepted and rejected cases: accepted=%d rejected=%d", accepted, rejected)
	}

	if got := len(seen); got != len(cases) {
		t.Fatalf("manifest names were not unique")
	}
}

func isHexSHA(value string) bool {
	if len(value) != 64 {
		return false
	}
	for _, char := range value {
		if (char >= '0' && char <= '9') || (char >= 'a' && char <= 'f') {
			continue
		}
		return false
	}
	return true
}
