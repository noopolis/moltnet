package transport

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authn "github.com/noopolis/moltnet/internal/auth"
)

func TestLocalConsoleSession(t *testing.T) {
	policy, err := authn.NewPolicy(authn.Config{Mode: authn.ModeOpen, Tokens: []authn.TokenConfig{
		{ID: "operator", Value: "test-owner-secret", Scopes: []authn.Scope{authn.ScopeObserve, authn.ScopeWrite, authn.ScopeAdmin}},
	}})
	if err != nil {
		t.Fatal(err)
	}
	for _, tc := range []struct {
		name, remote, host, origin, contentType, header string
		disabled                                        bool
		want                                            int
	}{
		{name: "local", want: 200},
		{name: "disabled", disabled: true, want: 403},
		{name: "cross origin", origin: "https://evil.example", want: 403},
		{name: "missing origin", origin: "-", want: 403},
		{name: "form post", contentType: "application/x-www-form-urlencoded", want: 403},
		{name: "remote", remote: "192.168.1.2:5000", want: 403},
		{name: "rebinding", host: "evil.example:8787", origin: "http://evil.example:8787", want: 403},
		{name: "forwarded", header: "Forwarded", want: 403},
		{name: "forwarded for", header: "X-Forwarded-For", want: 403},
		{name: "forwarded host", header: "X-Forwarded-Host", want: 403},
		{name: "forwarded proto", header: "X-Forwarded-Proto", want: 403},
	} {
		t.Run(tc.name, func(t *testing.T) {
			config := ConsoleConfig{LocalOwnerToken: "test-owner-secret"}
			if tc.disabled {
				config.LocalOwnerToken = ""
			}
			mux := http.NewServeMux()
			registerConsoleSessionRoutes(mux, policy, nil, config)
			req := httptest.NewRequest("POST", "http://127.0.0.1:8787/v1/console/session", strings.NewReader(`{"local":true,"remember":true}`))
			req.RemoteAddr = "127.0.0.1:5000"
			if tc.remote != "" {
				req.RemoteAddr = tc.remote
			}
			if tc.host != "" {
				req.Host = tc.host
			}
			origin := "http://127.0.0.1:8787"
			if tc.origin != "" {
				origin = tc.origin
			}
			if origin != "-" {
				req.Header.Set("Origin", origin)
			}
			ct := "application/json"
			if tc.contentType != "" {
				ct = tc.contentType
			}
			req.Header.Set("Content-Type", ct)
			if tc.header != "" {
				req.Header.Set(tc.header, "evil.example")
			}
			out := httptest.NewRecorder()
			mux.ServeHTTP(out, req)
			if out.Code != tc.want {
				t.Fatalf("status=%d body=%s", out.Code, out.Body.String())
			}
			if strings.Contains(out.Body.String(), "test-owner-secret") {
				t.Fatal("token leaked in response body")
			}
			if tc.want != 200 {
				if len(out.Result().Cookies()) != 0 {
					t.Fatal("failed login set a cookie")
				}
				return
			}
			cookies := out.Result().Cookies()
			if len(cookies) != 1 {
				t.Fatal("missing session cookie")
			}
			cookie := cookies[0]
			if !cookie.HttpOnly || cookie.SameSite != http.SameSiteStrictMode || cookie.MaxAge != 2592000 {
				t.Fatalf("incorrect cookie flags: %+v", cookie)
			}
			logout := httptest.NewRequest("DELETE", "http://127.0.0.1:8787/v1/console/session", nil)
			logout.Header.Set("Origin", "http://127.0.0.1:8787")
			logout.Header.Set("Content-Type", "application/json")
			result := httptest.NewRecorder()
			mux.ServeHTTP(result, logout)
			if result.Code != 204 || result.Result().Cookies()[0].MaxAge != -1 {
				t.Fatal("logout did not clear session")
			}
		})
	}
}

func TestConsoleManualTokenLogin(t *testing.T) {
	policy, _ := authn.NewPolicy(authn.Config{Mode: authn.ModeOpen, Tokens: []authn.TokenConfig{{ID: "observer", Value: "observe-secret", Scopes: []authn.Scope{authn.ScopeObserve}}}})
	mux := http.NewServeMux()
	registerConsoleSessionRoutes(mux, policy, nil, ConsoleConfig{})
	for _, tc := range []struct {
		token string
		want  int
	}{{"observe-secret", 200}, {"wrong", 401}, {"", 401}} {
		req := httptest.NewRequest("POST", "http://localhost/v1/console/session", strings.NewReader(`{"token":"`+tc.token+`"}`))
		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Origin", "http://localhost")
		out := httptest.NewRecorder()
		mux.ServeHTTP(out, req)
		if out.Code != tc.want {
			t.Fatalf("status=%d want=%d", out.Code, tc.want)
		}
		if tc.want == 200 && out.Result().Cookies()[0].MaxAge != 0 {
			t.Fatal("session login persisted unexpectedly")
		}
	}
}
