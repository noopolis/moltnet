package protocol

import (
	"bytes"
	"encoding/json"
	"fmt"
	"log/slog"
	"strings"
	"testing"

	"go.yaml.in/yaml/v3"
)

func TestSecretStringRedactsOutput(t *testing.T) {
	secret := NewSecretString("top-secret")

	encoded, err := json.Marshal(secret)
	if err != nil {
		t.Fatalf("marshal JSON: %v", err)
	}
	if got, want := string(encoded), `"[REDACTED]"`; got != want {
		t.Fatalf("JSON = %q, want %q", got, want)
	}

	encoded, err = yaml.Marshal(secret)
	if err != nil {
		t.Fatalf("marshal YAML: %v", err)
	}
	if got := string(encoded); !strings.Contains(got, RedactedSecretString) || strings.Contains(got, "top-secret") {
		t.Fatalf("YAML leaked secret: %q", got)
	}

	for _, formatted := range []string{fmt.Sprintf("%v", secret), fmt.Sprintf("%s", secret), fmt.Sprintf("%#v", secret)} {
		if formatted != RedactedSecretString {
			t.Fatalf("formatted secret = %q, want redaction", formatted)
		}
	}

	var logs bytes.Buffer
	slog.New(slog.NewTextHandler(&logs, nil)).Info("secret test", "token", secret)
	if text := logs.String(); strings.Contains(text, "top-secret") || !strings.Contains(text, RedactedSecretString) {
		t.Fatalf("slog leaked secret: %q", text)
	}
}

func TestSecretStringDecodesPlaintext(t *testing.T) {
	for _, input := range []string{`"json-secret"`, "yaml-secret\n"} {
		var secret SecretString
		var err error
		if strings.HasPrefix(input, `"`) {
			err = json.Unmarshal([]byte(input), &secret)
		} else {
			err = yaml.Unmarshal([]byte(input), &secret)
		}
		if err != nil {
			t.Fatalf("decode %q: %v", input, err)
		}
		if got := secret.Reveal(); got != strings.TrimSuffix(strings.Trim(input, `"`), "\n") {
			t.Fatalf("Reveal() = %q", got)
		}
	}
}
