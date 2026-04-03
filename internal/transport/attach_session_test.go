package transport

import (
	"context"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestAttachmentSessionTracksAckedCursor(t *testing.T) {
	t.Parallel()

	session := newAttachmentSession(" evt_prev ")
	if got := session.ResumeCursor(); got != "evt_prev" {
		t.Fatalf("unexpected initial resume cursor %q", got)
	}

	session.NoteSent(" evt_1 ")
	if !session.Ack(" evt_1 ") {
		t.Fatal("expected ack to succeed")
	}
	if got := session.ResumeCursor(); got != "evt_1" {
		t.Fatalf("unexpected resume cursor %q", got)
	}
	if session.Ack("evt_1") {
		t.Fatal("expected duplicate ack to fail")
	}
	if session.Ack("") {
		t.Fatal("expected blank ack to fail")
	}
}

func TestAttachmentSessionEvictsOldestPendingCursors(t *testing.T) {
	t.Parallel()

	session := newAttachmentSession("")
	total := maxPendingAttachmentAcks + 5
	for index := 0; index < total; index++ {
		session.NoteSent(fmt.Sprintf("evt_%d", index))
	}

	if len(session.pending) != maxPendingAttachmentAcks {
		t.Fatalf("unexpected pending size %d", len(session.pending))
	}
	if got := session.ResumeCursor(); got != "evt_4" {
		t.Fatalf("unexpected resume cursor %q", got)
	}
	for index := 0; index < 5; index++ {
		cursor := fmt.Sprintf("evt_%d", index)
		if session.Ack(cursor) {
			t.Fatalf("expected evicted cursor %q to be unacked", cursor)
		}
	}
	if !session.Ack("evt_5") {
		t.Fatal("expected retained cursor ack to succeed")
	}
}

func TestAttachmentSessionAckTrimsOrderHistory(t *testing.T) {
	t.Parallel()

	session := newAttachmentSession("")
	for index := 0; index < maxPendingAttachmentAcks*2; index++ {
		cursor := fmt.Sprintf("evt_%d", index)
		session.NoteSent(cursor)
		if !session.Ack(cursor) {
			t.Fatalf("expected ack for %q to succeed", cursor)
		}
	}

	if len(session.pending) != 0 {
		t.Fatalf("expected no pending cursors, got %d", len(session.pending))
	}
	if len(session.order) != 0 {
		t.Fatalf("expected order history to be trimmed, got %d", len(session.order))
	}
	if got := session.ResumeCursor(); got != fmt.Sprintf("evt_%d", maxPendingAttachmentAcks*2-1) {
		t.Fatalf("unexpected resume cursor %q", got)
	}
}

func TestConsumeAttachmentFramesHandlesAckPingAndUnexpectedOp(t *testing.T) {
	t.Parallel()

	serverConnCh := make(chan *websocket.Conn, 1)
	upgrader := websocket.Upgrader{CheckOrigin: func(*http.Request) bool { return true }}
	server := httptest.NewServer(http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		connection, err := upgrader.Upgrade(response, request, nil)
		if err != nil {
			t.Fatalf("upgrade websocket: %v", err)
		}
		serverConnCh <- connection
	}))
	defer server.Close()

	endpoint := "ws" + server.URL[len("http"):] + "/v1/attach"
	clientConn, _, err := websocket.DefaultDialer.Dial(endpoint, nil)
	if err != nil {
		t.Fatalf("dial websocket: %v", err)
	}
	defer clientConn.Close()

	serverConn := <-serverConnCh
	defer serverConn.Close()

	session := newAttachmentSession("")
	session.NoteSent("evt_1")
	writer := &attachmentWriter{connection: serverConn}

	errCh := make(chan error, 1)
	go func() {
		errCh <- consumeAttachmentFrames(context.Background(), serverConn, writer, session, time.Second)
	}()

	if err := clientConn.WriteJSON(protocol.AttachmentFrame{
		Op:      protocol.AttachmentOpAck,
		Version: protocol.AttachmentProtocolV1,
		Cursor:  "evt_1",
	}); err != nil {
		t.Fatalf("write ack: %v", err)
	}

	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		if got := session.ResumeCursor(); got == "evt_1" {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if got := session.ResumeCursor(); got != "evt_1" {
		t.Fatalf("unexpected resume cursor %q", got)
	}

	if err := clientConn.WriteJSON(protocol.AttachmentFrame{
		Op:      protocol.AttachmentOpPing,
		Version: protocol.AttachmentProtocolV1,
	}); err != nil {
		t.Fatalf("write ping: %v", err)
	}

	var pong protocol.AttachmentFrame
	if err := clientConn.ReadJSON(&pong); err != nil {
		t.Fatalf("read pong: %v", err)
	}
	if pong.Op != protocol.AttachmentOpPong {
		t.Fatalf("unexpected pong %#v", pong)
	}

	if err := clientConn.WriteJSON(protocol.AttachmentFrame{
		Op:      "unexpected",
		Version: protocol.AttachmentProtocolV1,
	}); err != nil {
		t.Fatalf("write invalid op: %v", err)
	}

	var errorFrame protocol.AttachmentFrame
	if err := clientConn.ReadJSON(&errorFrame); err != nil {
		t.Fatalf("read error frame: %v", err)
	}
	if errorFrame.Op != protocol.AttachmentOpError {
		t.Fatalf("unexpected error frame %#v", errorFrame)
	}

	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("expected consumeAttachmentFrames error")
		}
	case <-time.After(time.Second):
		t.Fatal("timed out waiting for frame loop to exit")
	}
}

func TestAttachmentFrameErrorHelpers(t *testing.T) {
	t.Parallel()

	root := errors.New("root cause")
	err := invalidAttachmentFrameError(" malformed frame ", root)

	var frameErr *attachmentFrameError
	if !errors.As(err, &frameErr) {
		t.Fatalf("expected typed frame error, got %v", err)
	}
	if frameErr.Error() != "malformed frame" {
		t.Fatalf("unexpected frame error message %q", frameErr.Error())
	}
	if !errors.Is(frameErr, root) {
		t.Fatalf("expected wrapped root error, got %v", frameErr)
	}
	if message, ok := attachmentFrameErrorMessage(err); !ok || message != "malformed frame" {
		t.Fatalf("unexpected frame error message result message=%q ok=%v", message, ok)
	}
	if message, ok := attachmentFrameErrorMessage(errors.New("plain")); ok || message != "" {
		t.Fatalf("expected plain errors to be ignored, got message=%q ok=%v", message, ok)
	}
}
