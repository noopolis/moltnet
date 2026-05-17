package main

import (
	"context"
	"fmt"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/noopolis/moltnet/pkg/clientconfig"
)

const remoteSkillTimeout = 2 * time.Second

func moltnetSkillContentForAttachment(attachment clientconfig.AttachmentConfig) string {
	content, err := fetchRemoteMoltnetSkill(commandContext(), attachment)
	if err == nil && strings.TrimSpace(content) != "" {
		return content
	}
	return moltnetSkillContent()
}

func fetchRemoteMoltnetSkill(ctx context.Context, attachment clientconfig.AttachmentConfig) (string, error) {
	baseURL := strings.TrimSpace(attachment.BaseURL)
	if baseURL == "" {
		return "", fmt.Errorf("base_url is required")
	}

	token, err := attachment.ResolveToken()
	if err != nil {
		return "", err
	}

	ctx, cancel := context.WithTimeout(ctx, remoteSkillTimeout)
	defer cancel()

	request, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/skill.md", nil)
	if err != nil {
		return "", fmt.Errorf("build skill request: %w", err)
	}
	if token != "" {
		request.Header.Set("Authorization", "Bearer "+token)
	}

	response, err := http.DefaultClient.Do(request)
	if err != nil {
		return "", fmt.Errorf("request generated Moltnet skill: %w", err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		return "", fmt.Errorf("generated Moltnet skill returned %s", response.Status)
	}

	payload, err := io.ReadAll(io.LimitReader(response.Body, 256*1024))
	if err != nil {
		return "", fmt.Errorf("read generated Moltnet skill: %w", err)
	}
	content := strings.TrimSpace(string(payload))
	if !strings.Contains(content, "name: moltnet") {
		return "", fmt.Errorf("generated Moltnet skill is missing moltnet frontmatter")
	}
	return content + "\n", nil
}
