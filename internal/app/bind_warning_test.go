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

// TestValidateListenAddr covers the syntax-validation gap PLAN.md's
// setup-wizard spec calls out explicitly: "config_file.go validates none
// today." Malformed values must be rejected with a clear message, while
// every real listen_addr shape used elsewhere in this codebase (wildcard,
// loopback, LAN, hostname, ":0" for a test's ephemeral port) stays valid.
func TestValidateListenAddr(t *testing.T) {
	valid := []string{
		"127.0.0.1:8787",
		"0.0.0.0:8787",
		":8787",
		":0",
		"[::1]:8787",
		"[::]:8787",
		"192.168.1.5:8787",
		"moltnet.example.com:8787",
		"localhost:8787",
		// Deliberate decision, not an oversight: ValidateListenAddr never
		// validates the host's own character content (see its doc comment)
		// -- an underscore-containing Docker/Compose service name like this
		// one is common enough in practice that guessing at a stricter
		// hostname grammar here risks rejecting a real, already-working
		// listen_addr this validator has never seen before.
		"moltnet_server:8787",
	}
	for _, addr := range valid {
		if err := ValidateListenAddr(addr); err != nil {
			t.Errorf("ValidateListenAddr(%q) error = %v, want nil", addr, err)
		}
	}

	invalid := []string{
		"",
		"127.0.0.1",
		"bad::addr",
		"127.0.0.1:",
		"127.0.0.1:abc",
		"127.0.0.1:99999",
		":8787:extra",
	}
	for _, addr := range invalid {
		if err := ValidateListenAddr(addr); err == nil {
			t.Errorf("ValidateListenAddr(%q) error = nil, want an error", addr)
		}
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

// TestNonLoopbackAnonymousWriteWarningNamesAnonymousReadOnPlainInitDefault is
// round 2's P2-4 fix: plain `init`'s own default shape -- auth.mode: open
// (which forces auth.public_read: true, mergeFileConfig) plus its "general"
// starter room at visibility: public (defaultMoltnetConfig,
// cmd/moltnet/templates.go) -- widened to a non-loopback bind used to warn
// only about write-scoped agent self-registration, saying nothing about the
// same bind serving that room's full history and live SSE stream to any
// anonymous visitor with no token at all. The warning must now name both.
func TestNonLoopbackAnonymousWriteWarningNamesAnonymousReadOnPlainInitDefault(t *testing.T) {
	cfg := Config{
		ListenAddr: "0.0.0.0:8787",
		Auth: authn.Config{
			Mode:              authn.ModeOpen,
			AgentRegistration: authn.AgentRegistrationOpen,
			PublicRead:        true,
		},
		Rooms: []RoomConfig{
			{ID: "general", Visibility: "public"},
		},
	}
	warning := NonLoopbackAnonymousWriteWarning(cfg)
	if warning == "" {
		t.Fatalf("expected a warning for %+v, got none", cfg)
	}
	if !strings.Contains(warning, "general") {
		t.Fatalf("expected warning to name the publicly readable room, got %q", warning)
	}
	if !strings.Contains(warning, "REST") || !strings.Contains(warning, "SSE") {
		t.Fatalf("expected warning to mention both the REST history and the SSE stream, got %q", warning)
	}
	if !strings.Contains(warning, "public_read") {
		t.Fatalf("expected warning to name auth.public_read as the reason, got %q", warning)
	}
	// The write-scoped registration clause must still be present -- this is
	// an addition, not a replacement.
	if !strings.Contains(warning, "register") {
		t.Fatalf("expected the registration clause to survive alongside the read clause, got %q", warning)
	}
}

// TestNonLoopbackAnonymousWriteWarningOmitsReadClauseWithoutPublicRoom
// covers the negative half: open registration with public_read true but no
// room actually public (or public_read false) must not claim a public room
// is anonymously readable when none exists.
func TestNonLoopbackAnonymousWriteWarningOmitsReadClauseWithoutPublicRoom(t *testing.T) {
	cases := []struct {
		name string
		cfg  Config
	}{
		{
			name: "public_read true, no rooms configured",
			cfg: Config{
				ListenAddr: "0.0.0.0:8787",
				Auth: authn.Config{
					Mode:              authn.ModeOpen,
					AgentRegistration: authn.AgentRegistrationOpen,
					PublicRead:        true,
				},
			},
		},
		{
			name: "public_read true, only a private room",
			cfg: Config{
				ListenAddr: "0.0.0.0:8787",
				Auth: authn.Config{
					Mode:              authn.ModeOpen,
					AgentRegistration: authn.AgentRegistrationOpen,
					PublicRead:        true,
				},
				Rooms: []RoomConfig{{ID: "private-room", Visibility: "private"}},
			},
		},
		{
			name: "public room but public_read false",
			cfg: Config{
				ListenAddr: "0.0.0.0:8787",
				Auth: authn.Config{
					Mode:              authn.ModeBearer,
					AgentRegistration: authn.AgentRegistrationOpen,
					PublicRead:        false,
				},
				Rooms: []RoomConfig{{ID: "general", Visibility: "public"}},
			},
		},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			warning := NonLoopbackAnonymousWriteWarning(testCase.cfg)
			if warning == "" {
				t.Fatalf("expected the registration warning itself to still fire for %+v", testCase.cfg)
			}
			if strings.Contains(warning, "SSE") || strings.Contains(warning, "public_read") {
				t.Fatalf("expected no anonymous-read clause when no room is anonymously readable, got %q", warning)
			}
		})
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
