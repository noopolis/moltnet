package bridgeconfig

import (
	"path/filepath"
	"testing"
)

func TestWebhookRuntimeValidation(t *testing.T) {
	base := RuntimeConfig{Kind: RuntimeWebhook, ControlURL: "https://example.com/hooks/test", TokenEnv: "GROK_WEBHOOK_KEY", ReceiptStorePath: filepath.Join(t.TempDir(), "receipts.json")}
	if err := validateWebhookRuntime(base); err != nil {
		t.Fatal(err)
	}
	for _, change := range []func(*RuntimeConfig){
		func(r *RuntimeConfig) { r.ControlURL = "http://example.com/hooks" },
		func(r *RuntimeConfig) { r.ControlURL = "https://user:secret@example.com/hooks" },
		func(r *RuntimeConfig) { r.ControlURL = "https://example.com/hooks?key=secret" },
		func(r *RuntimeConfig) { r.TokenEnv = "" },
		func(r *RuntimeConfig) { r.TokenEnv = "KEY\nOTHER" },
		func(r *RuntimeConfig) { r.Token = "inline-secret" },
		func(r *RuntimeConfig) { r.ReceiptStorePath = "relative.json" },
		func(r *RuntimeConfig) { r.MaxRunsPerDay = -1 },
	} {
		cfg := base
		change(&cfg)
		if err := validateWebhookRuntime(cfg); err == nil {
			t.Fatal("unsafe webhook config accepted")
		}
	}
	t.Setenv("GROK_WEBHOOK_KEY", "runtime-test-key")
	token, err := base.ResolveRuntimeToken()
	if err != nil || token.Reveal() != "runtime-test-key" {
		t.Fatal("runtime key resolution failed")
	}
}
