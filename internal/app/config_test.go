package app

import (
	"strings"
	"testing"
)

func TestConfigFromEnv(t *testing.T) {
	t.Setenv("MOLTNET_ALLOW_HUMAN_INGRESS", "false")
	t.Setenv("MOLTNET_ALLOW_DIRECT_MESSAGES", "false")
	t.Setenv("MOLTNET_LISTEN_ADDR", ":9999")
	t.Setenv("MOLTNET_NETWORK_ID", "lab")
	t.Setenv("MOLTNET_NETWORK_NAME", "Lab")
	t.Setenv("MOLTNET_PAIRINGS_JSON", `[{"id":"pair_1","remote_network_id":"remote","remote_network_name":"Remote Lab","status":"connected"}]`)

	config, err := ConfigFromEnv("1.2.3")
	if err != nil {
		t.Fatalf("ConfigFromEnv() error = %v", err)
	}
	if config.AllowHumanIngress {
		t.Fatalf("expected human ingress disabled, got %#v", config)
	}
	if !config.DisableDirectMessages {
		t.Fatalf("expected direct messages disabled, got %#v", config)
	}
	if config.ListenAddr != ":9999" || config.NetworkID != "lab" || config.NetworkName != "Lab" || config.Version != "1.2.3" {
		t.Fatalf("unexpected config %#v", config)
	}
	if len(config.Pairings) != 1 || config.Pairings[0].RemoteNetworkID != "remote" {
		t.Fatalf("unexpected pairings %#v", config.Pairings)
	}
}

func TestConfigFromEnvRejectsInvalidPairingsJSON(t *testing.T) {
	t.Setenv("MOLTNET_PAIRINGS_JSON", `not-json`)

	if _, err := ConfigFromEnv("1.2.3"); err == nil {
		t.Fatal("expected invalid pairings env error")
	}
}

func TestEnvValueAndBoolValue(t *testing.T) {
	t.Setenv("MOLTNET_VALUE", "  configured  ")
	if got, ok := envValue("MOLTNET_VALUE"); !ok || got != "configured" {
		t.Fatalf("unexpected envValue() result value=%q ok=%v", got, ok)
	}
	if got, ok := envValue("MOLTNET_MISSING"); ok || got != "" {
		t.Fatalf("expected missing envValue() result, got value=%q ok=%v", got, ok)
	}

	t.Setenv("MOLTNET_FLAG", "yes")
	if got, ok := envBoolValue("MOLTNET_FLAG"); !ok || !got {
		t.Fatalf("expected truthy envBoolValue(), got value=%v ok=%v", got, ok)
	}

	t.Setenv("MOLTNET_FLAG", "off")
	if got, ok := envBoolValue("MOLTNET_FLAG"); !ok || got {
		t.Fatalf("expected falsy envBoolValue(), got value=%v ok=%v", got, ok)
	}

	t.Setenv("MOLTNET_FLAG", "unknown")
	if got, ok := envBoolValue("MOLTNET_FLAG"); ok || got {
		t.Fatalf("expected invalid envBoolValue() result, got value=%v ok=%v", got, ok)
	}
}

func TestEnvPairings(t *testing.T) {
	t.Setenv("MOLTNET_PAIRINGS_JSON", `[{"id":"pair_1","remote_network_id":"remote"}]`)
	pairings, err := envPairings("MOLTNET_PAIRINGS_JSON")
	if err != nil {
		t.Fatalf("envPairings() error = %v", err)
	}
	if len(pairings) != 1 || pairings[0].ID != "pair_1" {
		t.Fatalf("unexpected pairings %#v", pairings)
	}

	t.Setenv("MOLTNET_PAIRINGS_JSON", `not-json`)
	got, err := envPairings("MOLTNET_PAIRINGS_JSON")
	if err == nil || !strings.Contains(err.Error(), "MOLTNET_PAIRINGS_JSON must contain valid JSON") {
		t.Fatalf("expected invalid json error, got pairings=%#v err=%v", got, err)
	}

	t.Setenv("MOLTNET_PAIRINGS_JSON", "")
	got, err = envPairings("MOLTNET_PAIRINGS_JSON")
	if err != nil || got != nil {
		t.Fatalf("expected empty env pairings to return nil,nil, got %#v %v", got, err)
	}
}
