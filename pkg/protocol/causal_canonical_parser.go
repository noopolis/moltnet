package protocol

import (
	"fmt"
	"strconv"
	"strings"
	"unicode/utf16"
	"unicode/utf8"
)

// canonicalJSONParser owns strict JSON tokenization and parsing. Public
// normalization and serialization stay in causal_canonical.go.
type canonicalJSONParser struct {
	data []byte
	i    int
}

func (parser *canonicalJSONParser) parseValue() (any, error) {
	parser.skipWhitespace()
	if parser.i >= len(parser.data) {
		return nil, fmt.Errorf("unexpected end of JSON")
	}

	switch char := parser.data[parser.i]; char {
	case '{':
		return parser.parseObject()
	case '[':
		return parser.parseArray()
	case '"':
		return parser.parseString()
	case '-':
		return parser.parseNumber()
	default:
		if isDigit(char) {
			return parser.parseNumber()
		}
		if parser.startsWith("true") {
			parser.i += 4
			return true, nil
		}
		if parser.startsWith("false") {
			parser.i += 5
			return false, nil
		}
		if parser.startsWith("null") {
			parser.i += 4
			return nil, nil
		}
		return nil, fmt.Errorf("invalid token at position %d", parser.i)
	}
}

func (parser *canonicalJSONParser) parseObject() (any, error) {
	parser.i++
	parser.skipWhitespace()

	object := make(map[string]any)
	keys := map[string]struct{}{}
	for {
		if parser.i >= len(parser.data) {
			return nil, fmt.Errorf("unterminated object")
		}
		if parser.data[parser.i] == '}' {
			parser.i++
			return object, nil
		}

		key, err := parser.parseString()
		if err != nil {
			return nil, err
		}
		if _, exists := keys[key]; exists {
			return nil, fmt.Errorf("duplicate key %q", key)
		}
		keys[key] = struct{}{}
		if parser.i >= len(parser.data) || parser.data[parser.i] != ':' {
			return nil, fmt.Errorf("invalid object key/value delimiter at position %d", parser.i)
		}
		parser.i++
		parser.skipWhitespace()

		value, err := parser.parseValue()
		if err != nil {
			return nil, err
		}
		object[key] = value
		parser.skipWhitespace()
		if parser.i >= len(parser.data) {
			return nil, fmt.Errorf("unterminated object")
		}
		switch parser.data[parser.i] {
		case ',':
			parser.i++
			parser.skipWhitespace()
			if parser.i >= len(parser.data) || parser.data[parser.i] == '}' {
				return nil, fmt.Errorf("invalid trailing comma at position %d", parser.i)
			}
		case '}':
			parser.i++
			return object, nil
		default:
			return nil, fmt.Errorf("invalid object terminator at position %d", parser.i)
		}
	}
}

func (parser *canonicalJSONParser) parseArray() (any, error) {
	parser.i++
	parser.skipWhitespace()

	var values []any
	for {
		if parser.i >= len(parser.data) {
			return nil, fmt.Errorf("unterminated array")
		}
		if parser.data[parser.i] == ']' {
			parser.i++
			return values, nil
		}

		value, err := parser.parseValue()
		if err != nil {
			return nil, err
		}
		values = append(values, value)
		parser.skipWhitespace()
		if parser.i >= len(parser.data) {
			return nil, fmt.Errorf("unterminated array")
		}
		switch parser.data[parser.i] {
		case ',':
			parser.i++
			parser.skipWhitespace()
			if parser.i >= len(parser.data) || parser.data[parser.i] == ']' {
				return nil, fmt.Errorf("invalid trailing comma at position %d", parser.i)
			}
		case ']':
			parser.i++
			return values, nil
		default:
			return nil, fmt.Errorf("invalid array terminator at position %d", parser.i)
		}
	}
}

func (parser *canonicalJSONParser) parseString() (string, error) {
	if parser.i >= len(parser.data) || parser.data[parser.i] != '"' {
		return "", fmt.Errorf("invalid string start at position %d", parser.i)
	}
	parser.i++

	var builder strings.Builder
	for {
		if parser.i >= len(parser.data) {
			return "", fmt.Errorf("unterminated string at position %d", parser.i)
		}
		char := parser.data[parser.i]
		if char == '"' {
			parser.i++
			return builder.String(), nil
		}
		if char == '\\' {
			parser.i++
			if parser.i >= len(parser.data) {
				return "", fmt.Errorf("unterminated escape at position %d", parser.i-1)
			}
			escaped := parser.data[parser.i]
			switch escaped {
			case '"':
				builder.WriteByte('"')
				parser.i++
			case '\\':
				builder.WriteByte('\\')
				parser.i++
			case '/':
				builder.WriteByte('/')
				parser.i++
			case 'b':
				builder.WriteByte('\b')
				parser.i++
			case 'f':
				builder.WriteByte('\f')
				parser.i++
			case 'n':
				builder.WriteByte('\n')
				parser.i++
			case 'r':
				builder.WriteByte('\r')
				parser.i++
			case 't':
				builder.WriteByte('\t')
				parser.i++
			case 'u':
				parser.i++
				r, err := parser.parseUnicodeEscape()
				if err != nil {
					return "", err
				}
				builder.WriteRune(r)
			default:
				return "", fmt.Errorf("invalid escape \\%c at position %d", escaped, parser.i-1)
			}
			continue
		}
		if char < 0x20 {
			return "", fmt.Errorf("control character must be escaped at position %d", parser.i)
		}

		r, size := utf8.DecodeRune(parser.data[parser.i:])
		if r == utf8.RuneError && size == 1 {
			return "", fmt.Errorf("invalid UTF-8 at position %d", parser.i)
		}
		if r >= 0xD800 && r <= 0xDBFF {
			return "", fmt.Errorf("invalid high surrogate at position %d", parser.i)
		}
		if r >= 0xDC00 && r <= 0xDFFF {
			return "", fmt.Errorf("invalid low surrogate at position %d", parser.i)
		}
		builder.WriteRune(r)
		parser.i += size
	}
}

func (parser *canonicalJSONParser) parseUnicodeEscape() (rune, error) {
	high, err := parser.parseHex4()
	if err != nil {
		return 0, err
	}
	if high < 0xD800 || high > 0xDBFF {
		return high, nil
	}
	if parser.i >= len(parser.data) || parser.data[parser.i] != '\\' || parser.i+1 >= len(parser.data) || parser.data[parser.i+1] != 'u' {
		return 0, fmt.Errorf("invalid high surrogate at position %d", parser.i)
	}
	parser.i += 2
	low, err := parser.parseHex4()
	if err != nil {
		return 0, err
	}
	if low < 0xDC00 || low > 0xDFFF {
		return 0, fmt.Errorf("invalid low surrogate at position %d", parser.i-4)
	}
	return utf16.DecodeRune(high, low), nil
}

func (parser *canonicalJSONParser) parseHex4() (rune, error) {
	var value rune
	for range 4 {
		if parser.i >= len(parser.data) {
			return 0, fmt.Errorf("invalid hex escape at position %d", parser.i)
		}
		char := parser.data[parser.i]
		parser.i++
		var digit int
		switch {
		case char >= '0' && char <= '9':
			digit = int(char - '0')
		case char >= 'a' && char <= 'f':
			digit = int(char-'a') + 10
		case char >= 'A' && char <= 'F':
			digit = int(char-'A') + 10
		default:
			return 0, fmt.Errorf("invalid hex escape at position %d", parser.i-1)
		}
		value = (value << 4) + rune(digit)
	}
	return value, nil
}

func (parser *canonicalJSONParser) parseNumber() (any, error) {
	start := parser.i
	if parser.i < len(parser.data) && parser.data[parser.i] == '-' {
		parser.i++
	}
	if parser.i >= len(parser.data) {
		return nil, fmt.Errorf("invalid number at position %d", start)
	}
	if parser.data[parser.i] == '0' {
		parser.i++
		if parser.i < len(parser.data) && isDigit(parser.data[parser.i]) {
			return nil, fmt.Errorf("invalid number at position %d", start)
		}
	} else if isDigit(parser.data[parser.i]) {
		for parser.i < len(parser.data) && isDigit(parser.data[parser.i]) {
			parser.i++
		}
	} else {
		return nil, fmt.Errorf("invalid number at position %d", start)
	}
	if parser.i < len(parser.data) && parser.data[parser.i] == '.' {
		parser.i++
		if parser.i >= len(parser.data) || !isDigit(parser.data[parser.i]) {
			return nil, fmt.Errorf("invalid number fraction at position %d", start)
		}
		for parser.i < len(parser.data) && isDigit(parser.data[parser.i]) {
			parser.i++
		}
	}
	if parser.i < len(parser.data) && (parser.data[parser.i] == 'e' || parser.data[parser.i] == 'E') {
		parser.i++
		if parser.i < len(parser.data) && (parser.data[parser.i] == '+' || parser.data[parser.i] == '-') {
			parser.i++
		}
		if parser.i >= len(parser.data) || !isDigit(parser.data[parser.i]) {
			return nil, fmt.Errorf("invalid number exponent at position %d", start)
		}
		for parser.i < len(parser.data) && isDigit(parser.data[parser.i]) {
			parser.i++
		}
	}

	raw := string(parser.data[start:parser.i])
	value, err := strconv.ParseFloat(raw, 64)
	if err != nil {
		return nil, fmt.Errorf("invalid number %q at position %d", raw, start)
	}
	if err := validateCanonicalNumber(value); err != nil {
		return nil, fmt.Errorf("%s at position %d", err, start)
	}
	return value, nil
}

func (parser *canonicalJSONParser) skipWhitespace() {
	for parser.i < len(parser.data) && isJsonWhitespace(parser.data[parser.i]) {
		parser.i++
	}
}

func (parser *canonicalJSONParser) startsWith(prefix string) bool {
	if len(prefix) > len(parser.data)-parser.i {
		return false
	}
	return string(parser.data[parser.i:parser.i+len(prefix)]) == prefix
}

func isDigit(char byte) bool {
	return char >= '0' && char <= '9'
}
