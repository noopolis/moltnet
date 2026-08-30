package pi

import (
	"context"
	"io"
	"net/http"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestCodecPreservesPiRequestBytes(t *testing.T) {
	codec := NewCodec()
	request, err := codec.EncodeRequest(context.Background(), bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{NetworkID: "local"},
		Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimePi, ControlURL: "http://control.invalid/wake", Token: "pi-secret"},
	}, loop.ControlDelivery{
		Target:        protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"},
		EventID:       "moltnet:msg_1",
		From:          "writer",
		Message:       "[room research] Writer\nhello",
		TransportText: "hello",
	})
	if err != nil {
		t.Fatalf("EncodeRequest() error = %v", err)
	}
	defer request.Body.Close()
	body, err := io.ReadAll(request.Body)
	if err != nil {
		t.Fatalf("read request: %v", err)
	}
	want := `{"context_id":"moltnet:local:room:research","event_id":"moltnet:msg_1","from":"writer","message":"[room research] Writer\nhello","transport_text":"hello","to":"researcher"}`
	if string(body) != want {
		t.Fatalf("Pi request bytes changed:\n got %s\nwant %s", body, want)
	}
	if request.Method != http.MethodPost || request.URL.String() != "http://control.invalid/wake" {
		t.Fatalf("unexpected Pi request %s %s", request.Method, request.URL)
	}
	if request.Header.Get("Content-Type") != "application/json" || request.Header.Get("Authorization") != "Bearer pi-secret" {
		t.Fatalf("unexpected Pi headers %#v", request.Header)
	}
}

func TestCodecPreservesPiResponseBehavior(t *testing.T) {
	codec := NewCodec()
	response := func(status int, body string) *http.Response {
		return &http.Response{StatusCode: status, Status: http.StatusText(status), Body: io.NopCloser(strings.NewReader(body))}
	}
	result, err := codec.DecodeResponse(
		bridgeconfig.Config{Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimePi}},
		loop.ControlDelivery{},
		response(http.StatusOK, `{"from":"researcher","message":"done"}`),
	)
	if err != nil || !result.Publish || result.Text != "done" {
		t.Fatalf("DecodeResponse() = %#v, %v", result, err)
	}

	result, err = codec.DecodeResponse(
		bridgeconfig.Config{},
		loop.ControlDelivery{},
		response(http.StatusOK, `{"from":"researcher","message":"done"}`),
	)
	if err != nil || result.Publish {
		t.Fatalf("non-Pi DecodeResponse() = %#v, %v", result, err)
	}
	if _, err := codec.DecodeResponse(bridgeconfig.Config{}, loop.ControlDelivery{}, response(http.StatusUnauthorized, `{}`)); err == nil {
		t.Fatal("expected Pi status error")
	}
}
