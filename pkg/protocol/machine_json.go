package protocol

import (
	"bytes"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"strings"
)

func decodeJSONEnvelope(raw string) (map[string]json.RawMessage, error) {
	record := strings.TrimSpace(raw)
	if record == "" {
		return nil, errors.New("empty input")
	}
	if err := validateNoDuplicateJSONValues([]byte(record)); err != nil {
		return nil, errors.New("invalid JSON")
	}

	dec := json.NewDecoder(strings.NewReader(record))
	var envelope map[string]json.RawMessage
	if err := dec.Decode(&envelope); err != nil {
		return nil, errors.New("invalid JSON")
	}
	if err := ensureSingleJSONValueDecoder(dec); err != nil {
		return nil, errors.New("invalid JSON")
	}
	if envelope == nil {
		return nil, errors.New("invalid JSON")
	}
	return envelope, nil
}

func decodeStrictJSONValue(raw json.RawMessage, target any) error {
	record := bytes.TrimSpace(raw)
	if len(record) == 0 || isNull(record) {
		return errors.New("missing payload")
	}
	if err := validateNoDuplicateJSONValues(record); err != nil {
		return errors.New("invalid JSON")
	}
	dec := json.NewDecoder(bytes.NewReader(record))
	dec.DisallowUnknownFields()
	if err := dec.Decode(target); err != nil {
		return errors.New("invalid JSON")
	}
	if err := ensureSingleJSONValueDecoder(dec); err != nil {
		return errors.New("invalid JSON")
	}
	return nil
}

func isNull(raw json.RawMessage) bool {
	recorded := strings.TrimSpace(string(raw))
	return recorded == "" || recorded == "null"
}

func ensureSingleJSONValue(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	var first struct{}
	if err := dec.Decode(&first); err != nil {
		return err
	}
	return ensureSingleJSONValueDecoder(dec)
}

func ensureSingleJSONValueDecoder(dec *json.Decoder) error {
	var terminal struct{}
	if err := dec.Decode(&terminal); err != nil {
		if err == io.EOF {
			return nil
		}
		return err
	}
	return fmt.Errorf("multiple JSON values")
}

func validateNoDuplicateJSONValues(raw []byte) error {
	dec := json.NewDecoder(bytes.NewReader(raw))
	if err := validateNoDuplicateJSONValue(dec, "root"); err != nil {
		return err
	}
	return ensureSingleJSONValueDecoder(dec)
}

func validateNoDuplicateJSONValue(dec *json.Decoder, path string) error {
	tok, err := dec.Token()
	if err != nil {
		return err
	}

	switch delim := tok.(type) {
	case json.Delim:
		switch delim {
		case '{':
			return validateNoDuplicateJSONObject(dec, path)
		case '[':
			return validateNoDuplicateJSONArray(dec, path)
		default:
			return fmt.Errorf("invalid JSON structure")
		}
	default:
		return nil
	}
}

func validateNoDuplicateJSONObject(dec *json.Decoder, path string) error {
	seen := make(map[string]struct{})
	for dec.More() {
		keyToken, err := dec.Token()
		if err != nil {
			return err
		}
		key, ok := keyToken.(string)
		if !ok {
			return fmt.Errorf("invalid object key")
		}
		if _, exists := seen[key]; exists {
			return fmt.Errorf("duplicate key %q", key)
		}
		seen[key] = struct{}{}
		if err := validateNoDuplicateJSONValue(dec, path+"."+key); err != nil {
			return err
		}
	}
	trailer, err := dec.Token()
	if err != nil {
		return err
	}
	if trailer != json.Delim('}') {
		return fmt.Errorf("expected '}'")
	}
	return nil
}

func validateNoDuplicateJSONArray(dec *json.Decoder, path string) error {
	index := 0
	for dec.More() {
		if err := validateNoDuplicateJSONValue(dec, path+"["+itoa(index)+"]"); err != nil {
			return err
		}
		index++
	}
	trailer, err := dec.Token()
	if err != nil {
		return err
	}
	if trailer != json.Delim(']') {
		return fmt.Errorf("expected ']'")
	}
	return nil
}

func itoa(value int) string {
	if value == 0 {
		return "0"
	}
	buffer := make([]byte, 0, 20)
	for value > 0 {
		buffer = append([]byte{byte('0' + value%10)}, buffer...)
		value /= 10
	}
	return string(buffer)
}
