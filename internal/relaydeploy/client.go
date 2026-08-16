// Package relaydeploy implements `moltnet relay deploy`: a minimal
// Cloudflare REST API client (stdlib net/http only) plus deploy
// orchestration for the relay Worker embedded from the sibling relay/
// package.
package relaydeploy

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime/multipart"
	"net"
	"net/http"
	"net/textproto"
	"net/url"
	"strings"
	"time"
)

const (
	defaultBaseURL   = "https://api.cloudflare.com/client/v4"
	defaultTimeout   = 30 * time.Second
	maxResponseBytes = 1 << 20
)

// Client is a minimal Cloudflare REST API client covering exactly the calls
// `moltnet relay deploy` needs: token verification, account resolution,
// module worker upload, secret management, and workers.dev route/subdomain
// handling. Auth is always CLOUDFLARE_API_TOKEN-style bearer auth; OAuth is
// deferred (see PLAN.md).
type Client struct {
	baseURL string
	token   string
	http    *http.Client
}

// NewClient returns a Cloudflare API client authenticated with token.
func NewClient(token string) *Client {
	return &Client{
		baseURL: defaultBaseURL,
		token:   token,
		http:    &http.Client{Timeout: defaultTimeout},
	}
}

// NewClientForTesting returns a Client pointed at baseURL using httpClient,
// instead of the real Cloudflare API host. It exists so tests in other
// packages (e.g. cmd/moltnet, which cannot reach Client's unexported
// fields directly) can exercise a real end-to-end Deploy call against a
// net/http/httptest fake Cloudflare server. Production code never calls
// this; only NewClient does. baseURL's host must be loopback
// (127.0.0.1/::1/localhost, as net/http/httptest.Server always is) — this
// panics otherwise, so a baseURL pointed at a real host (production misuse,
// or a typo'd test) fails immediately instead of silently working.
func NewClientForTesting(token, baseURL string, httpClient *http.Client) *Client {
	if !isLoopbackBaseURL(baseURL) {
		panic(fmt.Sprintf("relaydeploy.NewClientForTesting: baseURL %q is not loopback (127.0.0.1/::1/localhost); this constructor is test-only", baseURL))
	}
	return &Client{baseURL: baseURL, token: token, http: httpClient}
}

// isLoopbackBaseURL reports whether baseURL's host is a loopback address or
// "localhost", the only hosts NewClientForTesting accepts.
func isLoopbackBaseURL(baseURL string) bool {
	parsed, err := url.Parse(baseURL)
	if err != nil {
		return false
	}
	host := parsed.Hostname()
	if host == "localhost" {
		return true
	}
	if ip := net.ParseIP(host); ip != nil {
		return ip.IsLoopback()
	}
	return false
}

func (c *Client) httpClient() *http.Client {
	if c.http != nil {
		return c.http
	}
	return http.DefaultClient
}

// CloudflareMessage is a single error or informational message from a
// Cloudflare API response envelope.
type CloudflareMessage struct {
	Code    int    `json:"code"`
	Message string `json:"message"`
}

type cloudflareEnvelope struct {
	Success bool                `json:"success"`
	Errors  []CloudflareMessage `json:"errors"`
	Result  json.RawMessage     `json:"result"`
}

// CloudflareAPIError reports a Cloudflare REST API call that returned
// success:false, keeping the HTTP status and the API's own messages.
type CloudflareAPIError struct {
	StatusCode int
	Messages   []CloudflareMessage
}

func (e *CloudflareAPIError) Error() string {
	if len(e.Messages) == 0 {
		return fmt.Sprintf("cloudflare api error (http %d)", e.StatusCode)
	}
	parts := make([]string, len(e.Messages))
	for i, message := range e.Messages {
		parts[i] = fmt.Sprintf("%d: %s", message.Code, message.Message)
	}
	return fmt.Sprintf("cloudflare api error (http %d): %s", e.StatusCode, strings.Join(parts, "; "))
}

// Unauthorized reports whether the error represents a rejected or
// insufficiently scoped API token, as opposed to e.g. a not-found result.
func (e *CloudflareAPIError) Unauthorized() bool {
	return e.StatusCode == http.StatusUnauthorized || e.StatusCode == http.StatusForbidden
}

func apiError(statusCode int, envelope cloudflareEnvelope) error {
	return &CloudflareAPIError{StatusCode: statusCode, Messages: envelope.Errors}
}

// request issues a Cloudflare API call and decodes the response envelope.
// The returned error is non-nil only for transport or decode failures;
// callers interpret envelope.Success and the HTTP status themselves, since
// what counts as a hard failure differs per endpoint (an unclaimed
// workers.dev subdomain is a normal, not exceptional, response).
func (c *Client) request(ctx context.Context, method, path string, body io.Reader, contentType string) (cloudflareEnvelope, int, error) {
	request, err := http.NewRequestWithContext(ctx, method, c.baseURL+path, body)
	if err != nil {
		return cloudflareEnvelope{}, 0, fmt.Errorf("build cloudflare request %s %s: %w", method, path, err)
	}
	request.Header.Set("Authorization", "Bearer "+c.token)
	if contentType != "" {
		request.Header.Set("Content-Type", contentType)
	}

	response, err := c.httpClient().Do(request)
	if err != nil {
		return cloudflareEnvelope{}, 0, fmt.Errorf("cloudflare request %s %s: %w", method, path, err)
	}
	defer response.Body.Close()

	var envelope cloudflareEnvelope
	if err := json.NewDecoder(io.LimitReader(response.Body, maxResponseBytes)).Decode(&envelope); err != nil {
		return cloudflareEnvelope{}, response.StatusCode, fmt.Errorf("decode cloudflare response for %s %s: %w", method, path, err)
	}
	return envelope, response.StatusCode, nil
}

// VerifyToken confirms CLOUDFLARE_API_TOKEN is valid and usable.
func (c *Client) VerifyToken(ctx context.Context) error {
	envelope, status, err := c.request(ctx, http.MethodGet, "/user/tokens/verify", nil, "")
	if err != nil {
		return err
	}
	if !envelope.Success {
		return apiError(status, envelope)
	}
	return nil
}

// ResolveAccountID returns the first Cloudflare account the token can see.
// Cloudflare tokens are typically scoped to exactly one account; if the
// token spans several, the first is used (matching wrangler's default
// behavior when only one account is configured).
func (c *Client) ResolveAccountID(ctx context.Context) (string, error) {
	envelope, status, err := c.request(ctx, http.MethodGet, "/accounts", nil, "")
	if err != nil {
		return "", err
	}
	if !envelope.Success {
		return "", apiError(status, envelope)
	}

	var accounts []struct {
		ID string `json:"id"`
	}
	if err := json.Unmarshal(envelope.Result, &accounts); err != nil {
		return "", fmt.Errorf("decode cloudflare accounts: %w", err)
	}
	if len(accounts) == 0 {
		return "", errors.New("cloudflare token has access to no accounts")
	}
	return accounts[0].ID, nil
}

// UploadWorkerModule uploads script as the module worker's mainModule entry
// point, alongside metadataJSON (bindings, migrations, compatibility date),
// creating or overwriting scriptName. Uploading an existing script name is
// how idempotent re-deploy updates the code.
func (c *Client) UploadWorkerModule(ctx context.Context, accountID, scriptName, mainModule string, script, metadataJSON []byte) error {
	var body bytes.Buffer
	writer := multipart.NewWriter(&body)

	metadataHeader := textproto.MIMEHeader{}
	metadataHeader.Set("Content-Disposition", `form-data; name="metadata"`)
	metadataHeader.Set("Content-Type", "application/json")
	metadataPart, err := writer.CreatePart(metadataHeader)
	if err != nil {
		return fmt.Errorf("build worker upload metadata part: %w", err)
	}
	if _, err := metadataPart.Write(metadataJSON); err != nil {
		return fmt.Errorf("write worker upload metadata part: %w", err)
	}

	scriptHeader := textproto.MIMEHeader{}
	scriptHeader.Set("Content-Disposition", fmt.Sprintf(`form-data; name=%q; filename=%q`, mainModule, mainModule))
	scriptHeader.Set("Content-Type", "application/javascript+module")
	scriptPart, err := writer.CreatePart(scriptHeader)
	if err != nil {
		return fmt.Errorf("build worker upload script part: %w", err)
	}
	if _, err := scriptPart.Write(script); err != nil {
		return fmt.Errorf("write worker upload script part: %w", err)
	}

	if err := writer.Close(); err != nil {
		return fmt.Errorf("close worker upload multipart body: %w", err)
	}

	path := fmt.Sprintf("/accounts/%s/workers/scripts/%s", url.PathEscape(accountID), url.PathEscape(scriptName))
	envelope, status, err := c.request(ctx, http.MethodPut, path, &body, writer.FormDataContentType())
	if err != nil {
		return err
	}
	if !envelope.Success {
		return apiError(status, envelope)
	}
	return nil
}

// SetSecret creates or updates a Worker secret binding.
func (c *Client) SetSecret(ctx context.Context, accountID, scriptName, name, value string) error {
	payload, err := json.Marshal(struct {
		Name string `json:"name"`
		Text string `json:"text"`
		Type string `json:"type"`
	}{Name: name, Text: value, Type: "secret_text"})
	if err != nil {
		return fmt.Errorf("encode secret payload: %w", err)
	}

	path := fmt.Sprintf("/accounts/%s/workers/scripts/%s/secrets", url.PathEscape(accountID), url.PathEscape(scriptName))
	envelope, status, err := c.request(ctx, http.MethodPut, path, bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}
	if !envelope.Success {
		return apiError(status, envelope)
	}
	return nil
}

// EnableWorkersDevRoute turns on the script's *.workers.dev route.
func (c *Client) EnableWorkersDevRoute(ctx context.Context, accountID, scriptName string) error {
	path := fmt.Sprintf("/accounts/%s/workers/scripts/%s/subdomain", url.PathEscape(accountID), url.PathEscape(scriptName))
	envelope, status, err := c.request(ctx, http.MethodPost, path, strings.NewReader(`{"enabled":true}`), "application/json")
	if err != nil {
		return err
	}
	if !envelope.Success {
		return apiError(status, envelope)
	}
	return nil
}

// WorkersDevSubdomain resolves the account-level workers.dev subdomain
// (e.g. "acme" in "acme.workers.dev"). The account-level subdomain is a
// one-time claim the API cannot perform on the caller's behalf (PLAN.md
// review finding): an unclaimed subdomain is reported as claimed=false with
// a nil error, not as a failure, so callers can print the one dashboard
// step instead of a generic error. Only an authentication failure (401/403)
// is treated as a hard error here.
func (c *Client) WorkersDevSubdomain(ctx context.Context, accountID string) (subdomain string, claimed bool, err error) {
	path := fmt.Sprintf("/accounts/%s/workers/subdomain", url.PathEscape(accountID))
	envelope, status, err := c.request(ctx, http.MethodGet, path, nil, "")
	if err != nil {
		return "", false, err
	}
	if !envelope.Success {
		if status == http.StatusUnauthorized || status == http.StatusForbidden {
			return "", false, apiError(status, envelope)
		}
		return "", false, nil
	}

	var result struct {
		Subdomain string `json:"subdomain"`
	}
	if err := json.Unmarshal(envelope.Result, &result); err != nil {
		return "", false, fmt.Errorf("decode cloudflare workers.dev subdomain: %w", err)
	}
	if strings.TrimSpace(result.Subdomain) == "" {
		return "", false, nil
	}
	return result.Subdomain, true, nil
}

// ClaimWorkersDevSubdomain registers subdomain as accountID's account-level
// workers.dev subdomain (PUT /accounts/{account_id}/workers/subdomain,
// Cloudflare's "Create Subdomain" operation) — the one-time claim
// WorkersDevSubdomain above reads back afterward. It is the API call the
// interactive claim prompt (cmd/moltnet's `relay deploy`) makes after an
// operator types a name in response to ErrWorkersDevSubdomainUnclaimed;
// callers should normalize and validate the name against
// NormalizeWorkersDevSubdomainName / ValidateWorkersDevSubdomainName before
// calling this, to avoid spending a round trip on an obviously invalid one.
//
// Cloudflare documents two error codes this call can return that callers
// should branch on rather than treat identically: 10031 ("subdomain already
// exists" — the name is taken by some account, this one included) is
// retryable with a different name; 10036 ("account already has a
// subdomain", HTTP 409 — this account has already claimed one, and
// Cloudflare allows exactly one per account, ever) is not a failure at all
// — the caller should re-read the existing claim via WorkersDevSubdomain
// and continue as already claimed (cmd/moltnet's interactive claim prompt
// does both). Any other rejection surfaces as a *CloudflareAPIError
// carrying whatever message Cloudflare returned, shown verbatim.
func (c *Client) ClaimWorkersDevSubdomain(ctx context.Context, accountID, subdomain string) error {
	payload, err := json.Marshal(struct {
		Subdomain string `json:"subdomain"`
	}{Subdomain: subdomain})
	if err != nil {
		return fmt.Errorf("encode workers.dev subdomain claim payload: %w", err)
	}

	path := fmt.Sprintf("/accounts/%s/workers/subdomain", url.PathEscape(accountID))
	envelope, status, err := c.request(ctx, http.MethodPut, path, bytes.NewReader(payload), "application/json")
	if err != nil {
		return err
	}
	if !envelope.Success {
		return apiError(status, envelope)
	}
	return nil
}
