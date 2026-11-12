package app

import (
	authn "github.com/noopolis/moltnet/internal/auth"
	"testing"
)

func TestConsoleLocalOwnerGuards(t *testing.T) {
	owner := authn.TokenConfig{ID: "operator", Value: "owner-test", Scopes: []authn.Scope{authn.ScopeObserve, authn.ScopeWrite, authn.ScopeAdmin}}
	for _, tc := range []struct {
		name, listen                         string
		proxy, disabled, restricted, noWrite bool
		want                                 bool
	}{
		{name: "loopback", listen: "127.0.0.1:8787", want: true},
		{name: "ipv6", listen: "[::1]:8787", want: true},
		{name: "public", listen: "0.0.0.0:8787"},
		{name: "proxy", listen: "127.0.0.1:8787", proxy: true},
		{name: "restricted", listen: "127.0.0.1:8787", restricted: true},
		{name: "no write", listen: "127.0.0.1:8787", noWrite: true},
		{name: "disabled", listen: "0.0.0.0:8787", disabled: true, want: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			token := owner
			if tc.restricted {
				token.Agents = []string{"grok-bot"}
			}
			if tc.noWrite {
				token.Scopes = []authn.Scope{authn.ScopeObserve, authn.ScopeAdmin}
			}
			cfg := Config{ListenAddr: tc.listen, Console: ConsoleConfig{LocalOwnerLogin: !tc.disabled}, Auth: authn.Config{TrustForwardedProto: tc.proxy, Tokens: []authn.TokenConfig{token}}}
			secret, err := consoleLocalOwnerToken(cfg)
			if (err == nil) != tc.want {
				t.Fatalf("unexpected error: %v", err)
			}
			if tc.disabled && secret.Reveal() != "" {
				t.Fatal("disabled login retained secret")
			}
		})
	}
}
