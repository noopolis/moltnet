package client

import (
	"context"
	"net/http"
	"net/url"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func (c *Client) GetDiscussion(ctx context.Context, roomID string) (*protocol.Discussion, error) {
	var result struct {
		Discussion *protocol.Discussion `json:"discussion"`
	}
	err := c.doJSON(ctx, http.MethodGet, "/v1/rooms/"+url.PathEscape(roomID)+"/automation/discussion", nil, &result)
	return result.Discussion, err
}
