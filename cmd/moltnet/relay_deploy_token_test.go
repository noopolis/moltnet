package main

import (
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/relaydeploy"
)

// These tests cover the per-network stored Cloudflare API token's pure
// resolution/save-decision logic, tested directly against
// resolveCloudflareAPIToken, maybeSaveCloudflareToken, and
// promptYesNoDefaultYes — no real deploy involved. Full end-to-end deploys
// (through a fakeCloudflareCLIServer, relay_deploy_cloudflare_test.go) and
// --forget-token live in relay_deploy_token_e2e_test.go, split out to keep
// both files under the repo's 400-line-per-file rule.

func TestResolveCloudflareAPITokenEnvBeatsStored(t *testing.T) {
	token, source := resolveCloudflareAPIToken("env-token", "stored-token", true)
	if token != "env-token" || source != cloudflareTokenSourceEnv {
		t.Fatalf("resolveCloudflareAPIToken() = (%q, %q), want (env-token, %q)", token, source, cloudflareTokenSourceEnv)
	}
}

func TestResolveCloudflareAPITokenStoredBeatsMissing(t *testing.T) {
	token, source := resolveCloudflareAPIToken("", "stored-token", true)
	if token != "stored-token" || source != cloudflareTokenSourceStored {
		t.Fatalf("resolveCloudflareAPIToken() = (%q, %q), want (stored-token, %q)", token, source, cloudflareTokenSourceStored)
	}
}

func TestResolveCloudflareAPITokenNeitherIsEmpty(t *testing.T) {
	token, source := resolveCloudflareAPIToken("", "", false)
	if token != "" || source != "" {
		t.Fatalf("resolveCloudflareAPIToken() = (%q, %q), want (\"\", \"\")", token, source)
	}
}

func TestResolveCloudflareAPITokenBlankEnvFallsBackToStored(t *testing.T) {
	// os.Getenv on an unset var, and a var explicitly set to whitespace,
	// both look like "" after TrimSpace; either way stored must still win.
	token, source := resolveCloudflareAPIToken("   ", "stored-token", true)
	if token != "stored-token" || source != cloudflareTokenSourceStored {
		t.Fatalf("resolveCloudflareAPIToken() = (%q, %q), want (stored-token, %q)", token, source, cloudflareTokenSourceStored)
	}
}

func TestMaybeSaveCloudflareTokenSavesWithFlag(t *testing.T) {
	directory := t.TempDir()
	tokenPath := relaydeploy.CloudflareTokenPath(directory + "/Moltnet")

	output := captureStdout(t, func() {
		if err := maybeSaveCloudflareToken(&sectionPrinter{}, tokenPath, "cf-token", true, false); err != nil {
			t.Fatalf("maybeSaveCloudflareToken() error = %v", err)
		}
	})
	if got := cloudflareTokenFileContents(t, tokenPath); got != "cf-token" {
		t.Fatalf("saved token = %q, want cf-token", got)
	}
	if !strings.Contains(output, tokenPath) {
		t.Fatalf("expected save confirmation naming %q, got %q", tokenPath, output)
	}
	if strings.Contains(output, "cf-token") {
		t.Fatalf("expected the token value itself never to be printed, got %q", output)
	}
}

func TestMaybeSaveCloudflareTokenPromptYesSaves(t *testing.T) {
	directory := t.TempDir()
	tokenPath := relaydeploy.CloudflareTokenPath(directory + "/Moltnet")

	// P2 env-path save-offer gating fix: the offer now requires both stdin
	// AND stdout to be a real terminal, not stdin alone.
	withInteractiveOutput(t)
	withPromptAnswers(t, "y")
	output := captureStdout(t, func() {
		if err := maybeSaveCloudflareToken(&sectionPrinter{}, tokenPath, "cf-token", false, false); err != nil {
			t.Fatalf("maybeSaveCloudflareToken() error = %v", err)
		}
	})
	if got := cloudflareTokenFileContents(t, tokenPath); got != "cf-token" {
		t.Fatalf("saved token = %q, want cf-token", got)
	}
	if !strings.Contains(output, "save this token to "+tokenPath) {
		t.Fatalf("expected the save-offer prompt naming %q, got %q", tokenPath, output)
	}
}

func TestMaybeSaveCloudflareTokenPromptNoSkipsSave(t *testing.T) {
	directory := t.TempDir()
	tokenPath := relaydeploy.CloudflareTokenPath(directory + "/Moltnet")

	withInteractiveOutput(t)
	withPromptAnswers(t, "n")
	captureStdout(t, func() {
		if err := maybeSaveCloudflareToken(&sectionPrinter{}, tokenPath, "cf-token", false, false); err != nil {
			t.Fatalf("maybeSaveCloudflareToken() error = %v", err)
		}
	})
	if _, ok, err := relaydeploy.LoadCloudflareToken(tokenPath); err != nil || ok {
		t.Fatalf("LoadCloudflareToken() after declining = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
}

func TestMaybeSaveCloudflareTokenNonInteractiveNeverPrompts(t *testing.T) {
	directory := t.TempDir()
	tokenPath := relaydeploy.CloudflareTokenPath(directory + "/Moltnet")

	previousInteractive := isInteractive
	isInteractive = func() bool { return false }
	t.Cleanup(func() { isInteractive = previousInteractive })

	output := captureStdout(t, func() {
		if err := maybeSaveCloudflareToken(&sectionPrinter{}, tokenPath, "cf-token", false, false); err != nil {
			t.Fatalf("maybeSaveCloudflareToken() error = %v", err)
		}
	})
	if strings.Contains(output, "save this token") {
		t.Fatalf("expected no save-offer prompt when non-interactive, got %q", output)
	}
	if _, ok, err := relaydeploy.LoadCloudflareToken(tokenPath); err != nil || ok {
		t.Fatalf("LoadCloudflareToken() = (ok=%v, err=%v), want (false, nil): non-interactive must never save without --save-token", ok, err)
	}
}

func TestMaybeSaveCloudflareTokenSkipsOfferWhenAlreadyStored(t *testing.T) {
	directory := t.TempDir()
	tokenPath := relaydeploy.CloudflareTokenPath(directory + "/Moltnet")

	previousInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = previousInteractive })

	// storedTokenOK=true means something is already stored; the offer-once
	// prompt must not re-ask (and promptReader is left untouched, so a
	// prompt attempt here would hang reading EOF from os.Stdin — proof it
	// was skipped, not just declined).
	output := captureStdout(t, func() {
		if err := maybeSaveCloudflareToken(&sectionPrinter{}, tokenPath, "cf-token", false, true); err != nil {
			t.Fatalf("maybeSaveCloudflareToken() error = %v", err)
		}
	})
	if strings.Contains(output, "save this token") {
		t.Fatalf("expected no save-offer prompt when a token is already stored, got %q", output)
	}
}

// TestPromptYesNoDefaultYesBareEnterSaves pins the P2-3 fix directly: a
// bare Enter (a real newline, no other input) at promptYesNoDefaultYes must
// count as yes. Before this test existed, a default-NO mutation of this
// function's answer logic would have passed every other test in this
// package — this is the one that actually catches it.
func TestPromptYesNoDefaultYesBareEnterSaves(t *testing.T) {
	withPromptAnswers(t, "")

	var confirmed bool
	var err error
	captureStdout(t, func() {
		confirmed, err = promptYesNoDefaultYes("save? [Y/n] ")
	})
	if err != nil {
		t.Fatalf("promptYesNoDefaultYes() error = %v", err)
	}
	if !confirmed {
		t.Fatal("promptYesNoDefaultYes() = false, want true for a bare Enter (default yes)")
	}
}

// TestPromptYesNoDefaultYesEOFDoesNotSave pins the P2-4 fix: unlike a bare
// Enter (an actual newline with nothing before it), io.EOF with no newline
// at all (e.g. Ctrl-D, or a closed pipe) must count as no, not as the
// bare-Enter default-yes case.
func TestPromptYesNoDefaultYesEOFDoesNotSave(t *testing.T) {
	previousReader := promptReader
	promptReader = strings.NewReader("") // ReadString('\n') returns "", io.EOF immediately.
	t.Cleanup(func() { promptReader = previousReader })

	previousInteractive := isInteractive
	isInteractive = func() bool { return true }
	t.Cleanup(func() { isInteractive = previousInteractive })

	var confirmed bool
	var err error
	captureStdout(t, func() {
		confirmed, err = promptYesNoDefaultYes("save? [Y/n] ")
	})
	if err != nil {
		t.Fatalf("promptYesNoDefaultYes() error = %v", err)
	}
	if confirmed {
		t.Fatal("promptYesNoDefaultYes() = true, want false on io.EOF")
	}
}

// TestPromptYesNoDefaultYesWhitelistParsing pins the P0 consent-parser fix:
// promptYesNoDefaultYes must save on ONLY "", "y", or "yes" (case
// insensitive, surrounding whitespace trimmed) and treat everything else —
// including a typo like "nope" that starts with "n", plain garbage, and an
// escape sequence — as no. The previous `answer != "n" && answer != "no"`
// shape was a blocklist, not a whitelist: every one of the "no" cases below
// except the exact strings "n"/"no" would have wrongly saved under it.
func TestPromptYesNoDefaultYesWhitelistParsing(t *testing.T) {
	cases := []struct {
		name   string
		answer string
		want   bool
	}{
		{"bare enter", "", true},
		{"lowercase y", "y", true},
		{"uppercase Y", "Y", true},
		{"lowercase yes", "yes", true},
		{"uppercase YES", "YES", true},
		{"padded yes", "  yes  ", true},
		{"explicit n", "n", false},
		{"explicit no", "no", false},
		{"typo starting with n", "nope", false},
		{"garbage", "asdf", false},
		{"escape sequence", "\x1b[A", false},
		{"yes with trailing garbage", "yes please", false},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			withPromptAnswers(t, tc.answer)

			var confirmed bool
			var err error
			captureStdout(t, func() {
				confirmed, err = promptYesNoDefaultYes("save? [Y/n] ")
			})
			if err != nil {
				t.Fatalf("promptYesNoDefaultYes(%q) error = %v", tc.answer, err)
			}
			if confirmed != tc.want {
				t.Fatalf("promptYesNoDefaultYes(%q) = %v, want %v", tc.answer, confirmed, tc.want)
			}
		})
	}
}

// TestMaybeSavePastedCloudflareTokenDeclinePrintsNotSaved covers the P0
// consent-parser fix's other half: declining the post-deploy pasted-token
// save prompt (any non-affirmative answer) must print a "not saved" line,
// not just silently do nothing.
func TestMaybeSavePastedCloudflareTokenDeclinePrintsNotSaved(t *testing.T) {
	directory := t.TempDir()
	tokenPath := relaydeploy.CloudflareTokenPath(directory + "/Moltnet")

	withPromptAnswers(t, "nope")
	output := captureStdout(t, func() {
		if err := maybeSavePastedCloudflareToken(&sectionPrinter{}, tokenPath, "cf-token", false); err != nil {
			t.Fatalf("maybeSavePastedCloudflareToken() error = %v", err)
		}
	})
	if !strings.Contains(output, "not saved") {
		t.Fatalf("expected a \"not saved\" line after declining, got %q", output)
	}
	if _, ok, err := relaydeploy.LoadCloudflareToken(tokenPath); err != nil || ok {
		t.Fatalf("LoadCloudflareToken() after declining = (ok=%v, err=%v), want (false, nil)", ok, err)
	}
}
