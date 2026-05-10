package transport

import (
	"strings"
	"sync"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type attachmentSession struct {
	mu           sync.Mutex
	resumeCursor string
	pending      map[string]struct{}
	wakePending  map[string]protocol.Event
	order        []string
}

func newAttachmentSession(resumeCursor string) *attachmentSession {
	return &attachmentSession{
		resumeCursor: strings.TrimSpace(resumeCursor),
		pending:      make(map[string]struct{}),
		wakePending:  make(map[string]protocol.Event),
		order:        make([]string, 0, maxPendingAttachmentAcks),
	}
}

func (s *attachmentSession) NoteSent(cursor string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return
	}
	if _, ok := s.pending[cursor]; ok {
		return
	}
	s.pending[cursor] = struct{}{}
	s.order = append(s.order, cursor)
	s.compactPendingLocked()
}

func (s *attachmentSession) NoteWakeSent(cursor string, event protocol.Event) {
	s.mu.Lock()
	defer s.mu.Unlock()
	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return
	}
	s.wakePending[cursor] = event
}

func (s *attachmentSession) Ack(cursor string) (protocol.Event, bool, bool) {
	s.mu.Lock()
	defer s.mu.Unlock()

	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return protocol.Event{}, false, false
	}
	if _, ok := s.pending[cursor]; !ok {
		return protocol.Event{}, false, false
	}

	delete(s.pending, cursor)
	event, wake := s.wakePending[cursor]
	delete(s.wakePending, cursor)
	s.resumeCursor = cursor
	s.trimAckedPrefixLocked()
	return event, wake, true
}

func (s *attachmentSession) PendingWakes() []protocol.Event {
	s.mu.Lock()
	defer s.mu.Unlock()
	events := make([]protocol.Event, 0, len(s.wakePending))
	for _, cursor := range s.order {
		if event, ok := s.wakePending[cursor]; ok {
			events = append(events, event)
		}
	}
	return events
}

func (s *attachmentSession) ResumeCursor() string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.resumeCursor
}

func (s *attachmentSession) compactPendingLocked() {
	for len(s.pending) > maxPendingAttachmentAcks && len(s.order) > 0 {
		oldest := s.order[0]
		s.order = s.order[1:]
		if _, ok := s.pending[oldest]; !ok {
			delete(s.wakePending, oldest)
			continue
		}
		delete(s.pending, oldest)
		delete(s.wakePending, oldest)
		s.resumeCursor = oldest
	}
}

func (s *attachmentSession) trimAckedPrefixLocked() {
	trim := 0
	for trim < len(s.order) {
		if _, ok := s.pending[s.order[trim]]; ok {
			break
		}
		trim++
	}
	if trim == 0 {
		return
	}
	if trim >= len(s.order) {
		s.order = make([]string, 0, maxPendingAttachmentAcks)
		return
	}
	remaining := append(make([]string, 0, len(s.order)-trim), s.order[trim:]...)
	s.order = remaining
}
