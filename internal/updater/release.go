package updater

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"os"
	"strings"
)

const (
	defaultOwnerRepo      = "noopolis/moltnet"
	defaultGitHubAPIBase  = "https://api.github.com"
	defaultGitHubDownload = "https://github.com"
	maxReleaseBytes       = 200 << 20
)

type HTTPClient interface {
	Do(request *http.Request) (*http.Response, error)
}

type HTTPReleaseSource struct {
	APIBase         string
	Client          HTTPClient
	DownloadBaseURL string
	GitHubBase      string
	OwnerRepo       string
}

func NewHTTPReleaseSource(client HTTPClient) HTTPReleaseSource {
	return HTTPReleaseSource{
		Client:          client,
		DownloadBaseURL: strings.TrimSpace(os.Getenv("MOLTNET_DOWNLOAD_BASE_URL")),
		OwnerRepo:       strings.TrimSpace(os.Getenv("MOLTNET_REPO")),
	}
}

func (s HTTPReleaseSource) LatestVersion(ctx context.Context) (string, error) {
	if strings.TrimSpace(s.DownloadBaseURL) != "" {
		body, err := s.get(ctx, s.releaseMetadataURL())
		if err != nil {
			return "", err
		}
		var metadata struct {
			Version string `json:"version"`
		}
		if err := json.Unmarshal(body, &metadata); err != nil {
			return "", fmt.Errorf("decode release metadata: %w", err)
		}
		if strings.TrimSpace(metadata.Version) == "" {
			return "", fmt.Errorf("release metadata did not include a version")
		}
		return strings.TrimSpace(metadata.Version), nil
	}

	body, err := s.get(ctx, s.latestGitHubReleaseURL())
	if err != nil {
		return "", err
	}
	var release struct {
		TagName string `json:"tag_name"`
		Name    string `json:"name"`
	}
	if err := json.Unmarshal(body, &release); err != nil {
		return "", fmt.Errorf("decode GitHub release metadata: %w", err)
	}
	if strings.TrimSpace(release.TagName) != "" {
		return strings.TrimSpace(release.TagName), nil
	}
	if strings.TrimSpace(release.Name) != "" {
		return strings.TrimSpace(release.Name), nil
	}
	return "", fmt.Errorf("GitHub release metadata did not include a version")
}

func (s HTTPReleaseSource) Archive(ctx context.Context, version string, assetName string) ([]byte, error) {
	return s.get(ctx, s.assetURL(version, assetName))
}

func (s HTTPReleaseSource) Checksums(ctx context.Context, version string) ([]byte, error) {
	return s.get(ctx, s.assetURL(version, "checksums.txt"))
}

func (s HTTPReleaseSource) releaseMetadataURL() string {
	return joinURL(s.DownloadBaseURL, "release.json")
}

func (s HTTPReleaseSource) latestGitHubReleaseURL() string {
	apiBase := strings.TrimRight(strings.TrimSpace(s.APIBase), "/")
	if apiBase == "" {
		apiBase = defaultGitHubAPIBase
	}
	return fmt.Sprintf("%s/repos/%s/releases/latest", apiBase, s.ownerRepo())
}

func (s HTTPReleaseSource) assetURL(version string, assetName string) string {
	if strings.TrimSpace(s.DownloadBaseURL) != "" {
		return joinURL(s.DownloadBaseURL, assetName)
	}
	githubBase := strings.TrimRight(strings.TrimSpace(s.GitHubBase), "/")
	if githubBase == "" {
		githubBase = defaultGitHubDownload
	}
	return fmt.Sprintf("%s/%s/releases/download/%s/%s", githubBase, s.ownerRepo(), tagVersion(version), url.PathEscape(assetName))
}

func (s HTTPReleaseSource) ownerRepo() string {
	if strings.TrimSpace(s.OwnerRepo) != "" {
		return strings.TrimSpace(s.OwnerRepo)
	}
	return defaultOwnerRepo
}

func (s HTTPReleaseSource) get(ctx context.Context, requestURL string) ([]byte, error) {
	client := s.Client
	if client == nil {
		client = http.DefaultClient
	}
	request, err := http.NewRequestWithContext(ctx, http.MethodGet, requestURL, nil)
	if err != nil {
		return nil, err
	}
	request.Header.Set("Accept", "application/json, application/octet-stream")

	response, err := client.Do(request)
	if err != nil {
		return nil, err
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1024))
		return nil, fmt.Errorf("download %s: unexpected HTTP status %s", requestURL, response.Status)
	}

	body, err := io.ReadAll(io.LimitReader(response.Body, maxReleaseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(body) > maxReleaseBytes {
		return nil, fmt.Errorf("download %s: response exceeded %d bytes", requestURL, maxReleaseBytes)
	}
	return body, nil
}

func tagVersion(version string) string {
	trimmed := strings.TrimSpace(version)
	if strings.HasPrefix(trimmed, "v") || strings.HasPrefix(trimmed, "V") {
		return trimmed
	}
	return "v" + trimmed
}

func joinURL(base string, element string) string {
	return strings.TrimRight(strings.TrimSpace(base), "/") + "/" + strings.TrimLeft(element, "/")
}
