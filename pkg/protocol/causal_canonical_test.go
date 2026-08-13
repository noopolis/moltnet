package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"math"
	"testing"
)

func TestParseCanonicalJSONRejectsDuplicateDecodedKeys(t *testing.T) {
	_, err := ParseCanonicalJSON(`{"a":1,"a":2}`)
	if err == nil {
		t.Fatal("expected duplicate key error")
	}

	_, err = ParseCanonicalJSON(`{"\u0061":1,"a":2}`)
	if err == nil {
		t.Fatal("expected escaped-duplicate key error")
	}
}

func TestParseCanonicalJSONRejectsMalformedJSONValues(t *testing.T) {
	if _, err := ParseCanonicalJSON("{"); err == nil {
		t.Fatal("expected invalid object")
	}
	if _, err := ParseCanonicalJSON("1e"); err == nil {
		t.Fatal("expected invalid number syntax")
	}
	if _, err := ParseCanonicalJSON("Infinity"); err == nil {
		t.Fatal("expected non-canonical number spellling rejection")
	}
}

func TestParseCanonicalJSONRejectsInvalidUnicode(t *testing.T) {
	if _, err := ParseCanonicalJSON(`{"x":"\uDC00"}`); err == nil {
		t.Fatal("expected lone low surrogate value rejection")
	}
	if _, err := ParseCanonicalJSON(`{"\uDC00":"value"}`); err == nil {
		t.Fatal("expected lone low surrogate key rejection")
	}
	if _, err := ParseCanonicalJSON(`{"x":"\uD834\uDF06"}`); err != nil {
		t.Fatalf("expected valid surrogate pair acceptance: %v", err)
	}
	if _, err := ParseCanonicalJSON(`{"value":"\uD800"}`); err == nil {
		t.Fatal("expected lone surrogate rejection")
	}
	if _, err := ParseCanonicalJSON(`{"\uD800":"value"}`); err == nil {
		t.Fatal("expected lone surrogate key rejection")
	}
	if _, err := ParseCanonicalJSONBytes([]byte{0xff}); err == nil {
		t.Fatal("expected invalid UTF-8 rejection")
	}
	if _, err := ParseCanonicalJSONBytes([]byte{0xef, 0xbb, 0xbf, '{', '}'}); err == nil {
		t.Fatal("expected BOM rejection")
	}
}

func TestParseCanonicalJSONObjectWhitespaceRules(t *testing.T) {
	for _, json := range []string{
		`{"a":1,"b":2}`,
		`{"a" :1,"b" :2}`,
		`{"a":1,` + "\t" + `"b":` + "\r" + `2,` + ` "c":` + "\n" + `3}`,
		`{"a" ` + "\t\n\r" + `:` + "\n \r\t" + `1, "b"` + "\r\t" + `:` + "\t\n" + `2}`,
		`{"outer":{"inner"` + "\t" + `:1,"leaf":"x"},"next":2}`,
		`{"outer" : {"inner"` + "\n" + `:` + "\r" + `1}, "next" : 2}`,
	} {
		value, err := ParseCanonicalJSON(json)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", json, err)
		}
		record, ok := value.(map[string]any)
		if !ok {
			t.Fatal("expected object")
		}
		_ = record
	}

	for _, ws := range []string{"", " ", "\t", "\n", "\r", "\t \n", "\n\r\t", " \r\n\t "} {
		t.Run(fmt.Sprintf("legal_ws_%q", ws), func(t *testing.T) {
			json := fmt.Sprintf(`{"a"%s:%s1}`, ws, ws)
			if _, err := ParseCanonicalJSON(json); err != nil {
				t.Fatalf("unexpected error for whitespace %q: %v", ws, err)
			}
		})
	}

	for _, ws := range []string{"\v", "\f", "\u00a0"} {
		t.Run(fmt.Sprintf("illegal_ws_%q", ws), func(t *testing.T) {
			json := fmt.Sprintf(`{"a"%s:1}`, ws)
			if _, err := ParseCanonicalJSON(json); err == nil {
				t.Fatalf("expected parse rejection for illegal whitespace %q", ws)
			}
		})
	}
}

func TestCanonicalJSONStringEscapesControlCodePointsPerJSONRule(t *testing.T) {
	test := func(raw string, want string) {
		serialized, err := CanonicalJSONString(map[string]any{"value": raw})
		if err != nil {
			t.Fatalf("CanonicalJSONString() error = %v", err)
		}
		expected := "{\"value\":\"" + want + "\"}"
		if serialized != expected {
			t.Fatalf("unexpected canonical output for %q: got %q expected %q", raw, serialized, expected)
		}
	}

	for codepoint := 0; codepoint <= 0x1F; codepoint++ {
		raw := string(rune(codepoint))
		switch codepoint {
		case '\b':
			test(raw, "\\b")
		case '\t':
			test(raw, "\\t")
		case '\n':
			test(raw, "\\n")
		case '\f':
			test(raw, "\\f")
		case '\r':
			test(raw, "\\r")
		default:
			test(raw, fmt.Sprintf("\\u%04x", codepoint))
		}
	}

	test(`\`, "\\\\")
	test(`"`, "\\\"")
}

func TestCanonicalJSONStringSortsUTF16KeysAndPreservesArrayOrder(t *testing.T) {
	value := map[string]any{
		"\U0001D11E": 3,
		"b":          2,
		"a":          1,
	}
	serialized, err := CanonicalJSONString(value)
	if err != nil {
		t.Fatalf("CanonicalJSONString() error = %v", err)
	}
	if serialized != "{\"a\":1,\"b\":2,\"𝄞\":3}" {
		t.Fatalf("unexpected canonical order: %s", serialized)
	}
}

func TestCanonicalJSONStringSortsUTF16KeysNestedAndNumericValues(t *testing.T) {
	value := map[string]any{
		"z":  1,
		"2":  "outer-two",
		"10": "outer-ten",
		"a": map[string]any{
			"10":         "ten",
			"2":          "two",
			"a":          "letter",
			"\U0001F600": "smile",
		},
		"\u2665": "heart",
	}
	serialized, err := CanonicalJSONString(value)
	if err != nil {
		t.Fatalf("CanonicalJSONString() error = %v", err)
	}
	if serialized != "{\"10\":\"outer-ten\",\"2\":\"outer-two\",\"a\":{\"10\":\"ten\",\"2\":\"two\",\"a\":\"letter\",\"😀\":\"smile\"},\"z\":1,\"♥\":\"heart\"}" {
		t.Fatalf("unexpected canonical order: %s", serialized)
	}
}

func TestCanonicalJSONStringRejectsUnsupportedValues(t *testing.T) {
	if _, err := CanonicalJSONString(func() {}); err == nil {
		t.Fatal("expected unsupported value error")
	}
	if _, err := CanonicalJSONString(map[string]any{"value": math.Inf(1)}); err == nil {
		t.Fatal("expected non-finite number error")
	}
	if _, err := CanonicalJSONString(map[string]any{"value": 0}); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if _, err := CanonicalJSONString(map[string]any{"value": math.Copysign(0, -1)}); err == nil {
		t.Fatal("expected negative zero error")
	}
	if _, err := CanonicalJSONString(map[string]any{"value": float64(1 << 60)}); err == nil {
		t.Fatal("expected unsafe integer error")
	}
}

func TestCanonicalJSONStringEscapesNumbersLikeECMAScript(t *testing.T) {
	value := map[string]any{"value": 2e-3, "score": 0.125, "big": 1000}
	canonical, err := CanonicalJSONString(value)
	if err != nil {
		t.Fatalf("CanonicalJSONString() error = %v", err)
	}
	if canonical != "{\"big\":1000,\"score\":0.125,\"value\":0.002}" {
		t.Fatalf("unexpected canonical output: %s", canonical)
	}

	expected, _ := hex.DecodeString("7b226261673a313030307d")
	if len(expected) == 0 {
		t.Fatalf("expected hex decode sanity check")
	}
}

func TestCanonicalJSONStringRejectsInvalidRuntimeMapKeys(t *testing.T) {
	highSurrogate := string([]byte{0xed, 0xa0, 0x80})
	if _, err := CanonicalJSONString(map[string]any{highSurrogate: 1}); err == nil {
		t.Fatal("expected lone high surrogate key rejection")
	}
	lowSurrogate := string([]byte{0xed, 0xb0, 0x80})
	if _, err := CanonicalJSONString(map[string]any{lowSurrogate: 1}); err == nil {
		t.Fatal("expected lone low surrogate key rejection")
	}
	if _, err := CanonicalJSONString(map[string]any{string([]byte{0xff, 0xfe, 'x'}): 1}); err == nil {
		t.Fatal("expected invalid UTF-8 map key rejection")
	}
}

func TestCanonicalJSONStringSupportsValidSupplementaryRuntimeKeys(t *testing.T) {
	serialized, err := CanonicalJSONString(map[string]any{"\U0001D11E": 1})
	if err != nil {
		t.Fatalf("CanonicalJSONString() error = %v", err)
	}
	if serialized != "{\"𝄞\":1}" {
		t.Fatalf("unexpected canonical output: %s", serialized)
	}
}

func TestCanonicalJSONStringWritesECMAScriptCompatibleFloatSpellings(t *testing.T) {
	tests := []struct {
		name             string
		value            float64
		expectedSpelling string
		shouldAccept     bool
	}{
		{name: "zero", value: 0, expectedSpelling: "0", shouldAccept: true},
		{name: "positive_fixed_1e-6", value: 0.000001, expectedSpelling: "0.000001", shouldAccept: true},
		{name: "negative_fixed_1e-6", value: -0.000001, expectedSpelling: "-0.000001", shouldAccept: true},
		{name: "positive_exponent_1e-7", value: 1e-7, expectedSpelling: "1e-7", shouldAccept: true},
		{name: "negative_exponent_1e-7", value: -1e-7, expectedSpelling: "-1e-7", shouldAccept: true},
		{name: "positive_near_threshold", value: 0.00000095, expectedSpelling: "9.5e-7", shouldAccept: true},
		{name: "negative_near_threshold", value: -0.00000095, expectedSpelling: "-9.5e-7", shouldAccept: true},
		{name: "positive_above_threshold", value: 0.0000015, expectedSpelling: "0.0000015", shouldAccept: true},
		{name: "negative_above_threshold", value: -0.0000015, expectedSpelling: "-0.0000015", shouldAccept: true},
		{name: "ordinary_fraction", value: 0.125, expectedSpelling: "0.125", shouldAccept: true},
		{name: "ordinary_negative_fraction", value: -0.125, expectedSpelling: "-0.125", shouldAccept: true},
		{name: "round_trip_ratio", value: 1.25, expectedSpelling: "1.25", shouldAccept: true},
		{name: "round_trip_negative_ratio", value: -1.25, expectedSpelling: "-1.25", shouldAccept: true},
		{name: "large_finite_not_integer", value: 999999.999999, expectedSpelling: "999999.999999", shouldAccept: true},
		{name: "large_finite_not_integer_negative", value: -999999.999999, expectedSpelling: "-999999.999999", shouldAccept: true},
		{name: "very_small_normal", value: 2.2250738585072014e-308, expectedSpelling: "2.2250738585072014e-308", shouldAccept: true},
		{name: "very_small_normal_negative", value: -2.2250738585072014e-308, expectedSpelling: "-2.2250738585072014e-308", shouldAccept: true},
		{name: "fixed_boundary_positive", value: 1e20, expectedSpelling: "100000000000000000000", shouldAccept: false},
		{name: "fixed_boundary_negative", value: -1e20, expectedSpelling: "-100000000000000000000", shouldAccept: false},
		{name: "exponent_boundary_positive", value: 1e21, expectedSpelling: "1e+21", shouldAccept: false},
		{name: "exponent_boundary_negative", value: -1e21, expectedSpelling: "-1e+21", shouldAccept: false},
		{name: "very_small", value: 1e-10, expectedSpelling: "1e-10", shouldAccept: true},
		{name: "small_normal", value: 5e-324, expectedSpelling: "5e-324", shouldAccept: true},
		{name: "small_positive_normal", value: 2.2250738585072014e-308, expectedSpelling: "2.2250738585072014e-308", shouldAccept: true},
		{name: "very_large_finite", value: 1.7976931348623157e+308, expectedSpelling: "1.7976931348623157e+308", shouldAccept: false},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(tc.name, func(t *testing.T) {
			actual := canonicalJSONNumber(tc.value)
			if actual != tc.expectedSpelling {
				t.Fatalf("unexpected canonical spelling for %v: got %q expected %q", tc.value, actual, tc.expectedSpelling)
			}
			if tc.shouldAccept {
				serialized, err := CanonicalJSONString(map[string]any{"value": tc.value})
				if err != nil {
					t.Fatalf("CanonicalJSONString() error = %v", err)
				}
				if serialized != "{\"value\":"+tc.expectedSpelling+"}" {
					t.Fatalf("unexpected canonical JSON for %v: %q", tc.value, serialized)
				}
			} else {
				if _, err := CanonicalJSONString(map[string]any{"value": tc.value}); err == nil {
					t.Fatalf("expected unsafe integer rejection for %v", tc.value)
				}
			}
		})
	}
}

func TestCanonicalJSONStringNumberFormatterThresholdBytesAndHashes(t *testing.T) {
	tests := []struct {
		value            float64
		expectedSpelling string
		canonicalHex     string
		canonicalSHA     string
	}{
		{
			value:            1e20,
			expectedSpelling: "100000000000000000000",
			canonicalHex:     "7b2276223a3130303030303030303030303030303030303030307d",
			canonicalSHA:     "8e85c1241e1aebc42bf95fcc16c0fef8b6d1de48d57f31096b9a7be664c2cebb",
		},
		{
			value:            1e21,
			expectedSpelling: "1e+21",
			canonicalHex:     "7b2276223a31652b32317d",
			canonicalSHA:     "9e24d139b060bd89f98e510a1e34d48fade6aeb668a016e6fb2e296762a3aefa",
		},
		{
			value:            -1e20,
			expectedSpelling: "-100000000000000000000",
			canonicalHex:     "7b2276223a2d3130303030303030303030303030303030303030307d",
			canonicalSHA:     "afc9b072f80459d6f66c82bf9dfe00d5b152c945b67d6a02e9ac640965802fee",
		},
		{
			value:            -1e21,
			expectedSpelling: "-1e+21",
			canonicalHex:     "7b2276223a2d31652b32317d",
			canonicalSHA:     "e2c8234e1548bc80ba238ba3913aeb55c4c324df829c9c0641b7966a8a9b1067",
		},
	}

	for _, tc := range tests {
		tc := tc
		t.Run(fmt.Sprintf("%v", tc.value), func(t *testing.T) {
			actual := canonicalJSONNumber(tc.value)
			if actual != tc.expectedSpelling {
				t.Fatalf("unexpected canonical spelling for %v: got %q expected %q", tc.value, actual, tc.expectedSpelling)
			}

			canonicalLine := []byte(fmt.Sprintf("{\"v\":%s}", actual))
			if got := hex.EncodeToString(canonicalLine); got != tc.canonicalHex {
				t.Fatalf("unexpected canonical hex for %v: got %s expected %s", tc.value, got, tc.canonicalHex)
			}

			hasher := sha256.Sum256(canonicalLine)
			got := hex.EncodeToString(hasher[:])
			if got != tc.canonicalSHA {
				t.Fatalf("unexpected canonical sha for %v: got %s expected %s", tc.value, got, tc.canonicalSHA)
			}
		})
	}
}
