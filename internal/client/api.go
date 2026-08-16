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
	return newClient(attachment, nil)
}

// NewLoopbackOnly is New's variant for callers whose documented invariant is
// that a request never leaves the local machine — the zero-setup operator
// fallback (resolveOperatorClient, cmd/moltnet/client_operator_fallback.go),
// which only ever dials a loopback address it derived itself. Without this,
// the standard library's default redirect policy still follows a same- or
// cross-host 307/308 the local server issues: Go strips the Authorization
// header on a cross-host hop, but the request body — a message's text, for
// a send — still goes wherever the redirect points. refuseCrossHostRedirect
// makes that impossible instead of merely "usually fine because nothing
// redirects today".
func NewLoopbackOnly(attachment clientconfig.AttachmentConfig) (*Client, error) {
	return newClient(attachment, refuseCrossHostRedirect)
}

func newClient(attachment clientconfig.AttachmentConfig, checkRedirect func(*http.Request, []*http.Request) error) (*Client, error) {
	token, err := attachment.ResolveToken()
	if err != nil {
		return nil, err
	}

	return &Client{
		attachment: attachment,
		httpClient: &http.Client{Timeout: defaultTimeout, CheckRedirect: checkRedirect},
		token:      token,
	}, nil
}

// refuseCrossHostRedirect is NewLoopbackOnly's http.Client.CheckRedirect: it
// allows a redirect back to the same host the original request dialed
// (via[0] is always that original request), and refuses — with a clear,
// named error instead of silently following it — any redirect elsewhere.
// "Same host" compares URL.Host (host:port, not just hostname): two
// services on the same loopback IP but different ports are still two
// different destinations for this invariant, and the operator fallback
// this backs (resolveOperatorClient) already derived one specific
// host:port from server.listen_addr — nothing else is "the local machine"
// as far as it is concerned.
func refuseCrossHostRedirect(request *http.Request, via []*http.Request) error {
	if len(via) == 0 {
		return nil
	}
	origin := via[0].URL.Host
	if request.URL.Host == origin {
		return nil
	}
	return fmt.Errorf(
		"refusing redirect from %s to a different host %s: this client only ever dials the local machine",
		origin, request.URL.Host,
	)
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

func (c *Client) RemoveRoom(ctx context.Context, roomID string) (protocol.RemoveResult, error) {
	var result protocol.RemoveResult
	if err := c.doJSON(ctx, http.MethodDelete, "/v1/rooms/"+url.PathEscape(roomID), nil, &result); err != nil {
		return protocol.RemoveResult{}, err
	}
	return result, nil
}

func (c *Client) ApplyConfig(
	ctx context.Context,
	request protocol.ApplyConfigRequest,
) (protocol.ApplyConfigResult, error) {
	var result protocol.ApplyConfigResult
	if err := c.doJSON(ctx, http.MethodPost, "/v1/apply", outboundApplyConfigRequest(request), &result); err != nil {
		return protocol.ApplyConfigResult{}, err
	}
	return result, nil
}

// outboundApplyConfigRequest is intentionally client-local: /v1/apply is the
// authenticated operator-to-server boundary where room credentials must be
// plaintext, while protocol.SecretString remains redacted everywhere else.
func outboundApplyConfigRequest(request protocol.ApplyConfigRequest) applyConfigRequestWire {
	rooms := make([]createRoomRequestWire, 0, len(request.Rooms))
	for _, room := range request.Rooms {
		rooms = append(rooms, createRoomRequestWire{
			ID:          room.ID,
			Name:        room.Name,
			Members:     room.Members,
			Visibility:  room.Visibility,
			WritePolicy: room.WritePolicy,
			Federation:  room.Federation,
			Credential:  room.Credential.Reveal(),
		})
	}
	return applyConfigRequestWire{
		NetworkID: request.NetworkID,
		Rooms:     rooms,
		Agents:    request.Agents,
	}
}

type applyConfigRequestWire struct {
	NetworkID string                       `json:"network_id,omitempty"`
	Rooms     []createRoomRequestWire      `json:"rooms,omitempty"`
	Agents    []protocol.ApplyAgentRequest `json:"agents,omitempty"`
}

type createRoomRequestWire struct {
	ID          string                   `json:"id"`
	Name        string                   `json:"name,omitempty"`
	Members     []string                 `json:"members,omitempty"`
	Visibility  string                   `json:"visibility,omitempty"`
	WritePolicy string                   `json:"write_policy,omitempty"`
	Federation  *protocol.RoomFederation `json:"federation,omitempty"`
	Credential  string                   `json:"credential,omitempty"`
}

func (c *Client) UpdateRoomMembers(
	ctx context.Context,
	roomID string,
	request protocol.UpdateRoomMembersRequest,
) (protocol.Room, error) {
	var room protocol.Room
	path := "/v1/rooms/" + url.PathEscape(roomID) + "/members"
	if err := c.doJSON(ctx, http.MethodPatch, path, request, &room); err != nil {
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

func (c *Client) RemoveAgent(ctx context.Context, agentID string) (protocol.RemoveResult, error) {
	var result protocol.RemoveResult
	if err := c.doJSON(ctx, http.MethodDelete, "/v1/agents/"+url.PathEscape(agentID), nil, &result); err != nil {
		return protocol.RemoveResult{}, err
	}
	return result, nil
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
	decoder := json.NewDecoder(io.LimitReader(response.Body, 1<<20))
	decoder.UseNumber()
	if err := decoder.Decode(out); err != nil {
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
