package protocol

import (
	"encoding/json"
	"fmt"

	"go.yaml.in/yaml/v3"
)

// RedactedSecretString is the fixed value used when a SecretString is
// serialized or formatted. It is intentionally not accepted as a special
// input value: an input string is always treated as the supplied plaintext.
const RedactedSecretString = "[REDACTED]"

// SecretString holds plaintext that must never be exposed by normal
// serialization or formatting. Reveal is the sole explicit escape hatch for
// code that must send the secret to its intended destination.
type SecretString string

// NewSecretString constructs a SecretString from plaintext supplied by a
// trusted caller, such as a config decoder or command-line flag.
func NewSecretString(plaintext string) SecretString {
	return SecretString(plaintext)
}

// Reveal returns the plaintext. Callers should keep its use at the immediate
// boundary where the secret is required.
func (s SecretString) Reveal() string {
	return string(s)
}

// MarshalJSON prevents a SecretString from leaking into API responses.
func (SecretString) MarshalJSON() ([]byte, error) {
	return json.Marshal(RedactedSecretString)
}

// UnmarshalJSON accepts a plaintext JSON string when loading trusted input.
func (s *SecretString) UnmarshalJSON(data []byte) error {
	var plaintext string
	if err := json.Unmarshal(data, &plaintext); err != nil {
		return fmt.Errorf("decode secret string: %w", err)
	}
	*s = SecretString(plaintext)
	return nil
}

// MarshalYAML prevents a SecretString from leaking into generated YAML.
func (SecretString) MarshalYAML() (interface{}, error) {
	return RedactedSecretString, nil
}

// UnmarshalYAML accepts a plaintext YAML string when loading trusted input.
func (s *SecretString) UnmarshalYAML(value *yaml.Node) error {
	if value.Kind != yaml.ScalarNode || value.Tag != "!!str" {
		return fmt.Errorf("decode secret string: expected string")
	}
	*s = SecretString(value.Value)
	return nil
}

// String prevents fmt and slog from formatting the plaintext.
func (SecretString) String() string {
	return RedactedSecretString
}

// GoString prevents %#v from formatting the plaintext.
func (SecretString) GoString() string {
	return RedactedSecretString
}
