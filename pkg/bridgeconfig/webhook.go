package bridgeconfig

import (
	"fmt"
	"net"
	"net/url"
	"path/filepath"
	"strings"
)

// Webhook keys are supplied through the process environment, never a URL.
func validateWebhookRuntime(runtime RuntimeConfig) error {
	if err := validateURL("runtime.control_url", runtime.ControlURL); err != nil {
		return err
	}
	endpoint, err := url.Parse(runtime.ControlURL)
	if err != nil || endpoint.User != nil || endpoint.RawQuery != "" || endpoint.Fragment != "" {
		return fmt.Errorf("webhook control_url must not contain credentials, a query, or a fragment")
	}
	if endpoint.Scheme != "https" && endpoint.Hostname() != "localhost" && !net.ParseIP(endpoint.Hostname()).IsLoopback() {
		return fmt.Errorf("webhook control_url requires HTTPS except for loopback testing")
	}
	if runtime.Token.Reveal() != "" {
		return fmt.Errorf("webhook runtime.token is unsupported; use runtime.token_env")
	}
	if runtime.TokenEnv == "" {
		return fmt.Errorf("webhook runtime.token_env is required")
	}
	if err := validateEnvironmentName("runtime.token_env", runtime.TokenEnv); err != nil {
		return err
	}
	if runtime.ReceiptStorePath == "" || !filepath.IsAbs(runtime.ReceiptStorePath) || runtime.ReceiptStorePath != strings.TrimSpace(runtime.ReceiptStorePath) {
		return fmt.Errorf("webhook runtime.receipt_store_path must be an absolute path")
	}
	if runtime.MaxRunsPerDay < 0 || runtime.MaxRunsPerDay > 10000 {
		return fmt.Errorf("webhook runtime.max_runs_per_day must be between 0 (default 100) and 10000")
	}
	return nil
}
