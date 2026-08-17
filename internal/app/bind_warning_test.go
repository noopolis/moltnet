package app

import (
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
)

func TestIsLoopbackListenAddr(t *testing.T) {
	cases := []struct {
		name string
		addr string
		want bool
	}{
		{"loopback ipv4", "127.0.0.1:8787", true},
		{"loopback ipv4 no scheme", "127.0.0.2:8787", true},
		{"localhost", "localhost:8787", true},
		{"localhost mixed case", "LocalHost:8787", true},
		{"loopback ipv6", "[::1]:8787", true},
		{"wildcard colon-port", ":8787", false},
		{"explicit any ipv4", "0.0.0.0:8787", false},
		{"explicit any ipv6", "[::]:8787", false},
		{"lan address", "192.168.1.5:8787", false},
		{"public hostname", "moltnet.example.com:8787", false},
		{"empty", "", false},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if got := IsLoopbackListenAddr(testCase.addr); got != testCase.want {
				t.Fatalf("IsLoopbackListenAddr(%q) = %v, want %v", testCase.addr, got, testCase.want)
			}
		})
	}
}

// TestNonLoopbackAnonymousWriteWarningFiresForOpenRegistration covers
// PLAN.md phase 6a, item 2: widening listen_addr beyond loopback while
// agent_registration is open must warn, whether registration ended up open
// explicitly or via auth.mode: open.
func TestNonLoopbackAnonymousWriteWarningFiresForOpenRegistration(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{
			name: "explicit agent_registration open, bearer mode",
			cfg: Config{
				ListenAddr: "0.0.0.0:8787",
				Auth: authn.Config{
					Mode:              authn.ModeBearer,
					AgentRegistration: authn.AgentRegistrationOpen,
				},
			},
		},
		{
			name: "mode open (agent_registration resolved by mergeFileConfig)",
			cfg: Config{
				ListenAddr: "moltnet.example.com:8787",
				Auth: authn.Config{
					Mode:              authn.ModeOpen,
					AgentRegistration: authn.AgentRegistrationOpen,
				},
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			warning := NonLoopbackAnonymousWriteWarning(testCase.cfg)
			if warning == "" {
				t.Fatalf("expected a warning for %+v, got none", testCase.cfg)
			}
			if !strings.Contains(warning, testCase.cfg.ListenAddr) {
				t.Fatalf("expected warning to name the listen_addr %q, got %q", testCase.cfg.ListenAddr, warning)
			}
			if !strings.Contains(warning, "any host") && !strings.Contains(warning, "any reachable") && !strings.Contains(warning, "register") {
				t.Fatalf("expected warning to describe the registration exposure, got %q", warning)
			}
		})
	}
}

// TestNonLoopbackAnonymousWriteWarningFiresForModeNone covers PLAN.md phase
// 6a review, P2-3: the original check keyed only on agent_registration, so
// a non-loopback bind with auth.mode: none (the pre-phase-6a default —
// every write and admin route anonymous, with no registration step needed
// at all) stayed silent, while the strictly safer "bearer + open
// registration" shape correctly warned. mode: none on a non-loopback bind
// must warn on its own, regardless of agent_registration.
func TestNonLoopbackAnonymousWriteWarningFiresForModeNone(t *testing.T) {
	cfg := Config{
		ListenAddr: "0.0.0.0:8787",
		Auth: authn.Config{
			Mode:              authn.ModeNone,
			AgentRegistration: authn.AgentRegistrationDisabled,
		},
	}
	warning := NonLoopbackAnonymousWriteWarning(cfg)
	if warning == "" {
		t.Fatalf("expected a warning for %+v, got none", cfg)
	}
	if !strings.Contains(warning, cfg.ListenAddr) {
		t.Fatalf("expected warning to name the listen_addr %q, got %q", cfg.ListenAddr, warning)
	}
	if !strings.Contains(warning, "auth.mode") || !strings.Contains(warning, "none") {
		t.Fatalf("expected warning to name auth.mode \"none\" as the exposure, got %q", warning)
	}
}

// TestNonLoopbackAnonymousWriteWarningSilent covers the negative space:
// loopback binds never warn regardless of auth posture, and non-loopback
// binds never warn when auth.mode is not none and registration is not open
// — including the exact "bearer + agent_registration: disabled" shape from
// the phase 6a field report, which must stay silent (that combination is
// refused differently, by requiring a pre-existing admin token, not by this
// exposure warning).
func TestNonLoopbackAnonymousWriteWarningSilent(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{
			name: "loopback bind, open registration",
			cfg: Config{
				ListenAddr: "127.0.0.1:8787",
				Auth:       authn.Config{Mode: authn.ModeOpen, AgentRegistration: authn.AgentRegistrationOpen},
			},
		},
		{
			name: "localhost bind, open registration",
			cfg: Config{
				ListenAddr: "localhost:8787",
				Auth:       authn.Config{Mode: authn.ModeOpen, AgentRegistration: authn.AgentRegistrationOpen},
			},
		},
		{
			name: "loopback bind, mode none",
			cfg: Config{
				ListenAddr: "127.0.0.1:8787",
				Auth:       authn.Config{Mode: authn.ModeNone, AgentRegistration: authn.AgentRegistrationDisabled},
			},
		},
		{
			name: "non-loopback bind, registration disabled (the field-report shape)",
			cfg: Config{
				ListenAddr: "0.0.0.0:8787",
				Auth:       authn.Config{Mode: authn.ModeBearer, AgentRegistration: authn.AgentRegistrationDisabled},
			},
		},
		{
			name: "non-loopback bind, registration token",
			cfg: Config{
				ListenAddr: "0.0.0.0:8787",
				Auth:       authn.Config{Mode: authn.ModeBearer, AgentRegistration: authn.AgentRegistrationToken},
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			if warning := NonLoopbackAnonymousWriteWarning(testCase.cfg); warning != "" {
				t.Fatalf("expected no warning for %+v, got %q", testCase.cfg, warning)
			}
		})
	}
}
