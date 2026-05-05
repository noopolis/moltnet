package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
)

const DefaultServerTokenEnv = "MOLTNET_UPDATE_SERVER_TOKEN"

type HTTPServerProbe struct {
	Client HTTPClient
}

func (p HTTPServerProbe) ProbeServer(ctx context.Context, probe ServerProbeRequest) (ServerInfo, error) {
	serverURL := strings.TrimSpace(probe.URL)
	parsed, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil {
		return ServerInfo{}, err
	}
	if parsed.Scheme == "" || parsed.Host == "" {
		return ServerInfo{}, fmt.Errorf("server URL must include scheme and host")
	}
	parsed.Path = strings.TrimRight(parsed.Path, "/") + "/v1/network"
	parsed.RawQuery = ""
	parsed.Fragment = ""

	client := p.Client
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, parsed.String(), nil)
	if err != nil {
		return ServerInfo{}, err
	}
	if token := strings.TrimSpace(probe.Token); token != "" {
		request.Header.Set("Authorization", bearerAuthorization(token))
	}
	response, err := client.Do(request)
	if err != nil {
		return ServerInfo{}, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
		return ServerInfo{}, fmt.Errorf("server probe returned %s", response.Status)
	}
	var payload struct {
		Version string `json:"version"`
	}
	if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
		return ServerInfo{}, err
	}
	return ServerInfo{URL: strings.TrimSpace(serverURL), Version: strings.TrimSpace(payload.Version)}, nil
}

func bearerAuthorization(token string) string {
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		return token
	}
	return "Bearer " + token
}
