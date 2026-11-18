package acp

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/gorilla/websocket"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestACPAttachmentDeliversRoomsAndDMsWithoutPublishing(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	config, trace := fixtureConfig(t, "attachment", "")
	config.Rooms = []bridgeconfig.RoomBinding{{ID: "research", Wake: bridgeconfig.WakeMentions}}
	config.DMs = &bridgeconfig.DMConfig{Enabled: true}

	room := protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "research"}
	dm := protocol.Target{Kind: protocol.TargetKindDM, DMID: "direct", ParticipantIDs: []string{"writer", config.Agent.ID}}
	mention := protocol.AgentFQID("local", config.Agent.ID)
	events := []*protocol.Message{
		message("msg_skip", room, "no mention, no wake"),
		message("msg_room1", room, "first room line\nsecond room line", mention),
		message("msg_dm", dm, "private direct message"),
		message("msg_room2", room, "room follow-up", mention),
	}

	var publishes atomic.Int32
	attachResult := make(chan error, 1)
	upgrader := websocket.Upgrader{}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method == http.MethodPost {
			publishes.Add(1)
		}
		if r.URL.Path != "/v1/attach" {
			http.NotFound(w, r)
			return
		}
		conn, err := upgrader.Upgrade(w, r, nil)
		if err == nil {
			defer conn.Close()
			err = serveEvents(conn, config.Agent.ID, events)
		}
		select {
		case attachResult <- err:
		default:
		}
	}))
	defer server.Close()
	config.Moltnet.BaseURL = server.URL

	if err := New().Run(ctx, config); err != nil {
		t.Fatalf("run ACP attachment: %v", err)
	}
	select {
	case err := <-attachResult:
		if err != nil {
			t.Fatal(err)
		}
	case <-ctx.Done():
		t.Fatal("attachment handshake never completed")
	}
	if count := publishes.Load(); count != 0 {
		t.Fatalf("ACP output was published %d times", count)
	}

	var initializations, newSessions, prompts, completed int
	var roomSession, dmSession string
	observed := readFixtureTrace(t, trace)
	for _, item := range observed {
		switch item.Method {
		case "initialize":
			initializations++
		case "session/new":
			newSessions++
		case "prompt/complete":
			completed++
		case "session/prompt":
			prompts++
			if strings.Contains(item.Text, "no mention, no wake") || strings.Contains(item.Text, privateOutput) {
				t.Fatalf("unexpected prompt %q", item.Text)
			}
			if strings.Contains(item.Text, "first room line\nsecond room line") && strings.Contains(item.Text, "Message ID: msg_room1") &&
				strings.Contains(item.Text, "Chat ID: local:room:research") {
				roomSession = item.SessionID
			}
			if strings.Contains(item.Text, "private direct message") && strings.Contains(item.Text, "Message ID: msg_dm") &&
				strings.Contains(item.Text, "Chat ID: local:dm:direct") {
				dmSession = item.SessionID
			}
		}
	}
	if initializations != 1 || newSessions != 2 || roomSession == "" || dmSession == "" || roomSession == dmSession {
		t.Fatalf("initializations=%d new sessions=%d room=%q dm=%q", initializations, newSessions, roomSession, dmSession)
	}
	if completed != prompts {
		t.Fatalf("fixture did not complete all prompts: %d of %d", completed, prompts)
	}
	if !slices.ContainsFunc(observed, func(item fixtureEvent) bool {
		return item.SessionID == roomSession && strings.Contains(item.Text, "Message ID: msg_room2") && strings.Contains(item.Text, "room follow-up")
	}) {
		t.Fatal("room follow-up did not reuse the room session")
	}
	workspaceConfig, err := clientconfig.LoadFile(clientconfig.DefaultPathForWorkspace(config.Runtime.WorkspacePath))
	if err != nil {
		t.Fatal(err)
	}
	attachment, err := workspaceConfig.ResolveAttachmentFor("local", config.Agent.ID)
	if err != nil || attachment.Runtime != "acp" || attachment.BaseURL != server.URL {
		t.Fatalf("wrong ACP workspace client config: %+v, err=%v", attachment, err)
	}
}

func message(id string, target protocol.Target, text string, mentions ...string) *protocol.Message {
	return &protocol.Message{
		ID: id, NetworkID: "local", Target: target,
		From:     protocol.Actor{Type: "agent", ID: "writer"},
		Parts:    []protocol.Part{{Kind: protocol.PartKindText, Text: text}},
		Mentions: mentions,
	}
}

func serveEvents(conn *websocket.Conn, agentID string, messages []*protocol.Message) error {
	if err := conn.SetReadDeadline(time.Now().Add(15 * time.Second)); err != nil {
		return err
	}
	write := func(frame protocol.AttachmentFrame) error {
		frame.Version = protocol.AttachmentProtocolV1
		return conn.WriteJSON(frame)
	}
	if err := write(protocol.AttachmentFrame{Op: protocol.AttachmentOpHello}); err != nil {
		return err
	}
	var frame protocol.AttachmentFrame
	if err := conn.ReadJSON(&frame); err != nil {
		return err
	}
	if frame.Op != protocol.AttachmentOpIdentify || frame.Agent == nil || frame.Agent.ID != agentID {
		return fmt.Errorf("unexpected identify frame: %+v", frame)
	}
	if err := write(protocol.AttachmentFrame{Op: protocol.AttachmentOpReady, NetworkID: "local", AgentID: agentID}); err != nil {
		return err
	}
	for _, msg := range messages {
		event := &protocol.Event{ID: "event_" + msg.ID, NetworkID: "local", Type: protocol.EventTypeMessageCreated, Message: msg}
		if err := write(protocol.AttachmentFrame{Op: protocol.AttachmentOpEvent, Cursor: event.ID, Event: event}); err != nil {
			return err
		}
		if err := conn.ReadJSON(&frame); err != nil {
			return err
		}
		if frame.Op != protocol.AttachmentOpAck || frame.Cursor != event.ID {
			return fmt.Errorf("unexpected acknowledgement: %+v", frame)
		}
	}
	return conn.WriteControl(websocket.CloseMessage, websocket.FormatCloseMessage(websocket.CloseNormalClosure, ""), time.Now().Add(time.Second))
}
