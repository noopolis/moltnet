package rooms

import (
	"context"
	"testing"
	"time"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/pkg/protocol"
)

func privateVisibilityContext(agent string, scopes ...authn.Scope) context.Context {
	claims := authn.NewStaticClaims(authn.TokenConfig{ID: "restricted", Scopes: scopes, Agents: []string{agent}})
	return authn.WithMode(authn.WithClaims(context.Background(), claims), authn.ModeBearer)
}

func TestRestrictedStaticMemberRoomAccessReportsWriteAuthority(t *testing.T) {
	service := newTestService()
	if _, err := service.CreateRoom(protocol.CreateRoomRequest{
		ID:          "team",
		Members:     []string{"world"},
		Visibility:  protocol.RoomVisibilityPrivate,
		WritePolicy: protocol.RoomWritePolicyMembers,
	}); err != nil {
		t.Fatal(err)
	}

	for name, test := range map[string]struct {
		ctx  context.Context
		want bool
	}{
		"single member with write scope": {
			ctx:  privateVisibilityContext("world", authn.ScopeObserve, authn.ScopeWrite),
			want: true,
		},
		"single member without write scope": {
			ctx:  privateVisibilityContext("world", authn.ScopeObserve),
			want: false,
		},
		"ambiguous multi-agent token": {
			ctx: authn.WithMode(authn.WithClaims(context.Background(), authn.NewStaticClaims(authn.TokenConfig{
				ID:     "shared",
				Scopes: []authn.Scope{authn.ScopeObserve, authn.ScopeWrite},
				Agents: []string{"world", "other"},
			})), authn.ModeBearer),
			want: false,
		},
	} {
		t.Run(name, func(t *testing.T) {
			room, err := service.GetRoomContext(test.ctx, "team")
			if err != nil {
				t.Fatal(err)
			}
			if room.Access == nil || room.Access.CanWrite != test.want {
				t.Fatalf("room access = %#v, want can_write=%v", room.Access, test.want)
			}
		})
	}
}

func TestRestrictedVisibilityCoversRoomsMessagesThreadsAndDMs(t *testing.T) {
	service := newTestService()
	for _, room := range []protocol.CreateRoomRequest{
		{ID: "public", Visibility: protocol.RoomVisibilityPublic},
		{ID: "own", Members: []string{"luna"}, Visibility: protocol.RoomVisibilityPrivate},
		{ID: "other", Members: []string{"atlas"}, Visibility: protocol.RoomVisibilityPrivate},
	} {
		if _, err := service.CreateRoom(room); err != nil {
			t.Fatal(err)
		}
	}
	ctx := privateVisibilityContext("luna", authn.ScopeObserve)
	admin := privateVisibilityContext("luna", authn.ScopeAdmin)
	bareRestricted := privateVisibilityContext("luna")
	operator := authn.WithMode(authn.WithClaims(context.Background(), authn.NewStaticClaims(authn.TokenConfig{ID: "operator", Scopes: []authn.Scope{authn.ScopeAdmin}})), authn.ModeBearer)
	for name, candidate := range map[string]context.Context{"observe": ctx, "admin": admin, "agent-token": bareRestricted} {
		page, err := service.ListRoomsContext(candidate, protocol.PageRequest{Limit: 2})
		if err != nil || len(page.Rooms) != 2 || !containsRoomIDs(page.Rooms, "public", "own") {
			t.Fatalf("%s room list = %#v, %v", name, page.Rooms, err)
		}
		if _, err := service.GetRoomContext(candidate, "own"); err != nil {
			t.Fatalf("%s own room get: %v", name, err)
		}
		if _, err := service.GetRoomContext(candidate, "other"); err == nil {
			t.Fatalf("%s read unrelated room", name)
		}
	}
	all, err := service.ListRoomsContext(operator, protocol.PageRequest{Limit: 10})
	if err != nil || len(all.Rooms) != 3 {
		t.Fatalf("unrestricted operator rooms = %#v, %v", all.Rooms, err)
	}
	if _, err := service.ListRoomsContext(context.Background(), protocol.PageRequest{Limit: 10}); err != nil {
		t.Fatalf("anonymous compatibility list: %v", err)
	}
	for _, message := range []protocol.SendMessageRequest{
		{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "own"}, From: protocol.Actor{Type: "agent", ID: "luna"}, Parts: []protocol.Part{{Kind: protocol.PartKindFile, Filename: "own-room.txt"}}},
		{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "other"}, From: protocol.Actor{Type: "agent", ID: "atlas"}, Parts: []protocol.Part{{Kind: protocol.PartKindFile, Filename: "other-room.txt"}}},
		{Target: protocol.Target{Kind: protocol.TargetKindThread, RoomID: "own", ThreadID: "own-thread", ParentMessageID: "parent-own"}, From: protocol.Actor{Type: "agent", ID: "luna"}, Parts: []protocol.Part{{Kind: protocol.PartKindFile, Filename: "own-thread.txt"}}},
		{Target: protocol.Target{Kind: protocol.TargetKindThread, RoomID: "other", ThreadID: "other-thread", ParentMessageID: "parent-other"}, From: protocol.Actor{Type: "agent", ID: "atlas"}, Parts: []protocol.Part{{Kind: protocol.PartKindFile, Filename: "other-thread.txt"}}},
		{Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm-own", ParticipantIDs: []string{"luna", "mira"}}, From: protocol.Actor{Type: "agent", ID: "luna"}, Parts: []protocol.Part{{Kind: protocol.PartKindFile, Filename: "own-dm.txt"}}},
		{Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm-other", ParticipantIDs: []string{"atlas", "mira"}}, From: protocol.Actor{Type: "agent", ID: "atlas"}, Parts: []protocol.Part{{Kind: protocol.PartKindFile, Filename: "other-dm.txt"}}},
	} {
		if _, err := service.SendMessage(message); err != nil {
			t.Fatal(err)
		}
	}
	operatorDMs, err := service.ListDirectConversationsContext(operator, protocol.PageRequest{Limit: 10})
	if err != nil || len(operatorDMs.DMs) != 2 {
		t.Fatalf("unrestricted operator DM list = %#v, %v", operatorDMs.DMs, err)
	}
	if _, err := service.GetDirectConversationContext(operator, "dm-own"); err != nil {
		t.Fatalf("unrestricted operator DM get: %v", err)
	}
	if page, err := service.ListRoomMessagesContext(ctx, "own", protocol.PageRequest{Limit: 10}); err != nil || len(page.Messages) != 1 {
		t.Fatalf("own room messages = %#v, %v", page, err)
	}
	if _, err := service.ListRoomMessagesContext(ctx, "other", protocol.PageRequest{}); err == nil {
		t.Fatal("unrelated room messages were readable")
	}
	if page, err := service.ListThreadsContext(ctx, "own", protocol.PageRequest{Limit: 10}); err != nil || len(page.Threads) != 1 {
		t.Fatalf("own threads = %#v, %v", page, err)
	}
	if _, err := service.ListThreadsContext(ctx, "other", protocol.PageRequest{}); err == nil {
		t.Fatal("unrelated thread list was readable")
	}
	if thread, err := service.GetThreadContext(ctx, "own-thread"); err != nil || thread.RoomID != "own" {
		t.Fatalf("own thread get = %#v, %v", thread, err)
	}
	if _, err := service.GetThreadContext(ctx, "other-thread"); err == nil {
		t.Fatal("unrelated thread get was readable")
	}
	if page, err := service.ListThreadMessagesContext(ctx, "own-thread", protocol.PageRequest{}); err != nil || len(page.Messages) != 1 {
		t.Fatalf("own thread messages = %#v, %v", page, err)
	}
	if _, err := service.ListThreadMessagesContext(ctx, "other-thread", protocol.PageRequest{}); err == nil {
		t.Fatal("unrelated thread messages were readable")
	}
	dms, err := service.ListDirectConversationsContext(ctx, protocol.PageRequest{Limit: 1})
	if err != nil || len(dms.DMs) != 1 || dms.DMs[0].ID != "dm-own" {
		t.Fatalf("DMs were not filtered before pagination: %#v, %v", dms.DMs, err)
	}
	if _, err := service.GetDirectConversationContext(ctx, "dm-own"); err != nil {
		t.Fatalf("own DM get: %v", err)
	}
	if _, err := service.GetDirectConversationContext(ctx, "dm-other"); err == nil {
		t.Fatal("unrelated DM get was readable")
	}
	if _, err := service.ListDMMessagesContext(ctx, "dm-other", protocol.PageRequest{}); err == nil {
		t.Fatal("unrelated DM messages were readable")
	}
	if page, err := service.ListDMMessagesContext(ctx, "dm-own", protocol.PageRequest{}); err != nil || len(page.Messages) != 1 {
		t.Fatalf("own DM messages = %#v, %v", page, err)
	}
	if page, err := service.ListDMMessagesContext(operator, "dm-other", protocol.PageRequest{}); err != nil || len(page.Messages) != 1 {
		t.Fatalf("unrestricted operator DM messages = %#v, %v", page, err)
	}

	for name, filter := range map[string]protocol.ArtifactFilter{
		"room": {RoomID: "own"}, "thread": {ThreadID: "own-thread"}, "dm": {DMID: "dm-own"},
	} {
		page, err := service.ListArtifactsContext(ctx, filter, protocol.PageRequest{Limit: 10})
		if err != nil || len(page.Artifacts) == 0 {
			t.Fatalf("visible %s artifacts = %#v, %v", name, page, err)
		}
		for _, artifact := range page.Artifacts {
			if artifact.Filename == "" || (artifact.Target.Kind == protocol.TargetKindRoom && artifact.Target.RoomID != "own") || (artifact.Target.Kind == protocol.TargetKindThread && artifact.Target.ThreadID != "own-thread") || (artifact.Target.Kind == protocol.TargetKindDM && artifact.Target.DMID != "dm-own") {
				t.Fatalf("visible %s artifact escaped its parent: %#v", name, artifact)
			}
		}
	}
	for _, filter := range []protocol.ArtifactFilter{{RoomID: "other"}, {ThreadID: "other-thread"}, {DMID: "dm-other"}, {RoomID: "own", ThreadID: "other-thread"}} {
		if _, err := service.ListArtifactsContext(ctx, filter, protocol.PageRequest{}); err == nil {
			t.Fatalf("unrelated/mixed artifact scope accepted: %#v", filter)
		}
	}
	for _, filter := range []protocol.ArtifactFilter{{RoomID: "own", ThreadID: "own-thread"}, {RoomID: "own", DMID: "dm-own"}, {ThreadID: "own-thread", DMID: "dm-own"}} {
		if _, err := service.ListArtifactsContext(ctx, filter, protocol.PageRequest{}); err == nil {
			t.Fatalf("mixed readable artifact scope accepted: %#v", filter)
		}
	}
}

func TestRestrictedIdentityAndEventParentsUseNetworkQualifiedAuthorities(t *testing.T) {
	service := newTestService()
	for _, room := range []protocol.CreateRoomRequest{
		{ID: "local-room", Members: []string{"luna"}, Visibility: protocol.RoomVisibilityPrivate},
		{ID: "remote-room", Members: []string{"remote:luna"}, Visibility: protocol.RoomVisibilityPrivate},
	} {
		if _, err := service.CreateRoom(room); err != nil {
			t.Fatal(err)
		}
	}
	if _, err := service.SendMessage(protocol.SendMessageRequest{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "local-room"}, From: protocol.Actor{Type: "agent", ID: "luna"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "parent"}}}); err != nil {
		t.Fatal(err)
	}
	ctx := privateVisibilityContext("remote:luna", authn.ScopeObserve)
	fqidCtx := privateVisibilityContext("molt://remote/agents/luna", authn.ScopeObserve)
	for name, candidate := range map[string]context.Context{"scoped": ctx, "fqid": fqidCtx} {
		if _, err := service.GetRoomContext(candidate, "local-room"); err == nil {
			t.Fatalf("%s claim read same-named local room", name)
		}
		if _, err := service.GetRoomContext(candidate, "remote-room"); err != nil {
			t.Fatalf("%s claim did not read qualified remote room: %v", name, err)
		}
		if service.eventVisible(candidate, protocol.Event{Type: protocol.EventTypeRoomCreated, Room: &protocol.Room{ID: "local-room", NetworkID: "local", Members: []string{"luna"}, Visibility: protocol.RoomVisibilityPrivate}}) {
			t.Fatalf("%s claim accepted local room event", name)
		}
	}
	remoteRoom, err := service.GetRoomContext(ctx, "remote-room")
	if err != nil {
		t.Fatal(err)
	}
	if service.eventVisible(ctx, protocol.Event{Type: protocol.EventTypeRoomCreated, Room: &remoteRoom}) != true {
		t.Fatal("qualified remote room event was not visible")
	}
	inconsistentRoom := remoteRoom
	inconsistentRoom.Members = append(inconsistentRoom.Members, "atlas")
	if service.eventVisible(ctx, protocol.Event{Type: protocol.EventTypeRoomCreated, Room: &inconsistentRoom}) {
		t.Fatal("inconsistent room membership event was visible")
	}
	if service.eventVisible(ctx, protocol.Event{Type: protocol.EventTypeRoomCreated, Room: &protocol.Room{ID: "missing", NetworkID: "local", Members: []string{"remote:luna"}, Visibility: protocol.RoomVisibilityPrivate}}) {
		t.Fatal("missing authoritative room event was visible")
	}
	if _, err := service.RemoveRoomContext(context.Background(), "remote-room"); err != nil {
		t.Fatal(err)
	}
	if service.eventVisible(ctx, protocol.Event{Type: protocol.EventTypeRoomRemoved, Room: &protocol.Room{ID: "remote-room", NetworkID: "local", Members: []string{"remote:luna"}, Visibility: protocol.RoomVisibilityPrivate}}) {
		t.Fatal("removed room tombstone was visible")
	}
}

func TestRestrictedLifecycleIdentityIsNetworkQualified(t *testing.T) {
	service := newTestService()
	local := privateVisibilityContext("luna", authn.ScopeObserve)
	remote := privateVisibilityContext("remote:luna", authn.ScopeObserve)
	remoteFQID := privateVisibilityContext(protocol.AgentFQID("remote", "luna"), authn.ScopeObserve)
	lifecycleTypes := []string{protocol.EventTypeAgentConnected, protocol.EventTypeAgentDisconnected, protocol.EventTypeAgentRemoved, protocol.EventTypeAgentWakeDelivered, protocol.EventTypeAgentWakeFailed}
	tests := []struct {
		name, outer, nested, fqid string
		local, remote             bool
	}{
		{"no labels use local fallback", "", "", "", true, false},
		{"matching local authorities", "local", "local", protocol.AgentFQID("local", "luna"), true, false},
		{"matching remote authorities", "remote", "remote", protocol.AgentFQID("remote", "luna"), false, true},
		{"outer remote only", "remote", "", "", false, true},
		{"nested remote only", "", "remote", "", false, true},
		{"outer local nested remote", "local", "remote", "", false, false},
		{"outer local nested remote matching FQID", "local", "remote", protocol.AgentFQID("remote", "luna"), false, false},
		{"outer remote nested local", "remote", "local", "", false, false},
		{"outer remote nested local matching FQID", "remote", "local", protocol.AgentFQID("local", "luna"), false, false},
		{"remote FQID only", "", "", protocol.AgentFQID("remote", "luna"), false, true},
		{"matching local labels remote FQID", "local", "local", protocol.AgentFQID("remote", "luna"), false, false},
		{"matching remote labels local FQID", "remote", "remote", protocol.AgentFQID("local", "luna"), false, false},
		{"FQID agent mismatch", "local", "local", protocol.AgentFQID("local", "atlas"), false, false},
	}
	for _, eventType := range lifecycleTypes {
		for _, test := range tests {
			event := protocol.Event{NetworkID: test.outer, Type: eventType, Agent: &protocol.AgentEvent{
				AgentID: "luna", NetworkID: test.nested, FQID: test.fqid,
			}}
			for _, claim := range []struct {
				name string
				ctx  context.Context
				want bool
			}{
				{"local claim", local, test.local},
				{"remote scoped claim", remote, test.remote},
				{"remote FQID claim", remoteFQID, test.remote},
			} {
				if got := service.eventVisible(claim.ctx, event); got != claim.want {
					t.Fatalf("%s/%s %s = %v, want %v", test.name, claim.name, eventType, got, claim.want)
				}
			}
		}
	}
}

func TestRestrictedSubscribeFromFiltersReplayAndLiveEvents(t *testing.T) {
	service := newTestService()
	for _, room := range []protocol.CreateRoomRequest{{ID: "own", Members: []string{"luna"}, Visibility: protocol.RoomVisibilityPrivate}, {ID: "other", Members: []string{"atlas"}, Visibility: protocol.RoomVisibilityPrivate}} {
		if _, err := service.CreateRoom(room); err != nil {
			t.Fatal(err)
		}
	}
	_, err := service.SendMessage(protocol.SendMessageRequest{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "own"}, From: protocol.Actor{Type: "agent", ID: "luna"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "cursor"}}})
	if err != nil {
		t.Fatal(err)
	}
	dmOwn, err := service.SendMessage(protocol.SendMessageRequest{Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm-own", ParticipantIDs: []string{"luna", "mira"}}, From: protocol.Actor{Type: "agent", ID: "luna"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "dm"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := service.SendMessage(protocol.SendMessageRequest{Target: protocol.Target{Kind: protocol.TargetKindDM, DMID: "dm-other", ParticipantIDs: []string{"atlas", "mira"}}, From: protocol.Actor{Type: "agent", ID: "atlas"}, Parts: []protocol.Part{{Kind: protocol.PartKindText, Text: "hidden"}}}); err != nil {
		t.Fatal(err)
	}
	service.AgentConnected(context.Background(), protocol.Actor{Type: "agent", ID: "luna"})
	service.AgentWakeDelivered(context.Background(), protocol.Actor{Type: "agent", ID: "luna"}, protocol.Event{Message: &protocol.Message{ID: "wake-own", Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "own"}}})
	service.AgentWakeFailed(context.Background(), protocol.Actor{Type: "agent", ID: "atlas"}, protocol.Event{Message: &protocol.Message{ID: "wake-other", Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "other"}}}, context.Canceled, protocol.WakeFailureDetails{})
	ctx, cancel := context.WithCancel(privateVisibilityContext("luna", authn.ScopeObserve))
	defer cancel()
	stream := service.SubscribeFrom(ctx, "missing-cursor")
	broker := service.broker.(*events.Broker)
	broker.Publish(protocol.Event{ID: "blocked-room", Type: protocol.EventTypeMessageCreated, Message: &protocol.Message{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "other"}}})
	broker.Publish(protocol.Event{ID: "blocked-unknown", Type: protocol.EventTypeMessageCreated, Message: &protocol.Message{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "missing"}}})
	broker.Publish(protocol.Event{ID: "blocked-pair", Type: protocol.EventTypePairingUpdated})
	broker.Publish(protocol.Event{ID: "allowed-sentinel", Type: protocol.EventTypeMessageCreated, Message: &protocol.Message{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "own"}}})
	seen := make([]string, 0, 12)
	seenTypes := make(map[string]bool)
	deadline := time.After(time.Second)
	for {
		select {
		case event, ok := <-stream:
			if !ok {
				t.Fatal("restricted stream closed before sentinel")
			}
			seen = append(seen, event.ID)
			seenTypes[event.Type] = true
			if event.Type == protocol.EventTypeAgentWakeFailed && event.Agent != nil && event.Agent.AgentID == "atlas" {
				t.Fatalf("restricted stream leaked unrelated wake-failed agent: %#v", event.Agent)
			}
			if event.ID == "allowed-sentinel" {
				goto done
			}
		case <-deadline:
			t.Fatalf("stream did not reach sentinel, saw %v", seen)
		}
	}
done:
	for _, blocked := range []string{"blocked-room", "blocked-unknown", "blocked-pair"} {
		for _, id := range seen {
			if id == blocked {
				t.Fatalf("restricted stream leaked %q: %v", blocked, seen)
			}
		}
	}
	if len(seen) < 2 || !containsID(seen, dmOwn.EventID) || !containsID(seen, "allowed-sentinel") || !seenTypes[protocol.EventTypeReplayGap] || !seenTypes[protocol.EventTypeAgentConnected] || !seenTypes[protocol.EventTypeAgentWakeDelivered] {
		t.Fatalf("unexpected replay/live stream %v", seen)
	}
	operatorContext, operatorCancel := context.WithCancel(context.Background())
	defer operatorCancel()
	operator := authn.WithClaims(operatorContext, authn.NewStaticClaims(authn.TokenConfig{ID: "operator", Scopes: []authn.Scope{authn.ScopeAdmin}}))
	operatorStream := service.SubscribeFrom(operator, "blocked-room")
	broker.Publish(protocol.Event{ID: "operator-sentinel", Type: protocol.EventTypeMessageCreated, Message: &protocol.Message{Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: "other"}}})
	operatorSawPrivate := false
	operatorDeadline := time.After(time.Second)
	for {
		select {
		case event, ok := <-operatorStream:
			if !ok {
				t.Fatal("operator stream closed before sentinel")
			}
			if event.ID == "blocked-room" || (event.Message != nil && event.Message.Target.RoomID == "other") {
				operatorSawPrivate = true
			}
			if event.ID == "operator-sentinel" {
				if !operatorSawPrivate {
					t.Fatal("operator did not observe unrelated private replay/live event")
				}
				return
			}
		case <-operatorDeadline:
			t.Fatal("operator did not receive unrestricted event")
		}
	}
}

func containsID(ids []string, want string) bool {
	for _, id := range ids {
		if id == want {
			return true
		}
	}
	return false
}

func containsRoomIDs(rooms []protocol.Room, expected ...string) bool {
	seen := make(map[string]bool, len(rooms))
	for _, room := range rooms {
		seen[room.ID] = true
	}
	if len(seen) != len(expected) {
		return false
	}
	for _, id := range expected {
		if !seen[id] {
			return false
		}
	}
	return true
}
