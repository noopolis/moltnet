package app

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestAppCloseWaitsForRelayRunners(t *testing.T) {
	runnerStarted := make(chan struct{})
	releaseRunner := make(chan struct{})
	closer := &relayRunnerCloseNotifier{closed: make(chan struct{})}
	instance := &App{closers: []io.Closer{closer}}
	instance.relayWG.Add(1)
	go func() {
		close(runnerStarted)
		<-releaseRunner
		instance.relayWG.Done()
	}()
	<-runnerStarted

	closed := make(chan struct{})
	go func() {
		instance.close()
		close(closed)
	}()

	select {
	case <-closer.closed:
		t.Fatal("closed dependent resource before relay runner finished")
	case <-time.After(25 * time.Millisecond):
	}

	close(releaseRunner)
	select {
	case <-closed:
	case <-time.After(time.Second):
		t.Fatal("App.close() did not wait for relay runner")
	}
}

func TestAppRelayInboundUsesFrameAuthorization(t *testing.T) {
	for _, test := range []struct {
		name      string
		frameAuth string
		want      int
	}{
		{name: "missing originator credential", want: http.StatusUnauthorized},
		{name: "valid originator credential", frameAuth: "originator-pairing-token", want: http.StatusOK},
	} {
		t.Run(test.name, func(t *testing.T) {
			relayServer, responses, serverErrors := newAppRelayTestServer(t, "relay-connect-token", test.frameAuth)
			defer relayServer.Close()

			instance, err := New(Config{
				Auth: authn.Config{
					Mode: authn.ModeBearer,
					Tokens: []authn.TokenConfig{{
						ID:     "pairing",
						Value:  "originator-pairing-token",
						Scopes: []authn.Scope{authn.ScopePair},
					}},
				},
				ListenAddr:  "127.0.0.1:0",
				NetworkID:   "local",
				NetworkName: "Local",
				Version:     "test",
				Pairings: []protocol.Pairing{{
					ID:    "relay-remote",
					Token: "receiver-pairing-token",
					Relay: &protocol.PairingRelay{URL: relayServer.URL, Room: "test", Token: "relay-connect-token"},
				}},
			})
			if err != nil {
				t.Fatalf("New() error = %v", err)
			}

			directRequest := httptest.NewRequest(http.MethodGet, "/v1/network", nil)
			if test.frameAuth != "" {
				directRequest.Header.Set("Authorization", "Bearer "+test.frameAuth)
			}
			directResponse := httptest.NewRecorder()
			instance.Handler().ServeHTTP(directResponse, directRequest)

			runCtx, cancel := context.WithCancel(context.Background())
			runDone := make(chan error, 1)
			go func() { runDone <- instance.Run(runCtx) }()
			stopped := false
			t.Cleanup(func() {
				if stopped {
					return
				}
				cancel()
				select {
				case err := <-runDone:
					if err != nil {
						t.Errorf("Run() error = %v", err)
					}
				case <-time.After(time.Second):
					t.Error("Run() did not stop")
				}
			})

			var relayResponse appRelayFrame
			select {
			case relayResponse = <-responses:
			case err := <-serverErrors:
				t.Fatal(err)
			case <-time.After(time.Second):
				t.Fatal("did not receive relay response")
			}

			if relayResponse.Type != "res" || relayResponse.ID != "network" {
				t.Fatalf("relay response = %#v, want response for network", relayResponse)
			}
			if relayResponse.Status != test.want {
				t.Fatalf("relay status = %d, want %d", relayResponse.Status, test.want)
			}
			if relayResponse.Status != directResponse.Code {
				t.Fatalf("relay status = %d, direct status = %d", relayResponse.Status, directResponse.Code)
			}
			relayBody := normalizedAppRelayResponseBody(t, relayResponse.Body)
			directBody := normalizedAppRelayResponseBody(t, directResponse.Body.Bytes())
			if !reflect.DeepEqual(relayBody, directBody) {
				t.Fatalf("relay body = %#v, direct body = %#v", relayBody, directBody)
			}

			cancel()
			select {
			case err := <-runDone:
				stopped = true
				if err != nil {
					t.Fatalf("Run() error = %v", err)
				}
			case <-time.After(time.Second):
				t.Fatal("Run() did not stop")
			}
		})
	}
}

func normalizedAppRelayResponseBody(t *testing.T, body []byte) map[string]any {
	t.Helper()

	var decoded map[string]any
	if err := json.Unmarshal(body, &decoded); err != nil {
		t.Fatalf("decode response body %q: %v", body, err)
	}
	delete(decoded, "request_id")
	return decoded
}

type relayRunnerCloseNotifier struct {
	closed chan struct{}
}

func (c *relayRunnerCloseNotifier) Close() error {
	close(c.closed)
	return nil
}

type appRelayFrame struct {
	Type   string `json:"t"`
	ID     string `json:"id,omitempty"`
	Auth   string `json:"auth,omitempty"`
	Method string `json:"method,omitempty"`
	Path   string `json:"path,omitempty"`
	Status int    `json:"status,omitempty"`
	Body   []byte `json:"-"`
}

func newAppRelayTestServer(
	t *testing.T,
	relayToken string,
	frameAuth string,
) (*httptest.Server, <-chan appRelayFrame, <-chan error) {
	t.Helper()
	responses := make(chan appRelayFrame, 1)
	errors := make(chan error, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		wantAuthorization := ""
		if relayToken != "" {
			wantAuthorization = "Bearer " + relayToken
		}
		if got := request.Header.Get("Authorization"); got != wantAuthorization {
			sendAppRelayServerError(errors, fmt.Errorf("relay authorization = %q, want %q", got, wantAuthorization))
			http.Error(writer, "unexpected authorization", http.StatusUnauthorized)
			return
		}

		conn, err := upgrader.Upgrade(writer, request, nil)
		if err != nil {
			sendAppRelayServerError(errors, fmt.Errorf("upgrade relay connection: %w", err))
			return
		}
		defer conn.Close()

		if _, _, err := conn.ReadMessage(); err != nil {
			sendAppRelayServerError(errors, fmt.Errorf("read relay hello: %w", err))
			return
		}
		requestFrame, err := marshalAppRelayFrame(appRelayFrame{
			Type: "req", ID: "network", Auth: frameAuth, Method: http.MethodGet, Path: "/v1/network",
		})
		if err != nil {
			sendAppRelayServerError(errors, err)
			return
		}
		if err := conn.WriteMessage(websocket.BinaryMessage, requestFrame); err != nil {
			sendAppRelayServerError(errors, fmt.Errorf("write relay request: %w", err))
			return
		}
		_, rawResponse, err := conn.ReadMessage()
		if err != nil {
			sendAppRelayServerError(errors, fmt.Errorf("read relay response: %w", err))
			return
		}
		response, err := parseAppRelayFrame(rawResponse)
		if err != nil {
			sendAppRelayServerError(errors, err)
			return
		}
		responses <- response
	}))
	server.URL = "ws" + strings.TrimPrefix(server.URL, "http")
	return server, responses, errors
}

func marshalAppRelayFrame(frame appRelayFrame) ([]byte, error) {
	header, err := json.Marshal(frame)
	if err != nil {
		return nil, fmt.Errorf("marshal relay frame: %w", err)
	}
	if len(frame.Body) == 0 {
		return header, nil
	}
	return append(append(header, '\n'), frame.Body...), nil
}

func parseAppRelayFrame(raw []byte) (appRelayFrame, error) {
	header, body, found := bytes.Cut(raw, []byte{'\n'})
	if !found {
		header = raw
	}
	var frame appRelayFrame
	if err := json.Unmarshal(header, &frame); err != nil {
		return appRelayFrame{}, fmt.Errorf("parse relay frame: %w", err)
	}
	frame.Body = body
	return frame, nil
}

func sendAppRelayServerError(errors chan<- error, err error) {
	select {
	case errors <- err:
	default:
	}
}
