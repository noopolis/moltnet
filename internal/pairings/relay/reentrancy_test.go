package relay

import (
	"context"
	"fmt"
	"net/http"
	"sync"
	"testing"
	"time"

	"github.com/gorilla/websocket"
)

func TestInboundReentrantCallKeepsConnectionServing(t *testing.T) {
	outerResponse := make(chan receivedRequest, 1)
	afterResponse := make(chan receivedRequest, 1)
	server := newRelayTestServer(t, func(conn *websocket.Conn) {
		readHello(t, conn, make(chan frameHeader, 1))
		writeInboundTestRequest(t, conn, "outer", "/v1/network")

		nested := readRequest(t, conn)
		if nested.header.Path != "/v1/rooms" {
			t.Errorf("nested request path = %q, want /v1/rooms", nested.header.Path)
			return
		}
		writeTestFrame(t, conn, frameHeader{Type: "res", ID: nested.header.ID, Status: http.StatusOK}, []byte("nested response"))

		outer := readTestFrame(t, conn)
		outerResponse <- outer
		writeInboundTestRequest(t, conn, "after", "/v1/agents")
		afterResponse <- readTestFrame(t, conn)
	})
	defer server.Close()

	var client *Client
	clientReady := make(chan struct{})
	client, runDone := startInboundTestClient(t, server.URL, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/v1/network":
			<-clientReady
			status, body, err := client.Call(request.Context(), http.MethodGet, "/v1/rooms", nil)
			if err != nil || status != http.StatusOK || string(body) != "nested response" {
				http.Error(writer, fmt.Sprintf("nested Call() = (%d, %q, %v)", status, body, err), http.StatusBadGateway)
				return
			}
			_, _ = writer.Write([]byte("outer response"))
		case "/v1/agents":
			_, _ = writer.Write([]byte("after response"))
		default:
			http.NotFound(writer, request)
		}
	}))
	close(clientReady)
	defer stopInboundTestClient(t, client, runDone)

	select {
	case response := <-outerResponse:
		if response.header.Type != "res" || response.header.ID != "outer" || response.header.Status != http.StatusOK || string(response.body) != "outer response" {
			t.Fatalf("outer response = %#v, body %q", response.header, response.body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("reentrant inbound exchange did not complete")
	}

	select {
	case response := <-afterResponse:
		if response.header.Type != "res" || response.header.ID != "after" || response.header.Status != http.StatusOK || string(response.body) != "after response" {
			t.Fatalf("post-reentrancy response = %#v, body %q", response.header, response.body)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("connection did not serve an inbound request after reentrancy")
	}
}

func TestSlowInboundHandlerDoesNotBlockClientCall(t *testing.T) {
	slowStarted := make(chan struct{})
	releaseSlow := make(chan struct{})
	slowResponse := make(chan receivedRequest, 1)
	var releaseOnce sync.Once
	release := func() { releaseOnce.Do(func() { close(releaseSlow) }) }

	server := newRelayTestServer(t, func(conn *websocket.Conn) {
		readHello(t, conn, make(chan frameHeader, 1))
		writeInboundTestRequest(t, conn, "slow", "/v1/network")
		<-slowStarted

		fast := readRequest(t, conn)
		if fast.header.Path != "/v1/rooms" {
			t.Errorf("fast request path = %q, want /v1/rooms", fast.header.Path)
			return
		}
		writeTestFrame(t, conn, frameHeader{Type: "res", ID: fast.header.ID, Status: http.StatusOK}, []byte("fast response"))
		slowResponse <- readTestFrame(t, conn)
	})
	defer server.Close()

	client, runDone := startInboundTestClient(t, server.URL, http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		if request.URL.Path != "/v1/network" {
			http.NotFound(writer, request)
			return
		}
		close(slowStarted)
		select {
		case <-releaseSlow:
			_, _ = writer.Write([]byte("slow response"))
		case <-request.Context().Done():
		}
	}))
	defer func() {
		release()
		stopInboundTestClient(t, client, runDone)
	}()
	select {
	case <-slowStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("slow inbound handler did not start")
	}

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	started := time.Now()
	status, body, err := client.Call(ctx, http.MethodGet, "/v1/rooms", nil)
	elapsed := time.Since(started)
	if err != nil {
		t.Fatalf("Call() error = %v", err)
	}
	if status != http.StatusOK || string(body) != "fast response" {
		t.Fatalf("Call() = (%d, %q), want (200, fast response)", status, body)
	}
	if elapsed >= 750*time.Millisecond {
		t.Fatalf("Call() took %v while slow inbound handler was blocked", elapsed)
	}

	release()
	select {
	case response := <-slowResponse:
		if response.header.Type != "res" || response.header.ID != "slow" || response.header.Status != http.StatusOK || string(response.body) != "slow response" {
			t.Fatalf("slow response = %#v, body %q", response.header, response.body)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("slow inbound handler did not finish after release")
	}
}

func TestCloseCancelsAndWaitsForInboundHandler(t *testing.T) {
	handlerStarted := make(chan struct{})
	handlerCanceled := make(chan struct{})
	server := newRelayTestServer(t, func(conn *websocket.Conn) {
		readHello(t, conn, make(chan frameHeader, 1))
		writeInboundTestRequest(t, conn, "blocking", "/v1/network")
		_, _, _ = conn.ReadMessage() // Close closes the connection once the handler is canceled.
	})
	defer server.Close()

	client, runDone := startInboundTestClient(t, server.URL, http.HandlerFunc(func(_ http.ResponseWriter, request *http.Request) {
		close(handlerStarted)
		<-request.Context().Done()
		close(handlerCanceled)
	}))
	runStopped := false
	defer func() {
		client.Close()
		if !runStopped {
			waitForInboundTestRun(t, runDone)
		}
	}()

	select {
	case <-handlerStarted:
	case <-time.After(2 * time.Second):
		t.Fatal("inbound handler did not start")
	}

	closeDone := make(chan struct{})
	go func() {
		client.Close()
		close(closeDone)
	}()
	select {
	case <-closeDone:
	case <-time.After(2 * time.Second):
		t.Fatal("Close() did not return while inbound handler was blocked")
	}
	select {
	case <-handlerCanceled:
	case <-time.After(time.Second):
		t.Fatal("inbound handler did not observe context cancellation")
	}
	waitForInboundTestRun(t, runDone)
	runStopped = true
}

func startInboundTestClient(t *testing.T, relayURL string, handler http.Handler) (*Client, <-chan error) {
	t.Helper()
	client := NewClient(relayURL, "relay-token", "juan-berlin", WithInboundHandler(handler))
	runDone := make(chan error, 1)
	go func() { runDone <- client.Run(context.Background()) }()
	return client, runDone
}

func stopInboundTestClient(t *testing.T, client *Client, runDone <-chan error) {
	t.Helper()
	client.Close()
	waitForInboundTestRun(t, runDone)
}

func waitForInboundTestRun(t *testing.T, runDone <-chan error) {
	t.Helper()
	select {
	case err := <-runDone:
		if err != nil {
			t.Errorf("Run() error = %v", err)
		}
	case <-time.After(2 * time.Second):
		t.Error("Run() did not stop after Close()")
	}
}

func writeInboundTestRequest(t *testing.T, conn *websocket.Conn, id, path string) {
	t.Helper()
	writeTestFrame(t, conn, frameHeader{Type: "req", ID: id, Method: http.MethodGet, Path: path}, nil)
}

func writeTestFrame(t *testing.T, conn *websocket.Conn, header frameHeader, body []byte) {
	t.Helper()
	frame, err := makeFrame(header, body)
	if err != nil {
		t.Errorf("make relay frame: %v", err)
		return
	}
	if err := conn.WriteMessage(websocket.BinaryMessage, frame); err != nil {
		t.Errorf("write relay frame: %v", err)
	}
}

func readTestFrame(t *testing.T, conn *websocket.Conn) receivedRequest {
	t.Helper()
	_, raw, err := conn.ReadMessage()
	if err != nil {
		t.Errorf("read relay frame: %v", err)
		return receivedRequest{}
	}
	header, body, err := parseFrame(raw)
	if err != nil {
		t.Errorf("parse relay frame: %v", err)
		return receivedRequest{}
	}
	return receivedRequest{header: header, body: body}
}
