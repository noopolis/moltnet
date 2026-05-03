package transport

import (
	"strings"
	"sync"
)

type attachmentSession struct {
	mu           sync.Mutex
	resumeCursor string
	pending      map[string]struct{}
	order        []string
}

func newAttachmentSession(resumeCursor string) *attachmentSession {
	return &attachmentSession{
		resumeCursor: strings.TrimSpace(resumeCursor),
		pending:      make(map[string]struct{}),
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

func (s *attachmentSession) Ack(cursor string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()

	cursor = strings.TrimSpace(cursor)
	if cursor == "" {
		return false
	}
	if _, ok := s.pending[cursor]; !ok {
		return false
	}

	delete(s.pending, cursor)
	s.resumeCursor = cursor
	s.trimAckedPrefixLocked()
	return true
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
			continue
		}
		delete(s.pending, oldest)
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
