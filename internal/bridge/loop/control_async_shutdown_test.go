package loop

import (
	"context"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// asyncShutdownCodec models what the Daimon codec does: StartControlAsync
// spawns a follower bound to the loop's context, and that follower still has
// durable work to finish after cancellation (writing its receipt store).
type asyncShutdownCodec struct {
	legacyControlCodec
	done     chan struct{}
	finished atomic.Bool
}

func (c *asyncShutdownCodec) StartControlAsync(ctx context.Context, _ *MoltnetClient, _ bridgeconfig.Config) error {
	c.done = make(chan struct{})
	go func() {
		defer close(c.done)
		<-ctx.Done()
		// The window the flake lived in: cancellation is not completion, and a
		// follower that is mid-write keeps writing after the loop is told to stop.
		time.Sleep(20 * time.Millisecond)
		c.finished.Store(true)
	}()
	return nil
}

func (c *asyncShutdownCodec) ControlAccepted(bridgeconfig.Config, protocol.Event, ControlAcceptance) error {
	return nil
}

func (c *asyncShutdownCodec) WaitControlAsync() {
	if c.done == nil {
		return
	}
	<-c.done
}

// RunControlLoopWithCodec returning must mean every goroutine it started has
// returned. Without the barrier the loop cancels the follower and returns
// immediately, so a caller has no safe point to tear down the state that
// follower is still writing — which is how `t.TempDir()` cleanup started
// racing the Daimon receipt store.
func TestRunControlLoopWaitsForAsyncCodecBeforeReturning(t *testing.T) {
	t.Parallel()

	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	moltnetServer := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/attach" {
			response.WriteHeader(http.StatusNotFound)
			return
		}
		connection, err := upgrader.Upgrade(response, request, nil)
		if err != nil {
			t.Errorf("upgrade websocket: %v", err)
			return
		}
		defer connection.Close()
		writeAttachmentHandshake(t, connection, "researcher")
		_ = connection.WriteControl(
			websocket.CloseMessage,
			websocket.FormatCloseMessage(websocket.CloseNormalClosure, "done"),
			time.Now().Add(time.Second),
		)
	}))
	defer moltnetServer.Close()

	config := bridgeconfig.Config{
		Agent:   bridgeconfig.AgentConfig{ID: "researcher", Name: "Researcher"},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: moltnetServer.URL, NetworkID: "local"},
		Runtime: bridgeconfig.RuntimeConfig{Kind: bridgeconfig.RuntimePi, ControlURL: moltnetServer.URL},
		Rooms:   []bridgeconfig.RoomBinding{{ID: "research", Wake: bridgeconfig.WakeMentions}},
	}

	codec := &asyncShutdownCodec{}
	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(50 * time.Millisecond)
		cancel()
	}()

	if err := RunControlLoopWithCodec(ctx, config, codec); err != nil {
		t.Fatalf("RunControlLoopWithCodec() error = %v", err)
	}

	if !codec.finished.Load() {
		t.Fatal("RunControlLoopWithCodec returned while its async codec was still working")
	}
}
