package pairings

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

const pairingRoomCredential = "pairing-room-join-secret"

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestFetchRoomsDropsCredentialShapedResponseData(t *testing.T) {
	client := NewClient()
	client.http.http = &http.Client{Transport: roundTripFunc(func(request *http.Request) (*http.Response, error) {
		if request.URL.Path != "/v1/rooms" {
			t.Fatalf("request path = %q", request.URL.Path)
		}
		return &http.Response{
			StatusCode: http.StatusOK,
			Header:     make(http.Header),
			Body:       io.NopCloser(strings.NewReader(`{"rooms":[{"id":"research","credential":"pairing-room-join-secret"}]}`)),
		}, nil
	})}

	rooms, err := client.FetchRooms(context.Background(), protocol.Pairing{RemoteBaseURL: "https://remote.example"})
	if err != nil {
		t.Fatal(err)
	}
	payload, err := json.Marshal(rooms)
	if err != nil {
		t.Fatal(err)
	}
	if bytes.Contains(payload, []byte(pairingRoomCredential)) {
		t.Fatalf("credential leaked in FetchRooms result %s", payload)
	}
}
