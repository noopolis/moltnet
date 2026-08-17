package transport

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// loopbackRequest builds a GET request that looks exactly like one from a
// direct, unproxied local caller: httptest.NewRequest defaults RemoteAddr to
// the non-loopback "192.0.2.1:1234" (a documentation address), so tests that
// want the loopback carve-out have to set it explicitly.
func loopbackRequest(target string) *http.Request {
	request := httptest.NewRequest(http.MethodGet, target, nil)
	request.RemoteAddr = "127.0.0.1:54321"
	return request
}

// TestInstallMarkdownAnonymousLoopbackNeverLeaksPrivateRooms is the P1-1
// regression test: a real bug (live-proven) had the loopback carve-out
// register installMarkdownHandler directly, bypassing requestWithAuthMode,
// so authn.ModeFromContext defaulted to ModeNone for the whole render —
// including the room-listing call the real rooms.Service makes, which
// computes each room's Access from context (canReadRoom,
// internal/rooms/access_policy.go). mode == ModeNone makes every room
// readable unconditionally (canReadRoom's fast path), so the renderer
// treated an anonymous loopback caller as being on a fully unauthenticated
// network: a private room ("secret-ops") showed up in the anonymous
// install.md output on a bearer-protected, public-read network — the exact
// same room set GET /v1/rooms (correctly auth-mode-aware) would only ever
// show a *bearer-authenticated* caller.
//
// This uses the real rooms.Service (not fakeService), because fakeService's
// ListRoomsContext ignores context entirely and never reproduces the bug —
// the leak lives in production room-access computation, not in the
// transport package's install.md rendering logic alone. It also drives a
// genuine httptest.NewServer connection rather than a synthetic RemoteAddr,
// so the request really did originate from 127.0.0.1 the way a live
// exploit would.
func TestInstallMarkdownAnonymousLoopbackNeverLeaksPrivateRooms(t *testing.T) {
	memory := store.NewMemoryStore()
	service := rooms.NewService(rooms.ServiceConfig{
		AllowHumanIngress: true,
		NetworkID:         "local_lab",
		NetworkName:       "Local Lab",
		Version:           "test",
		Store:             memory,
		Messages:          memory,
		Broker:            events.NewBroker(),
	})
	for _, room := range []protocol.CreateRoomRequest{
		{ID: "lobby", Visibility: protocol.RoomVisibilityPublic},
		{ID: "secret-ops", Visibility: protocol.RoomVisibilityPrivate},
	} {
		if _, err := service.CreateRoom(room); err != nil {
			t.Fatalf("CreateRoom(%q) error = %v", room.ID, err)
		}
	}

	operatorToken := authn.TokenConfig{ID: "operator", Value: "operator-secret", Scopes: []authn.Scope{authn.ScopeObserve, authn.ScopeWrite, authn.ScopeAdmin}}
	policy, err := authn.NewPolicy(authn.Config{
		Mode:       authn.ModeBearer,
		PublicRead: true,
		Tokens:     []authn.TokenConfig{operatorToken},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}

	server := httptest.NewServer(NewHTTPHandler(service, policy))
	defer server.Close()

	// Ground truth: an authenticated observer sees both rooms.
	authedBody := getBody(t, server.URL+"/v1/rooms", "Bearer "+operatorToken.Value)
	for _, want := range []string{"lobby", "secret-ops"} {
		if !strings.Contains(authedBody, want) {
			t.Fatalf("authenticated GET /v1/rooms missing %q:\n%s", want, authedBody)
		}
	}

	// public_read: true lets an anonymous caller read /v1/rooms too, but
	// only the public room — this is the posture install.md must match.
	anonymousRoomsBody := getBody(t, server.URL+"/v1/rooms", "")
	if strings.Contains(anonymousRoomsBody, "secret-ops") {
		t.Fatalf("anonymous GET /v1/rooms leaked the private room \"secret-ops\":\n%s", anonymousRoomsBody)
	}
	if !strings.Contains(anonymousRoomsBody, "lobby") {
		t.Fatalf("anonymous GET /v1/rooms should list the public room \"lobby\":\n%s", anonymousRoomsBody)
	}

	installResponse, err := http.Get(server.URL + "/install.md")
	if err != nil {
		t.Fatalf("GET /install.md error = %v", err)
	}
	defer installResponse.Body.Close()
	if installResponse.StatusCode != http.StatusOK {
		t.Fatalf("expected anonymous loopback GET /install.md to serve, got %d", installResponse.StatusCode)
	}
	bodyBytes, err := io.ReadAll(installResponse.Body)
	if err != nil {
		t.Fatalf("read /install.md body: %v", err)
	}
	body := string(bodyBytes)

	if strings.Contains(body, "secret-ops") {
		t.Fatalf("anonymous /install.md leaked the private room \"secret-ops\":\n%s", body)
	}
	if !strings.Contains(body, "lobby") {
		t.Fatalf("anonymous /install.md should still list the public room \"lobby\" (public_read: true):\n%s", body)
	}
}

// getBody issues a GET request to url, optionally with an Authorization
// header (skipped when authorization is ""), and returns the response body
// as a string. It fails the test on any transport error or non-200 status.
func getBody(t *testing.T, url string, authorization string) string {
	t.Helper()
	request, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		t.Fatalf("NewRequest(%q) error = %v", url, err)
	}
	if authorization != "" {
		request.Header.Set("Authorization", authorization)
	}
	response, err := http.DefaultClient.Do(request)
	if err != nil {
		t.Fatalf("GET %q error = %v", url, err)
	}
	defer response.Body.Close()
	if response.StatusCode != http.StatusOK {
		t.Fatalf("GET %q status = %d", url, response.StatusCode)
	}
	bodyBytes, err := io.ReadAll(response.Body)
	if err != nil {
		t.Fatalf("read %q body: %v", url, err)
	}
	return string(bodyBytes)
}

// TestInstallMarkdownAnonymousOnDirectLoopbackRequest covers PLAN.md phase
// 6a review, item 3/P1-2: GET /install.md — the join guide, no secrets — is
// readable without auth when the request itself looks like a direct,
// unproxied local call, even on a bearer-protected network that would
// otherwise 401 it.
func TestInstallMarkdownAnonymousOnDirectLoopbackRequest(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{
		Mode: authn.ModeBearer,
		Tokens: []authn.TokenConfig{
			{ID: "operator", Value: "operator-secret", Scopes: []authn.Scope{authn.ScopeObserve, authn.ScopeWrite, authn.ScopeAdmin}},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	handler := NewHTTPHandler(&fakeService{
		network: protocol.Network{ID: "local_lab", Name: "Local Lab"},
		rooms:   []protocol.Room{{ID: "lab", Name: "Lab"}},
	}, policy)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, loopbackRequest("/install.md"))

	if response.Code != http.StatusOK {
		t.Fatalf("expected a direct loopback request to serve install.md, got %d: %s", response.Code, response.Body.String())
	}
	if !strings.Contains(response.Body.String(), "# Join Local Lab Moltnet") {
		t.Fatalf("unexpected install markdown body:\n%s", response.Body.String())
	}

	// The rest of the bearer-protected surface is unaffected — only
	// /install.md gets this carve-out.
	response = httptest.NewRecorder()
	handler.ServeHTTP(response, loopbackRequest("/skill.md"))
	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected /skill.md to stay protected for a direct loopback caller, got %d", response.Code)
	}
}

// TestInstallMarkdownGatedForNonLoopbackRequest is the negative space: a
// request whose RemoteAddr is not loopback (httptest.NewRequest's default,
// "192.0.2.1:1234") keeps requiring auth exactly as before.
func TestInstallMarkdownGatedForNonLoopbackRequest(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{
		Mode: authn.ModeBearer,
		Tokens: []authn.TokenConfig{
			{ID: "operator", Value: "operator-secret", Scopes: []authn.Scope{authn.ScopeObserve}},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	handler := NewHTTPHandler(&fakeService{
		network: protocol.Network{ID: "local_lab", Name: "Local Lab"},
	}, policy)

	response := httptest.NewRecorder()
	request := httptest.NewRequest(http.MethodGet, "/install.md", nil)
	handler.ServeHTTP(response, request)

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected a non-loopback caller to still require auth for install.md, got %d", response.Code)
	}
}

// TestInstallMarkdownGatedWhenLoopbackRequestCarriesForwardedFor covers
// P1-2's proxy-indication check: a loopback RemoteAddr alone is not enough
// once the request itself shows signs of having been forwarded — that is
// exactly the "loopback bind behind a reverse proxy" topology this repo's
// own guides recommend (guides/public-open-networks.md,
// guides/securing-remote-agents.md), under which every request the app
// process sees really does come from 127.0.0.1.
func TestInstallMarkdownGatedWhenLoopbackRequestCarriesForwardedFor(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{
		Mode: authn.ModeBearer,
		Tokens: []authn.TokenConfig{
			{ID: "operator", Value: "operator-secret", Scopes: []authn.Scope{authn.ScopeObserve}},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	handler := NewHTTPHandler(&fakeService{
		network: protocol.Network{ID: "local_lab", Name: "Local Lab"},
	}, policy)

	for _, header := range []string{"X-Forwarded-For", "X-Real-IP"} {
		t.Run(header, func(t *testing.T) {
			request := loopbackRequest("/install.md")
			request.Header.Set(header, "203.0.113.7")
			response := httptest.NewRecorder()
			handler.ServeHTTP(response, request)

			if response.Code != http.StatusUnauthorized {
				t.Fatalf("expected a loopback request carrying %s to stay gated, got %d", header, response.Code)
			}
		})
	}
}

// TestInstallMarkdownGatedWhenTrustForwardedProtoSet covers P1-2's other
// signal: once the operator has told Moltnet a proxy is in the path
// (server.trust_forwarded_proto), a loopback RemoteAddr no longer proves the
// request originated on this machine, even when this particular request
// carries neither X-Forwarded-For nor X-Real-IP.
func TestInstallMarkdownGatedWhenTrustForwardedProtoSet(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{
		Mode:                authn.ModeBearer,
		TrustForwardedProto: true,
		Tokens: []authn.TokenConfig{
			{ID: "operator", Value: "operator-secret", Scopes: []authn.Scope{authn.ScopeObserve}},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	handler := NewHTTPHandler(&fakeService{
		network: protocol.Network{ID: "local_lab", Name: "Local Lab"},
	}, policy)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, loopbackRequest("/install.md"))

	if response.Code != http.StatusUnauthorized {
		t.Fatalf("expected a loopback request to stay gated when trust_forwarded_proto is set, got %d", response.Code)
	}
}

// TestInstallMarkdownContentBearerShowsTokenFlags covers PLAN.md phase 6a,
// item 3's other fix: the generated join guide's `moltnet connect` example
// used to omit --token/--token-env entirely on a bearer-auth network (the
// exact dead end the field report's Grok agent hit). It must now show a
// concrete way to supply a token.
func TestInstallMarkdownContentBearerShowsTokenFlags(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{
		Mode: authn.ModeBearer,
		Tokens: []authn.TokenConfig{
			{ID: "operator", Value: "operator-secret", Scopes: []authn.Scope{authn.ScopeObserve, authn.ScopeWrite, authn.ScopeAdmin}},
		},
	})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	handler := NewHTTPHandler(&fakeService{
		network: protocol.Network{ID: "local_lab", Name: "Local Lab"},
		rooms:   []protocol.Room{{ID: "lab", Name: "Lab"}},
	}, policy)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, loopbackRequest("/install.md"))
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}

	body := response.Body.String()
	for _, want := range []string{
		"--auth-mode bearer",
		"--token-env MOLTNET_TOKEN",
		"ask the operator for a token",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("install markdown for a bearer network missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "--registration open") {
		t.Fatalf("a bearer-only network (no open registration) should not suggest --registration open\n%s", body)
	}
}

// TestInstallMarkdownContentOpenRegistrationLeadsWithSelfRegister pins that
// an open-registration network's join guide leads with self-registration
// (--registration open) rather than a command that requires a pre-existing
// token — the other half of item 3.
func TestInstallMarkdownContentOpenRegistrationLeadsWithSelfRegister(t *testing.T) {
	t.Parallel()

	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeOpen})
	if err != nil {
		t.Fatalf("NewPolicy() error = %v", err)
	}
	handler := NewHTTPHandler(&fakeService{
		network: protocol.Network{ID: "local_lab", Name: "Local Lab"},
		rooms:   []protocol.Room{{ID: "lab", Name: "Lab"}},
	}, policy)

	response := httptest.NewRecorder()
	handler.ServeHTTP(response, loopbackRequest("/install.md"))
	if response.Code != http.StatusOK {
		t.Fatalf("unexpected status %d: %s", response.Code, response.Body.String())
	}

	body := response.Body.String()
	for _, want := range []string{
		"--registration open",
		"claims `<agent-id>` for you and writes your own generated agent token",
		"You never need a token from the operator",
	} {
		if !strings.Contains(body, want) {
			t.Fatalf("install markdown for an open-registration network missing %q\n%s", want, body)
		}
	}
	if strings.Contains(body, "--token-env") {
		t.Fatalf("an open-registration network should not need --token-env in its connect example\n%s", body)
	}
}
