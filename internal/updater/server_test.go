package updater

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestHTTPServerProbeSendsBearerToken(t *testing.T) {
	var authHeader string
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		authHeader = request.Header.Get("Authorization")
		fmt.Fprint(response, `{"version":"v0.2.0"}`)
	}))
	defer server.Close()

	result, err := (HTTPServerProbe{Client: server.Client()}).ProbeServer(context.Background(), ServerProbeRequest{
		Token: "secret-token",
		URL:   server.URL,
	})
	if err != nil {
		t.Fatalf("ProbeServer() error = %v", err)
	}
	if result.Version != "v0.2.0" {
		t.Fatalf("server version = %q", result.Version)
	}
	if authHeader != "Bearer secret-token" {
		t.Fatalf("Authorization header = %q", authHeader)
	}
}
