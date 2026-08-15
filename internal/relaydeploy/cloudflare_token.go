package relaydeploy

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
)

// cloudflareTokenCredentials is the on-disk shape of the per-network stored
// Cloudflare API token: `moltnet relay deploy --save-token` (or the
// interactive save prompt) writes it beside the existing relay credentials
// so future deploys for this network do not require re-exporting
// CLOUDFLARE_API_TOKEN.
type cloudflareTokenCredentials struct {
	APIToken string `json:"apiToken"`
}

// CloudflareTokenPath returns the .moltnet/cloudflare.json path to use for
// the Moltnet config at configPath, sitting next to CredentialsPath's
// relay.json in the same .moltnet/ runtime-state directory.
func CloudflareTokenPath(configPath string) string {
	return filepath.Join(filepath.Dir(configPath), ".moltnet", "cloudflare.json")
}

// LoadCloudflareToken reads a stored Cloudflare API token. ok is false, with
// a nil error, when nothing has been saved yet.
//
// If the file is readable by group or other (SaveCloudflareToken always
// writes 0600, so this means something outside Moltnet loosened it), a
// warning naming the file is printed to stderr. It is only a warning: the
// permissions are never silently chmod'd back, since that could surprise
// whatever changed them on purpose.
func LoadCloudflareToken(path string) (token string, ok bool, err error) {
	contents, err := os.ReadFile(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("read stored Cloudflare API token %q: %w", path, err)
	}
	warnIfCloudflareTokenFileTooOpen(path)

	var creds cloudflareTokenCredentials
	if err := json.Unmarshal(contents, &creds); err != nil {
		return "", false, fmt.Errorf("decode stored Cloudflare API token %q: %w", path, err)
	}
	token = strings.TrimSpace(creds.APIToken)
	if token == "" {
		return "", false, nil
	}
	return token, true, nil
}

// SaveCloudflareToken writes token to path atomically (temp file + rename),
// mode 0600, creating the parent .moltnet/ directory if needed. It never
// logs or echoes token itself.
func SaveCloudflareToken(path, token string) error {
	contents, err := json.MarshalIndent(cloudflareTokenCredentials{APIToken: token}, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Cloudflare API token: %w", err)
	}
	contents = append(contents, '\n')

	if err := writeSecretFileAtomic(path, contents); err != nil {
		return fmt.Errorf("save Cloudflare API token %q: %w", path, err)
	}
	return nil
}

// warnIfCloudflareTokenFileTooOpen prints a stderr warning naming path when
// its permission bits grant group or other any access. Windows permission
// bits don't map onto this POSIX group/other model, so the check is skipped
// there. The permissions are only ever reported, never corrected: silently
// chmod'ing a file back to 0600 could clobber a deliberate change (e.g. a
// shared multi-user checkout) the caller made on purpose.
func warnIfCloudflareTokenFileTooOpen(path string) {
	if runtime.GOOS == "windows" {
		return
	}
	info, err := os.Stat(path)
	if err != nil {
		return
	}
	if perm := info.Mode().Perm(); perm&0o077 != 0 {
		fmt.Fprintf(os.Stderr, "warning: stored Cloudflare API token %s is readable by group/other (mode %o); consider chmod 600 %s\n", path, perm, path)
	}
}

// DeleteCloudflareToken removes a stored Cloudflare API token file, for
// `relay deploy --forget-token`. removed is false, with a nil error, when
// there was nothing stored at path.
func DeleteCloudflareToken(path string) (removed bool, err error) {
	if err := os.Remove(path); err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return false, nil
		}
		return false, fmt.Errorf("remove stored Cloudflare API token %q: %w", path, err)
	}
	return true, nil
}
