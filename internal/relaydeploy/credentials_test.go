package relaydeploy

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestCredentialsPathIsUnderDotMoltnetNextToConfig(t *testing.T) {
	t.Parallel()
	got := CredentialsPath("/home/alice/project/Moltnet")
	want := filepath.Join("/home/alice/project", ".moltnet", "relay.json")
	if got != want {
		t.Fatalf("CredentialsPath() = %q, want %q", got, want)
	}
}

func TestLoadCredentialsMissingFileIsNotAnError(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := CredentialsPath(filepath.Join(directory, "Moltnet"))

	creds, ok, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v, want nil for a missing file", err)
	}
	if ok {
		t.Fatal("LoadCredentials() ok = true, want false for a missing file")
	}
	if creds != (RelayCredentials{}) {
		t.Fatalf("LoadCredentials() creds = %#v, want zero value", creds)
	}
}

func TestSaveAndLoadCredentialsRoundTrip(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := CredentialsPath(filepath.Join(directory, "Moltnet"))

	want := RelayCredentials{URL: "wss://moltnet-relay.acme.workers.dev", Token: "relay-secret-value", ScriptName: "moltnet-relay"}
	if err := SaveCredentials(path, want); err != nil {
		t.Fatalf("SaveCredentials() error = %v", err)
	}

	got, ok, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}
	if !ok {
		t.Fatal("LoadCredentials() ok = false after a successful save")
	}
	if got != want {
		t.Fatalf("LoadCredentials() = %#v, want %#v", got, want)
	}

	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatalf("stat credentials file: %v", err)
		}
		if perm := info.Mode().Perm(); perm != 0o600 {
			t.Fatalf("credentials file mode = %o, want 0600", perm)
		}
	}
}

func TestSaveCredentialsOverwritesExisting(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := CredentialsPath(filepath.Join(directory, "Moltnet"))

	if err := SaveCredentials(path, RelayCredentials{URL: "wss://old.workers.dev", Token: "old-token"}); err != nil {
		t.Fatalf("first SaveCredentials() error = %v", err)
	}
	want := RelayCredentials{URL: "wss://new.workers.dev", Token: "new-token"}
	if err := SaveCredentials(path, want); err != nil {
		t.Fatalf("second SaveCredentials() error = %v", err)
	}

	got, ok, err := LoadCredentials(path)
	if err != nil || !ok {
		t.Fatalf("LoadCredentials() = (%#v, %v, %v)", got, ok, err)
	}
	if got != want {
		t.Fatalf("LoadCredentials() = %#v, want %#v", got, want)
	}
}

func TestMatchesScriptNameSameNameMatches(t *testing.T) {
	t.Parallel()
	creds := RelayCredentials{URL: "wss://second-relay.acme.workers.dev", Token: "tok", ScriptName: "second-relay"}
	if !creds.MatchesScriptName("second-relay") {
		t.Fatal("MatchesScriptName(same name) = false, want true")
	}
}

func TestMatchesScriptNameDifferentNameDoesNotMatch(t *testing.T) {
	t.Parallel()
	// A stored ScriptName from a first `relay deploy` must never be treated
	// as belonging to a differently-named `--name second-relay` deploy: that
	// cross-name reuse is the P2 bug (a fresh Worker seeded with the first
	// relay's RELAY_TOKEN).
	creds := RelayCredentials{URL: "wss://moltnet-relay.acme.workers.dev", Token: "tok", ScriptName: "moltnet-relay"}
	if creds.MatchesScriptName("second-relay") {
		t.Fatal("MatchesScriptName(different name) = true, want false")
	}
}

func TestMatchesScriptNameLegacyCredentialsFallBackToHostnamePrefix(t *testing.T) {
	t.Parallel()
	// Credentials saved before ScriptName existed have it empty; the
	// fallback must still let a same-name redeploy reuse the token.
	legacy := RelayCredentials{URL: "wss://moltnet-relay.acme.workers.dev", Token: "tok"}
	if !legacy.MatchesScriptName("moltnet-relay") {
		t.Fatal("legacy MatchesScriptName(matching hostname prefix) = false, want true")
	}
	if legacy.MatchesScriptName("second-relay") {
		t.Fatal("legacy MatchesScriptName(non-matching hostname prefix) = true, want false")
	}
}

func TestMatchesScriptNameLegacyCredentialsFileStillLoads(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	path := CredentialsPath(filepath.Join(directory, "Moltnet"))
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatalf("MkdirAll() error = %v", err)
	}
	// Simulate a relay.json written before the ScriptName field existed: no
	// "scriptName" key at all.
	legacyJSON := `{"url":"wss://moltnet-relay.acme.workers.dev","token":"legacy-token"}`
	if err := os.WriteFile(path, []byte(legacyJSON), 0o600); err != nil {
		t.Fatalf("WriteFile() error = %v", err)
	}

	creds, ok, err := LoadCredentials(path)
	if err != nil {
		t.Fatalf("LoadCredentials() error = %v", err)
	}
	if !ok {
		t.Fatal("LoadCredentials() ok = false, want true")
	}
	if creds.ScriptName != "" {
		t.Fatalf("creds.ScriptName = %q, want empty for a legacy file", creds.ScriptName)
	}
	if creds.Token != "legacy-token" {
		t.Fatalf("creds.Token = %q, want legacy-token", creds.Token)
	}
	if !creds.MatchesScriptName("moltnet-relay") {
		t.Fatal("legacy creds MatchesScriptName(matching hostname) = false, want true")
	}
}

func TestSaveCredentialsCreatesParentDirectory(t *testing.T) {
	t.Parallel()
	directory := t.TempDir()
	// .moltnet/ does not exist yet under directory.
	path := CredentialsPath(filepath.Join(directory, "Moltnet"))

	if err := SaveCredentials(path, RelayCredentials{URL: "wss://relay.example.dev", Token: "token"}); err != nil {
		t.Fatalf("SaveCredentials() error = %v", err)
	}
	if _, err := os.Stat(filepath.Join(directory, ".moltnet")); err != nil {
		t.Fatalf("expected .moltnet directory to be created: %v", err)
	}
}
