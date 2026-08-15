package relaydeploy

import (
	"context"
	"strings"
	"testing"
)

func TestClientHappyPath(t *testing.T) {
	t.Parallel()
	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{
		accountID: "acct-happy",
		authOK:    true,
		subdomain: "acme",
	})
	client := fake.client()
	ctx := context.Background()

	if err := client.VerifyToken(ctx); err != nil {
		t.Fatalf("VerifyToken() error = %v", err)
	}

	accountID, err := client.ResolveAccountID(ctx)
	if err != nil {
		t.Fatalf("ResolveAccountID() error = %v", err)
	}
	if accountID != "acct-happy" {
		t.Fatalf("ResolveAccountID() = %q, want acct-happy", accountID)
	}

	script := []byte("export default {};")
	metadata := []byte(`{"main_module":"worker.js","bindings":[{"type":"durable_object_namespace","name":"RelayRoom","class_name":"RelayRoom"}],"migrations":{"new_tag":"v1","new_sqlite_classes":["RelayRoom"]}}`)
	if err := client.UploadWorkerModule(ctx, accountID, "moltnet-relay", "worker.js", script, metadata); err != nil {
		t.Fatalf("UploadWorkerModule() error = %v", err)
	}
	if got := string(fake.uploadedScript); got != string(script) {
		t.Fatalf("uploaded script = %q, want %q", got, script)
	}
	if got := string(fake.uploadedMetadata); got != string(metadata) {
		t.Fatalf("uploaded metadata = %q, want %q", got, metadata)
	}

	if err := client.SetSecret(ctx, accountID, "moltnet-relay", "RELAY_TOKEN", "secret-value"); err != nil {
		t.Fatalf("SetSecret() error = %v", err)
	}
	if value, ok := fake.secretValue("RELAY_TOKEN"); !ok || value != "secret-value" {
		t.Fatalf("secret RELAY_TOKEN = (%q, %v), want secret-value, true", value, ok)
	}

	if err := client.EnableWorkersDevRoute(ctx, accountID, "moltnet-relay"); err != nil {
		t.Fatalf("EnableWorkersDevRoute() error = %v", err)
	}
	if !fake.routeEnabled {
		t.Fatal("expected workers.dev route to be enabled")
	}

	subdomain, claimed, err := client.WorkersDevSubdomain(ctx, accountID)
	if err != nil {
		t.Fatalf("WorkersDevSubdomain() error = %v", err)
	}
	if !claimed || subdomain != "acme" {
		t.Fatalf("WorkersDevSubdomain() = (%q, %v), want (acme, true)", subdomain, claimed)
	}
}

func TestClientWorkersDevSubdomainUnclaimed(t *testing.T) {
	t.Parallel()
	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{
		accountID: "acct-unclaimed",
		authOK:    true,
		subdomain: "", // unclaimed
	})
	client := fake.client()

	subdomain, claimed, err := client.WorkersDevSubdomain(context.Background(), "acct-unclaimed")
	if err != nil {
		t.Fatalf("WorkersDevSubdomain() error = %v, want nil (unclaimed is not an error)", err)
	}
	if claimed {
		t.Fatal("WorkersDevSubdomain() claimed = true, want false")
	}
	if subdomain != "" {
		t.Fatalf("WorkersDevSubdomain() subdomain = %q, want empty", subdomain)
	}
}

func TestClientAuthFailure(t *testing.T) {
	t.Parallel()
	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{
		accountID:  "acct-auth-fail",
		authOK:     false,
		authStatus: 403,
	})
	client := fake.client()

	err := client.VerifyToken(context.Background())
	if err == nil {
		t.Fatal("VerifyToken() error = nil, want auth failure")
	}
	apiErr, ok := err.(*CloudflareAPIError)
	if !ok {
		t.Fatalf("VerifyToken() error type = %T, want *CloudflareAPIError", err)
	}
	if !apiErr.Unauthorized() {
		t.Fatalf("CloudflareAPIError.Unauthorized() = false for status %d, want true", apiErr.StatusCode)
	}
	if !strings.Contains(apiErr.Error(), "invalid token") {
		t.Fatalf("error message = %q, want it to mention the cloudflare message", apiErr.Error())
	}
}

func TestClientWorkersDevSubdomainAuthFailureIsHardError(t *testing.T) {
	t.Parallel()
	fake := newFakeCloudflareServer(t, fakeCloudflareConfig{
		accountID:       "acct-sub-auth-fail",
		authOK:          true,
		subdomain:       "",
		subdomainStatus: 401,
	})
	client := fake.client()

	_, claimed, err := client.WorkersDevSubdomain(context.Background(), "acct-sub-auth-fail")
	if err == nil {
		t.Fatal("WorkersDevSubdomain() error = nil, want auth failure surfaced as an error")
	}
	if claimed {
		t.Fatal("WorkersDevSubdomain() claimed = true, want false on error")
	}
}

func TestClientResolveAccountIDNoAccounts(t *testing.T) {
	t.Parallel()
	server := newEmptyAccountsCloudflareServer(t)
	client := server.client()
	_, err := client.ResolveAccountID(context.Background())
	if err == nil || !strings.Contains(err.Error(), "no accounts") {
		t.Fatalf("ResolveAccountID() error = %v, want no-accounts error", err)
	}
}

func TestNewClientForTestingAcceptsLoopbackHosts(t *testing.T) {
	t.Parallel()
	for _, baseURL := range []string{
		"http://127.0.0.1:8080",
		"http://localhost:8080",
		"http://[::1]:8080",
	} {
		func() {
			defer func() {
				if r := recover(); r != nil {
					t.Fatalf("NewClientForTesting(%q) panicked: %v", baseURL, r)
				}
			}()
			NewClientForTesting("token", baseURL, nil)
		}()
	}
}

func TestNewClientForTestingPanicsOnNonLoopbackHost(t *testing.T) {
	t.Parallel()
	for _, baseURL := range []string{
		"https://api.cloudflare.com/client/v4",
		"https://example.com",
		"http://8.8.8.8",
	} {
		func() {
			defer func() {
				if r := recover(); r == nil {
					t.Fatalf("NewClientForTesting(%q) did not panic, want a panic for a non-loopback baseURL", baseURL)
				}
			}()
			NewClientForTesting("token", baseURL, nil)
		}()
	}
}
