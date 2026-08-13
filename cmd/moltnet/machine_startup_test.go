package main

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestMachineStartupFailuresAreRedacted(t *testing.T) {
	const (
		configMissing = "CFG_MISSING_8f0f9e17"
		configDir     = "CFG_DIRECTORY_593a4c2e"
		configBad     = "CFG_MALFORMED_14d97b6a"
		tokenEnv      = "TOKEN_ENV_3ca2d8e9"
		tokenFile     = "TOKEN_FILE_6be430f1"
		network       = "NET_PRIVATE_249fd1c8"
		member        = "MEMBER_PRIVATE_a7e6b203"
	)
	temp := t.TempDir()
	malformed := filepath.Join(temp, configBad+".json")
	if err := os.WriteFile(malformed, []byte(`{"version":"`+configBad+`"}`), 0o600); err != nil {
		t.Fatalf("write malformed config: %v", err)
	}
	valid := func(auth string, attachments string) string {
		if attachments != "" {
			return `{"version":"moltnet.client.v1","attachments":` + attachments + `}`
		}
		return `{"version":"moltnet.client.v1","attachments":[{"base_url":"https://startup.invalid","network_id":"net","member_id":"member","auth":` + auth + `}]}`
	}
	write := func(name, contents string) string {
		path := filepath.Join(temp, name+".json")
		if err := os.WriteFile(path, []byte(contents), 0o600); err != nil {
			t.Fatalf("write %s: %v", name, err)
		}
		return path
	}
	tokenEnvPath := write("token-env", valid(`{"mode":"bearer","token_env":"`+tokenEnv+`"}`, ""))
	tokenFilePath := write("token-file", valid(`{"mode":"bearer","token_path":"`+tokenFile+`"}`, ""))
	selectionPath := write("selection", valid("", `[
  {"base_url":"https://startup.invalid/a","network_id":"`+network+`","member_id":"`+member+`","auth":{"mode":"open"}},
  {"base_url":"https://startup.invalid/b","network_id":"`+network+`","member_id":"member-other","auth":{"mode":"open"}},
  {"base_url":"https://startup.invalid/c","network_id":"network-other","member_id":"`+member+`","auth":{"mode":"open"}}
]`))
	privateCorpus := []string{
		configMissing, configDir, configBad, malformed, tokenEnv, tokenFile,
		filepath.Join(temp, tokenFile), network, member, "MEMBER_NO_MATCH_d81f4a90",
		"https://startup.invalid",
	}

	cases := []struct {
		name      string
		args      []string
		configure func(*testing.T)
	}{
		{"missing explicit config", []string{"--config", filepath.Join(temp, configMissing)}, nil},
		{"directory explicit config", []string{"--config", filepath.Join(temp, configDir)}, func(t *testing.T) {
			if err := os.Mkdir(filepath.Join(temp, configDir), 0o700); err != nil {
				t.Fatal(err)
			}
		}},
		{"malformed config", []string{"--config", malformed}, nil},
		{"missing token environment", []string{"--config", tokenEnvPath}, func(t *testing.T) { t.Setenv(tokenEnv, "") }},
		{"missing token file", []string{"--config", tokenFilePath}, nil},
		{"no matching attachment", []string{"--config", selectionPath, "--network", network, "--member", "MEMBER_NO_MATCH_d81f4a90"}, nil},
		{"ambiguous attachment", []string{"--config", selectionPath, "--network", network}, nil},
	}

	for _, test := range cases {
		t.Run(test.name, func(t *testing.T) {
			if test.configure != nil {
				test.configure(t)
			}
			var output bytes.Buffer
			err := runMachineWithIO(context.Background(), test.args, machineIO{input: io.NopCloser(strings.NewReader("")), output: &output})
			if err != errMachineStartup || err.Error() != "machine startup failed" {
				t.Fatalf("startup error = %v", err)
			}
			value := err.Error() + output.String()
			for _, secret := range privateCorpus {
				if strings.Contains(value, secret) {
					t.Fatalf("startup leaked %q in %q", secret, value)
				}
			}
		})
	}
}
