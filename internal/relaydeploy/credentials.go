package relaydeploy

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strings"
)

// RelayCredentials is the local record `moltnet relay deploy` writes so
// `moltnet pair invite` can reuse an already-deployed relay without
// re-flagging --relay-url/--relay-token-env every time (PLAN.md "Relay
// credentials storage").
type RelayCredentials struct {
	URL   string `json:"url"`
	Token string `json:"token"`
	// ScriptName is the Cloudflare Worker script name (deploy's --name) these
	// credentials belong to. Empty for credentials files written before this
	// field existed; MatchesScriptName falls back to a hostname-prefix match
	// for those.
	ScriptName string `json:"scriptName,omitempty"`
}

// MatchesScriptName reports whether creds were saved for a deploy of
// scriptName, so their Token is safe to reuse as a redeploy's PriorToken.
// Reusing a token across different script names would seed a brand-new
// Worker with a different relay's shared secret instead of generating its
// own (the P2 cross-name RELAY_TOKEN reuse bug).
//
// Credentials saved before ScriptName existed fall back to matching the
// stored URL's hostname prefix against scriptName, since Deploy always
// derives the hostname as "<scriptName>.<subdomain>.workers.dev".
func (creds RelayCredentials) MatchesScriptName(scriptName string) bool {
	if creds.ScriptName != "" {
		return creds.ScriptName == scriptName
	}
	if scriptName == "" {
		return false
	}
	parsed, err := url.Parse(creds.URL)
	if err != nil {
		return false
	}
	return strings.HasPrefix(parsed.Host, scriptName+".")
}

// CredentialsPath returns the .moltnet/relay.json path to use for the
// Moltnet config at configPath, mirroring the .moltnet/ runtime-state
// convention used elsewhere (e.g. register-agent's identity.json).
func CredentialsPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), ".moltnet", "relay.json")
}

// LoadCredentials reads stored relay credentials. ok is false, with a nil
// error, when nothing has been saved yet.
func LoadCredentials(path string) (creds RelayCredentials, ok bool, err error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return RelayCredentials{}, false, nil
		}
		return RelayCredentials{}, false, fmt.Errorf("read relay credentials %q: %w", path, err)
	}

	if err := json.Unmarshal(contents, &creds); err != nil {
		return RelayCredentials{}, false, fmt.Errorf("decode relay credentials %q: %w", path, err)
	}
	return creds, true, nil
}

// SaveCredentials writes creds to path atomically (temp file + rename),
// mode 0600, creating the parent .moltnet/ directory if needed.
func SaveCredentials(path string, creds RelayCredentials) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o700); err != nil {
		return fmt.Errorf("create %q: %w", dir, err)
	}

	contents, err := json.MarshalIndent(creds, "", "  ")
	if err != nil {
		return fmt.Errorf("encode relay credentials: %w", err)
	}
	contents = append(contents, '\n')

	temp, err := os.CreateTemp(dir, ".relay-credentials-*")
	if err != nil {
		return fmt.Errorf("create temporary relay credentials file: %w", err)
	}
	tempPath := temp.Name()
	defer func() { _ = os.Remove(tempPath) }()

	if err := temp.Chmod(0o600); err != nil {
		_ = temp.Close()
		return fmt.Errorf("chmod temporary relay credentials file: %w", err)
	}
	if _, err := temp.Write(contents); err != nil {
		_ = temp.Close()
		return fmt.Errorf("write temporary relay credentials file: %w", err)
	}
	if err := temp.Close(); err != nil {
		return fmt.Errorf("close temporary relay credentials file: %w", err)
	}
	if err := os.Rename(tempPath, path); err != nil {
		return fmt.Errorf("replace relay credentials file: %w", err)
	}
	if err := os.Chmod(path, 0o600); err != nil {
		return fmt.Errorf("chmod relay credentials file: %w", err)
	}

	return nil
}
