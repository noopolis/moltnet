package protocol

import (
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

const CausalCanonicalJSONVersion = "noopolis.canonical-json.v1"
const maxSafeInteger = 9007199254740991

// ParseCanonicalJSON parses strict canonical-json.v1 input and validates
// canonical invariants while preserving value semantics.
func ParseCanonicalJSON(input string) (any, error) {
	trimmed := trimJsonWhitespace(input)
	if trimmed == "" {
		return nil, fmt.Errorf("empty JSON")
	}

	parser := &canonicalJSONParser{
		data: []byte(trimmed),
	}
	value, err := parser.parseValue()
	if err != nil {
		return nil, err
	}
	if parser.i < len(parser.data) {
		return nil, fmt.Errorf("unexpected trailing content at position %d", parser.i)
	}

	normalized, err := normalizeCanonicalValue(value)
	if err != nil {
		return nil, err
	}
	return normalized, nil
}

// ParseCanonicalJSONBytes parses strict canonical-json.v1 input bytes.
func ParseCanonicalJSONBytes(input []byte) (any, error) {
	if hasBOM(input) {
		return nil, fmt.Errorf("invalid UTF-8: leading BOM is not allowed")
	}
	if !utf8.Valid(input) {
		return nil, fmt.Errorf("invalid UTF-8: malformed UTF-8 sequence")
	}
	return ParseCanonicalJSON(string(input))
}

// CanonicalJSONString serializes supported values using strict canonical JSON
// object-key ordering, deterministic number form, and ECMAScript-style escaping.
func CanonicalJSONString(value any) (string, error) {
	normalized, err := normalizeCanonicalValue(value)
	if err != nil {
		return "", err
	}

	var builder strings.Builder
	if err := writeCanonicalJSONValue(&builder, normalized); err != nil {
		return "", err
	}
	return builder.String(), nil
}

// CanonicalJSONBytes serializes strict canonical JSON as UTF-8.
func CanonicalJSONBytes(value any) ([]byte, error) {
	text, err := CanonicalJSONString(value)
	if err != nil {
		return nil, err
	}
	return []byte(text), nil
}

func isJsonWhitespace(char byte) bool {
	return char == ' ' || char == '\t' || char == '\n' || char == '\r'
}

func trimJsonWhitespace(input string) string {
	start := 0
	for start < len(input) && isJsonWhitespace(input[start]) {
		start += 1
	}
	end := len(input)
	for end > start && isJsonWhitespace(input[end-1]) {
		end -= 1
	}
	return input[start:end]
}

func hasBOM(bytes []byte) bool {
	return len(bytes) >= 3 && bytes[0] == 0xef && bytes[1] == 0xbb && bytes[2] == 0xbf
}

func validateCanonicalNumber(value float64) error {
	if math.IsNaN(value) || math.IsInf(value, 0) {
		return fmt.Errorf("non-finite number")
	}
	if math.Signbit(value) && value == 0 {
		return fmt.Errorf("negative zero")
	}
	if math.Trunc(value) == value && (value < -maxSafeInteger || value > maxSafeInteger) {
		return fmt.Errorf("unsafe integer")
	}
	return nil
}

func validateCanonicalString(value string) error {
	for index := 0; index < len(value); {
		r, size := utf8.DecodeRuneInString(value[index:])
		if r >= 0xD800 && r <= 0xDBFF {
			return fmt.Errorf("lone high surrogate")
		}
		if r >= 0xDC00 && r <= 0xDFFF {
			return fmt.Errorf("lone low surrogate")
		}
		if r == utf8.RuneError && size == 1 {
			return fmt.Errorf("invalid UTF-8")
		}
		index += size
	}
	return nil
}

func normalizeCanonicalValue(value any) (any, error) {
	switch typed := value.(type) {
	case nil:
		return nil, nil
	case bool:
		return typed, nil
	case int:
		return asCanonicalNumber(float64(typed))
	case int8:
		return asCanonicalNumber(float64(typed))
	case int16:
		return asCanonicalNumber(float64(typed))
	case int32:
		return asCanonicalNumber(float64(typed))
	case int64:
		return asCanonicalNumber(float64(typed))
	case uint:
		return asCanonicalNumber(float64(typed))
	case uint8:
		return asCanonicalNumber(float64(typed))
	case uint16:
		return asCanonicalNumber(float64(typed))
	case uint32:
		return asCanonicalNumber(float64(typed))
	case uint64:
		return asCanonicalNumber(float64(typed))
	case float32:
		return asCanonicalNumber(float64(typed))
	case float64:
		return asCanonicalNumber(typed)
	case string:
		if err := validateCanonicalString(typed); err != nil {
			return nil, err
		}
		return typed, nil
	case []any:
		canonical := make([]any, len(typed))
		for index, element := range typed {
			child, err := normalizeCanonicalValue(element)
			if err != nil {
				return nil, err
			}
			canonical[index] = child
		}
		return canonical, nil
	case map[string]any:
		canonical := make(map[string]any, len(typed))
		for key, child := range typed {
			normalized, err := normalizeCanonicalValue(child)
			if err != nil {
				return nil, err
			}
			canonical[key] = normalized
		}
		return canonical, nil
	default:
		return nil, fmt.Errorf("unsupported canonical JSON type %T", value)
	}
}

func asCanonicalNumber(value float64) (any, error) {
	if err := validateCanonicalNumber(value); err != nil {
		return nil, err
	}
	return value, nil
}

func writeCanonicalJSONValue(builder *strings.Builder, value any) error {
	switch typed := value.(type) {
	case nil:
		builder.WriteString("null")
	case bool:
		if typed {
			builder.WriteString("true")
		} else {
			builder.WriteString("false")
		}
	case float64:
		builder.WriteString(canonicalJSONNumber(typed))
	case string:
		if err := encodeCanonicalJSONString(builder, typed); err != nil {
			return err
		}
	case []any:
		builder.WriteByte('[')
		for index, child := range typed {
			if index > 0 {
				builder.WriteByte(',')
			}
			if err := writeCanonicalJSONValue(builder, child); err != nil {
				return err
			}
		}
		builder.WriteByte(']')
	case map[string]any:
		builder.WriteByte('{')
		keys := make([]string, 0, len(typed))
		for key := range typed {
			keys = append(keys, key)
		}
		sort.Slice(keys, func(i, j int) bool {
			return utf16Less(keys[i], keys[j])
		})
		for index, key := range keys {
			if index > 0 {
				builder.WriteByte(',')
			}
			if err := encodeCanonicalJSONString(builder, key); err != nil {
				return err
			}
			builder.WriteByte(':')
			if err := writeCanonicalJSONValue(builder, typed[key]); err != nil {
				return err
			}
		}
		builder.WriteByte('}')
	default:
		return fmt.Errorf("unsupported canonical JSON type %T", value)
	}
	return nil
}

func canonicalJSONNumber(value float64) string {
	if value == 0 {
		return "0"
	}

	absolute := math.Abs(value)
	if absolute >= 1e-6 && absolute < 1e21 {
		return strconv.FormatFloat(value, 'f', -1, 64)
	}

	return formatExponentFloat(value)
}

func formatExponentFloat(value float64) string {
	raw := strconv.FormatFloat(value, 'e', -1, 64)
	exponentIndex := strings.IndexByte(raw, 'e')
	if exponentIndex == -1 {
		return raw
	}
	exponent := raw[exponentIndex+1:]

	exponentSign := "+"
	if len(exponent) > 0 && (exponent[0] == '+' || exponent[0] == '-') {
		exponentSign = string(exponent[0])
		exponent = exponent[1:]
	}
	exponent = strings.TrimLeft(exponent, "0")
	if exponent == "" {
		exponent = "0"
	}

	return raw[:exponentIndex] + "e" + exponentSign + exponent
}

func utf16Less(left string, right string) bool {
	leftUnits := utf16.Encode([]rune(left))
	rightUnits := utf16.Encode([]rune(right))
	max := len(leftUnits)
	if len(rightUnits) < max {
		max = len(rightUnits)
	}

	for index := 0; index < max; index += 1 {
		if leftUnits[index] == rightUnits[index] {
			continue
		}
		return leftUnits[index] < rightUnits[index]
	}
	return len(leftUnits) < len(rightUnits)
}

func encodeCanonicalJSONString(builder *strings.Builder, value string) error {
	formatUTF16 := func(char rune) string {
		return fmt.Sprintf("%04x", char)
	}

	builder.WriteByte('"')
	for _, char := range value {
		switch char {
		case '"':
			builder.WriteString(`\"`)
		case '\\':
			builder.WriteString(`\\`)
		case '\b':
			builder.WriteString(`\b`)
		case '\f':
			builder.WriteString(`\f`)
		case '\n':
			builder.WriteString(`\n`)
		case '\r':
			builder.WriteString(`\r`)
		case '\t':
			builder.WriteString(`\t`)
		default:
			if char >= 0x00 && char <= 0x1f {
				builder.WriteString(`\u00`)
				builder.WriteString(formatUTF16(char))
			} else {
				builder.WriteRune(char)
			}
		}
	}
	builder.WriteByte('"')
	return nil
}
