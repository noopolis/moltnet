package app

import "testing"

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("MOLTNET_ALLOW_HUMAN_INGRESS", "false")
	t.Setenv("MOLTNET_LISTEN_ADDR", ":9999")
	t.Setenv("MOLTNET_NETWORK_ID", "lab")
	t.Setenv("MOLTNET_NETWORK_NAME", "Lab")
	t.Setenv("MOLTNET_PAIRINGS_JSON", `[{"id":"pair_1","remote_network_id":"remote","remote_network_name":"Remote Lab","status":"connected"}]`)

	config := ConfigFromEnv("1.2.3")
	if config.AllowHumanIngress {
		t.Fatalf("expected human ingress disabled, got %#v", config)
	}
	if config.ListenAddr != ":9999" || config.NetworkID != "lab" || config.NetworkName != "Lab" || config.Version != "1.2.3" {
		t.Fatalf("unexpected config %#v", config)
	}
	if len(config.Pairings) != 1 || config.Pairings[0].RemoteNetworkID != "remote" {
		t.Fatalf("unexpected pairings %#v", config.Pairings)
	}
}

func TestEnvOrDefault(t *testing.T) {
	t.Parallel()

	if got := envOrDefault("MOLTNET_UNKNOWN", "fallback"); got != "fallback" {
		t.Fatalf("unexpected fallback %q", got)
	}
}

func TestEnvBoolOrDefault(t *testing.T) {
	t.Setenv("MOLTNET_FLAG", "yes")
	if got := envBoolOrDefault("MOLTNET_FLAG", false); !got {
		t.Fatal("expected truthy value")
	}

	t.Setenv("MOLTNET_FLAG", "off")
	if got := envBoolOrDefault("MOLTNET_FLAG", true); got {
		t.Fatal("expected falsy value")
	}

	t.Setenv("MOLTNET_FLAG", "unknown")
	if got := envBoolOrDefault("MOLTNET_FLAG", true); !got {
		t.Fatal("expected fallback value")
	}
}

func TestEnvPairings(t *testing.T) {
	t.Setenv("MOLTNET_PAIRINGS_JSON", `[{"id":"pair_1","remote_network_id":"remote"}]`)
	pairings := envPairings("MOLTNET_PAIRINGS_JSON")
	if len(pairings) != 1 || pairings[0].ID != "pair_1" {
		t.Fatalf("unexpected pairings %#v", pairings)
	}

	t.Setenv("MOLTNET_PAIRINGS_JSON", `not-json`)
	if got := envPairings("MOLTNET_PAIRINGS_JSON"); got != nil {
		t.Fatalf("expected nil on invalid json, got %#v", got)
	}
}
