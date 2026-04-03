package protocol

import "testing"

func TestCanonicalIDs(t *testing.T) {
	t.Parallel()

	if got := RoomFQID("local_lab", "research"); got != "molt://local_lab/rooms/research" {
		t.Fatalf("unexpected room fqid %q", got)
	}

	if got := DMFQID("local_lab", "dm-1"); got != "molt://local_lab/dms/dm-1" {
		t.Fatalf("unexpected dm fqid %q", got)
	}

	if got := AgentFQID("local_lab", "writer"); got != "molt://local_lab/agents/writer" {
		t.Fatalf("unexpected agent fqid %q", got)
	}

	if got := ThreadFQID("local_lab", "thread-1"); got != "molt://local_lab/threads/thread-1" {
		t.Fatalf("unexpected thread fqid %q", got)
	}

	if got := ArtifactFQID("local_lab", "art-1"); got != "molt://local_lab/artifacts/art-1" {
		t.Fatalf("unexpected artifact fqid %q", got)
	}
}

func TestScopedAgentHelpers(t *testing.T) {
	t.Parallel()

	if got := ScopedAgentID("net_a", "alpha"); got != "net_a:alpha" {
		t.Fatalf("unexpected scoped agent id %q", got)
	}

	networkID, agentID, ok := ParseScopedAgentID("net_a:alpha")
	if !ok || networkID != "net_a" || agentID != "alpha" {
		t.Fatalf("unexpected scoped parse %q %q %v", networkID, agentID, ok)
	}

	networkID, agentID, ok = ParseAgentFQID("molt://net_a/agents/alpha")
	if !ok || networkID != "net_a" || agentID != "alpha" {
		t.Fatalf("unexpected fqid parse %q %q %v", networkID, agentID, ok)
	}

	actor := NormalizeActor("net_a", Actor{ID: "alpha"})
	if actor.NetworkID != "net_a" || actor.FQID != "molt://net_a/agents/alpha" {
		t.Fatalf("unexpected normalized actor %#v", actor)
	}

	if !ActorMatches("net_a", "alpha", "alpha") {
		t.Fatal("expected plain actor match")
	}
	if !ActorMatches("net_a", "alpha", "net_a:alpha") {
		t.Fatal("expected scoped actor match")
	}
	if !ActorMatches("net_a", "alpha", "molt://net_a/agents/alpha") {
		t.Fatal("expected fqid actor match")
	}
	if ActorMatches("net_a", "alpha", "net_b:alpha") {
		t.Fatal("expected other network actor mismatch")
	}

	if got := RemoteParticipantID("net_b", Actor{ID: "alpha", NetworkID: "net_a"}); got != "net_a:alpha" {
		t.Fatalf("unexpected remote participant id %q", got)
	}
	if got := RemoteParticipantID("net_a", Actor{ID: "alpha", NetworkID: "net_a"}); got != "alpha" {
		t.Fatalf("unexpected local participant id %q", got)
	}
}

func TestScopedAgentHelpersEdgeCases(t *testing.T) {
	t.Parallel()

	if got := ScopedAgentID("  net_a  ", "  alpha  "); got != "net_a:alpha" {
		t.Fatalf("unexpected trimmed scoped agent id %q", got)
	}
	if got := ScopedAgentID("   ", " alpha "); got != "alpha" {
		t.Fatalf("expected trimmed agent fallback, got %q", got)
	}

	for _, value := range []string{"", "   ", "alpha", ":alpha", "net_a:", "molt://net_a/agents/alpha"} {
		value := value
		t.Run("scoped:"+value, func(t *testing.T) {
			t.Parallel()

			networkID, agentID, ok := ParseScopedAgentID(value)
			if ok || networkID != "" || agentID != "" {
				t.Fatalf("expected invalid scoped parse for %q, got %q %q %v", value, networkID, agentID, ok)
			}
		})
	}

	for _, value := range []string{"", "net_a:alpha", "molt://", "molt:///agents/alpha", "molt://net_a/agents/"} {
		value := value
		t.Run("fqid:"+value, func(t *testing.T) {
			t.Parallel()

			networkID, agentID, ok := ParseAgentFQID(value)
			if ok || networkID != "" || agentID != "" {
				t.Fatalf("expected invalid fqid parse for %q, got %q %q %v", value, networkID, agentID, ok)
			}
		})
	}

	actor := NormalizeActor("net_a", Actor{
		ID:        "alpha",
		NetworkID: "net_b",
		FQID:      "molt://net_b/agents/alpha",
	})
	if actor.NetworkID != "net_b" || actor.FQID != "molt://net_b/agents/alpha" {
		t.Fatalf("unexpected normalized actor %#v", actor)
	}

	if ActorMatches("net_a", "alpha", "beta") {
		t.Fatal("expected different actor id to not match")
	}
	if got := RemoteParticipantID("net_a", Actor{ID: "alpha"}); got != "alpha" {
		t.Fatalf("unexpected participant id without network %q", got)
	}
}
