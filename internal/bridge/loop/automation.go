package loop

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/url"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func (c *MoltnetClient) automationAllowed(ctx context.Context, message *protocol.Message, id string) (bool, error) {
	if message == nil || message.Wake == nil {
		return true, nil
	}
	query := url.Values{"agent_id": {id}, "message_id": {message.ID}}
	path := c.baseURL + "/v1/rooms/" + url.PathEscape(message.Target.RoomID) + "/automation/wake?" + query.Encode()
	r, err := http.NewRequestWithContext(ctx, http.MethodGet, path, nil)
	if err != nil {
		return false, err
	}
	if c.token != "" {
		r.Header.Set("Authorization", "Bearer "+c.token)
	}
	response, err := c.client.Do(r)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return false, fmt.Errorf("automation wake check returned HTTP %d", response.StatusCode)
	}
	var result struct {
		Allowed bool `json:"allowed"`
	}
	err = json.NewDecoder(response.Body).Decode(&result)
	return result.Allowed, err
}
