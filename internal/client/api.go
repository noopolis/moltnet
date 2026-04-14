package client

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"

	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const defaultTimeout = 10 * time.Second

type Client struct {
	attachment clientconfig.AttachmentConfig
	httpClient *http.Client
	token      string
}

func New(attachment clientconfig.AttachmentConfig) (*Client, error) {
	token, err := attachment.ResolveToken()
	if err != nil {
		return nil, err
	}

	return &Client{
		attachment: attachment,
		httpClient: &http.Client{Timeout: defaultTimeout},
		token:      token,
	}, nil
}

func (c *Client) ListRooms(ctx context.Context) (protocol.RoomPage, error) {
	var page protocol.RoomPage
	if err := c.doJSON(ctx, http.MethodGet, "/v1/rooms", nil, &page); err != nil {
		return protocol.RoomPage{}, err
	}
	return page, nil
}

func (c *Client) GetRoom(ctx context.Context, roomID string) (protocol.Room, error) {
	var room protocol.Room
	if err := c.doJSON(ctx, http.MethodGet, "/v1/rooms/"+url.PathEscape(roomID), nil, &room); err != nil {
		return protocol.Room{}, err
	}
	return room, nil
}

func (c *Client) ListRoomMessages(ctx context.Context, roomID string, page protocol.PageRequest) (protocol.MessagePage, error) {
	var messages protocol.MessagePage
	path := "/v1/rooms/" + url.PathEscape(roomID) + "/messages" + encodePage(page)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &messages); err != nil {
		return protocol.MessagePage{}, err
	}
	return messages, nil
}

func (c *Client) ListDMs(ctx context.Context) (protocol.DirectConversationPage, error) {
	var page protocol.DirectConversationPage
	if err := c.doJSON(ctx, http.MethodGet, "/v1/dms", nil, &page); err != nil {
		return protocol.DirectConversationPage{}, err
	}
	return page, nil
}

func (c *Client) GetDM(ctx context.Context, dmID string) (protocol.DirectConversation, error) {
	var dm protocol.DirectConversation
	if err := c.doJSON(ctx, http.MethodGet, "/v1/dms/"+url.PathEscape(dmID), nil, &dm); err != nil {
		return protocol.DirectConversation{}, err
	}
	return dm, nil
}

func (c *Client) ListDMMessages(ctx context.Context, dmID string, page protocol.PageRequest) (protocol.MessagePage, error) {
	var messages protocol.MessagePage
	path := "/v1/dms/" + url.PathEscape(dmID) + "/messages" + encodePage(page)
	if err := c.doJSON(ctx, http.MethodGet, path, nil, &messages); err != nil {
		return protocol.MessagePage{}, err
	}
	return messages, nil
}

func (c *Client) SendMessage(ctx context.Context, request protocol.SendMessageRequest) (protocol.MessageAccepted, error) {
	var accepted protocol.MessageAccepted
	if err := c.doJSON(ctx, http.MethodPost, "/v1/messages", request, &accepted); err != nil {
		return protocol.MessageAccepted{}, err
	}
	return accepted, nil
}

func (c *Client) RegisterAgent(
	ctx context.Context,
	request protocol.RegisterAgentRequest,
) (protocol.AgentRegistration, error) {
	var registration protocol.AgentRegistration
	if err := c.doJSON(ctx, http.MethodPost, "/v1/agents/register", request, &registration); err != nil {
		return protocol.AgentRegistration{}, err
	}
	return registration, nil
}

func (c *Client) doJSON(ctx context.Context, method string, requestPath string, body any, out any) error {
	endpoint := strings.TrimRight(c.attachment.BaseURL, "/") + "/" + strings.TrimLeft(requestPath, "/")
	if !strings.HasPrefix(requestPath, "/") {
		endpoint = strings.TrimRight(c.attachment.BaseURL, "/") + "/" + requestPath
	}

	var requestBody io.Reader
	if body != nil {
		payload, err := json.Marshal(body)
		if err != nil {
			return fmt.Errorf("encode Moltnet request: %w", err)
		}
		requestBody = bytes.NewReader(payload)
	}

	request, err := http.NewRequestWithContext(ctx, method, endpoint, requestBody)
	if err != nil {
		return fmt.Errorf("build Moltnet request: %w", err)
	}
	if body != nil {
		request.Header.Set("Content-Type", "application/json")
	}
	if c.token != "" {
		request.Header.Set("Authorization", "Bearer "+c.token)
	}

	response, err := c.httpClient.Do(request)
	if err != nil {
		return fmt.Errorf("request Moltnet %s %s: %w", method, request.URL.Redacted(), err)
	}
	defer response.Body.Close()

	if response.StatusCode < 200 || response.StatusCode >= 300 {
		message, _ := io.ReadAll(io.LimitReader(response.Body, 1<<20))
		trimmed := strings.TrimSpace(string(message))
		if trimmed == "" {
			return fmt.Errorf("moltnet %s %s returned %s", method, request.URL.Redacted(), response.Status)
		}
		return fmt.Errorf("moltnet %s %s returned %s: %s", method, request.URL.Redacted(), response.Status, trimmed)
	}

	if out == nil {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		return nil
	}
	if err := json.NewDecoder(io.LimitReader(response.Body, 1<<20)).Decode(out); err != nil {
		return fmt.Errorf("decode Moltnet response: %w", err)
	}

	return nil
}

func encodePage(page protocol.PageRequest) string {
	values := url.Values{}
	if strings.TrimSpace(page.Before) != "" {
		values.Set("before", strings.TrimSpace(page.Before))
	}
	if strings.TrimSpace(page.After) != "" {
		values.Set("after", strings.TrimSpace(page.After))
	}
	if page.Limit > 0 {
		values.Set("limit", fmt.Sprintf("%d", page.Limit))
	}
	if encoded := values.Encode(); encoded != "" {
		return "?" + encoded
	}
	return ""
}
