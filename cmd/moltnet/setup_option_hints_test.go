package main

import (
	"context"
	"path/filepath"
	"strings"
	"testing"
)

// Every option this wizard offers must say what it does. Only Q1's two
// options ever carried a hint, which is exactly why Q1 read clearly and the
// rest did not: "open it up" and "add more" told a reader nothing about what
// choosing them would actually cause. This pins a hint onto every option of
// every select question, so a bare option cannot ship again.
//
// It asserts on the *rendered* prompt output rather than the option structs,
// because selectOptionText is what a person actually reads — a hint set on a
// struct but dropped by the renderer would still be a bare option on screen.
func TestSetupSelectOptionsAllExplainThemselves(t *testing.T) {
	tests := []struct {
		name    string
		answers []string
		ask     func()
		// wantHints are substrings that must appear in the rendered block,
		// one per option offered by that question.
		wantHints []string
	}{
		{
			name:    "where the network lives",
			answers: []string{"1"},
			ask:     func() { _, _ = askSetupScope(context.Background(), nil) },
			wantHints: []string{
				"always available", "lives with this project",
			},
		},
		{
			name:    "reachable from",
			answers: []string{"1"},
			ask:     func() { _, _ = askSetupWideBind(context.Background(), false) },
			wantHints: []string{
				"only agents running on this computer", "any device on your LAN",
			},
		},
		{
			name:    "rooms",
			answers: []string{"1"},
			ask:     func() { _, _ = askSetupRooms(context.Background()) },
			wantHints: []string{
				"one room, ready to talk in", "call that first room something else", "several rooms, named now",
			},
		},
		{
			name:    "run as a service",
			answers: []string{"1"},
			ask:     func() { _, _ = askSetupService(context.Background()) },
			wantHints: []string{
				"starts on login", "you run `moltnet start` when you want it",
			},
		},
		{
			name:    "connect to another network",
			answers: []string{"1"},
			ask:     func() { _, _ = askSetupConnect(context.Background()) },
			wantHints: []string{
				"just my own agents", "deploy a relay, get a code to send them", "a friend already sent you an invite",
			},
		},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			withPromptAnswers(t, test.answers...)
			output := captureStdout(t, test.ask)
			for _, hint := range test.wantHints {
				if !strings.Contains(output, hint) {
					t.Errorf("option hint %q missing from the rendered question:\n%s", hint, output)
				}
			}
		})
	}
}

// The wizard must send an operator to Cloudflare's token page with the one
// required permission group already selected, exactly as `relay deploy` does.
// It previously printed a bare dashboard URL plus a hand-named token template,
// which dropped the operator on an empty form to configure themselves.
func TestSetupCloudflareTokenPromptUsesThePreFilledDeepLink(t *testing.T) {
	t.Setenv("CLOUDFLARE_API_TOKEN", "")
	configPath := filepath.Join(t.TempDir(), "Moltnet")

	var err error
	output := captureStdout(t, func() {
		_, err = resolveSetupCloudflareToken(configPath)
	})

	if err == nil {
		t.Fatal("expected a refusal when no token can be resolved")
	}
	if !strings.Contains(err.Error(), "invite a friend") {
		t.Errorf("refusal should name the option the operator picked, got: %v", err)
	}

	deepLink := buildCloudflareTokenDeepLink(cloudflareTokenTemplateName)
	if !strings.Contains(output, deepLink) {
		t.Errorf("prompt must print the pre-filled deep link.\nwant: %s\ngot:\n%s", deepLink, output)
	}
	if !strings.Contains(output, "permissionGroupKeys") {
		t.Error("deep link lost its pre-selected permission group")
	}
}
