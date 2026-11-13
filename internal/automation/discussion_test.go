package automation

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

var testMembers = []string{"codex", "grok"}

func testMessage(id, from, reply string) protocol.Message {
	return protocol.Message{ID: id, NetworkID: "local", From: protocol.Actor{Type: "agent", ID: from, CredentialBound: true}, Target: protocol.Target{Kind: "room", RoomID: "general"}, ReplyTo: reply, DiscussionID: "d"}
}

func newDiscussion(t *testing.T, rounds int) *Manager {
	t.Helper()
	m, err := New("", "local")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.Start("general", "d", "prompt", protocol.StartDiscussionRequest{Topic: "compare approaches", Participants: testMembers, MaxRounds: rounds}); err != nil {
		t.Fatal(err)
	}
	prompt := testMessage("prompt", "operator", "")
	prompt.From.Type = "human"
	if err := m.Apply(&prompt, testMembers, func() (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(prompt.Wake.Recipients, []string{"codex"}) {
		t.Fatal(prompt.Wake)
	}
	return m
}

func TestDiscussionFiniteRoundsAndStaleReplies(t *testing.T) {
	m := newDiscussion(t, 2)
	previous := "prompt"
	for turn := 0; turn < 4; turn++ {
		message := testMessage(fmt.Sprint("reply", turn), testMembers[turn%2], previous)
		if err := m.Apply(&message, testMembers, func() (bool, error) { return true, nil }); err != nil {
			t.Fatal(err)
		}
		d := m.View("general", testMembers).Discussion
		if d.CompletedTurns != turn+1 {
			t.Fatal(d)
		}
		if turn < 3 {
			next := testMembers[(turn+1)%2]
			if !m.Eligible(message, next) {
				t.Fatal("next speaker not routed")
			}
		} else if d.Active || len(message.Wake.Recipients) != 0 {
			t.Fatal("discussion exceeded budget", d)
		}
		previous = message.ID
	}
	stale := testMessage("stale", "codex", previous)
	called := false
	if err := m.Apply(&stale, testMembers, func() (bool, error) { called = true; return true, nil }); err == nil || called {
		t.Fatal("stopped discussion accepted a reply")
	}
}

func TestDiscussionRejectsSpoofWrongTurnAndRollback(t *testing.T) {
	for _, kind := range []string{"wrong sender", "unbound", "wrong parent", "wrong discussion", "missing discussion", "missing parent", "missing both", "foreign origin", "append failure", "duplicate"} {
		t.Run(kind, func(t *testing.T) {
			m := newDiscussion(t, 1)
			message := testMessage("reply", "codex", "prompt")
			switch kind {
			case "wrong sender":
				message.From.ID = "grok"
			case "unbound":
				message.From.CredentialBound = false
			case "wrong parent":
				message.ReplyTo = "other"
			case "wrong discussion":
				message.DiscussionID = "other"
			case "missing discussion":
				message.DiscussionID = ""
			case "missing parent":
				message.ReplyTo = ""
			case "missing both":
				message.DiscussionID, message.ReplyTo = "", ""
			case "foreign origin":
				message.Origin.ReceivedVia = "pair-1"
			}
			called := false
			err := m.Apply(&message, testMembers, func() (bool, error) {
				called = true
				if kind == "append failure" {
					return false, errors.New("disk failed")
				}
				return kind != "duplicate", nil
			})
			if kind != "duplicate" && err == nil {
				t.Fatal("expected rejection")
			}
			if kind != "duplicate" && kind != "append failure" && called {
				t.Fatal("invalid reply reached storage")
			}
			if d := m.View("general", testMembers).Discussion; d.CompletedTurns != 0 || d.ExpectedAgent != "codex" {
				t.Fatal("turn advanced without durable reply", d)
			}
		})
	}
}

func TestConcurrentRepliesAdvanceOnce(t *testing.T) {
	m := newDiscussion(t, 1)
	var wg sync.WaitGroup
	accepted := make(chan bool, 2)
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			msg := testMessage(fmt.Sprint(i), "codex", "prompt")
			accepted <- m.Apply(&msg, testMembers, func() (bool, error) { return true, nil }) == nil
		}(i)
	}
	wg.Wait()
	close(accepted)
	count := 0
	for ok := range accepted {
		if ok {
			count++
		}
	}
	if count != 1 {
		t.Fatalf("accepted %d replies", count)
	}
}

func TestOwnerInterruptStopTimeoutRestart(t *testing.T) {
	for _, operation := range []string{"owner", "stop", "timeout", "restart"} {
		t.Run(operation, func(t *testing.T) {
			m := newDiscussion(t, 2)
			switch operation {
			case "owner":
				msg := testMessage("human", "operator", "")
				msg.DiscussionID = ""
				msg.From.Type = "human"
				if err := m.Apply(&msg, testMembers, func() (bool, error) { return true, nil }); err != nil {
					t.Fatal(err)
				}
			case "stop":
				if err := m.Stop("general", "stopped"); err != nil {
					t.Fatal(err)
				}
			case "timeout":
				m.room("general").Discussion.UpdatedAt = time.Now().Add(-11 * time.Minute)
			case "restart":
				m.path = filepath.Join(t.TempDir(), "automation.json")
				if err := m.save(); err != nil {
					t.Fatal(err)
				}
				var err error
				m, err = New(m.path, "local")
				if err != nil {
					t.Fatal(err)
				}
			}
			if m.View("general", testMembers).Discussion.Active {
				t.Fatal("discussion still active")
			}
		})
	}
}

func TestSettingsPersistAndNoSecretInView(t *testing.T) {
	path := filepath.Join(t.TempDir(), "automation.json")
	m, err := New(path, "local")
	if err != nil {
		t.Fatal(err)
	}
	if err := m.SetMode("general", "grok", "never"); err != nil {
		t.Fatal(err)
	}
	m, err = New(path, "local")
	if err != nil {
		t.Fatal(err)
	}
	if m.View("general", testMembers).Settings[1].Mode != "never" {
		t.Fatal("setting not persisted")
	}
	msg := testMessage("owner", "operator", "")
	msg.DiscussionID = ""
	msg.From.Type = "human"
	if err := m.Apply(&msg, testMembers, func() (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(msg.Wake.Recipients, []string{"codex"}) {
		t.Fatal(msg.Wake)
	}
	if info, err := os.Stat(path); err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("store is not private", err)
	}
}

func TestDiscussionValidationBoundaries(t *testing.T) {
	for _, rounds := range []int{-1, 0, 1, 20, 21} {
		m, _ := New("", "local")
		err := m.Start("general", "d", "p", protocol.StartDiscussionRequest{Topic: "topic", Participants: testMembers, MaxRounds: rounds})
		if (err == nil) != (rounds >= 1 && rounds <= 20) {
			t.Fatalf("rounds=%d err=%v", rounds, err)
		}
	}
}

func TestExpiredDiscussionDoesNotBlockOrdinaryAgentMessage(t *testing.T) {
	m := newDiscussion(t, 1)
	m.room("general").Discussion.UpdatedAt = time.Now().Add(-11 * time.Minute)
	message := testMessage("ordinary", "codex", "")
	message.DiscussionID = ""
	if err := m.Apply(&message, testMembers, func() (bool, error) { return true, nil }); err != nil {
		t.Fatal(err)
	}
	if m.View("general", testMembers).Discussion.Active || len(message.Wake.Recipients) != 0 {
		t.Fatal("expired discussion blocked ordinary traffic or woke another agent")
	}
}
