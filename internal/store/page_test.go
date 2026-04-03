package store

import (
	"errors"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestPaginateByIDSupportsBeforeAndAfter(t *testing.T) {
	t.Parallel()

	rooms := []roomItem{
		{Room: protocol.Room{ID: "a"}},
		{Room: protocol.Room{ID: "b"}},
		{Room: protocol.Room{ID: "c"}},
	}
	afterValues, afterInfo, err := paginateByID(rooms, protocol.PageRequest{
		After: "a",
		Limit: 1,
	})
	if err != nil {
		t.Fatalf("paginateByID() after error = %v", err)
	}
	if len(afterValues) != 1 || afterValues[0].GetID() != "b" {
		t.Fatalf("unexpected after values %#v", afterValues)
	}
	if !afterInfo.HasMore || afterInfo.NextAfter != "b" || afterInfo.NextBefore != "b" {
		t.Fatalf("unexpected after page info %#v", afterInfo)
	}

	beforeValues, beforeInfo, err := paginateByID(rooms, protocol.PageRequest{
		Before: "c",
		Limit:  1,
	})
	if err != nil {
		t.Fatalf("paginateByID() before error = %v", err)
	}
	if len(beforeValues) != 1 || beforeValues[0].GetID() != "b" {
		t.Fatalf("unexpected before values %#v", beforeValues)
	}
	if !beforeInfo.HasMore || beforeInfo.NextBefore != "b" || beforeInfo.NextAfter != "b" {
		t.Fatalf("unexpected before page info %#v", beforeInfo)
	}
}

func TestPaginateByIDRejectsUnknownCursor(t *testing.T) {
	t.Parallel()

	rooms := []roomItem{
		{Room: protocol.Room{ID: "a"}},
		{Room: protocol.Room{ID: "b"}},
	}

	afterValues, afterInfo, err := paginateByID(rooms, protocol.PageRequest{
		After: "missing",
		Limit: 1,
	})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expected ErrInvalidCursor, got %v", err)
	}
	if len(afterValues) != 0 || afterInfo != (protocol.PageInfo{}) {
		t.Fatalf("unexpected after result %#v %#v", afterValues, afterInfo)
	}

	beforeValues, beforeInfo, err := paginateByID(rooms, protocol.PageRequest{
		Before: "missing",
		Limit:  1,
	})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expected ErrInvalidCursor, got %v", err)
	}
	if len(beforeValues) != 0 || beforeInfo != (protocol.PageInfo{}) {
		t.Fatalf("unexpected before result %#v %#v", beforeValues, beforeInfo)
	}
}

func TestItemGetIDHelpers(t *testing.T) {
	t.Parallel()

	if got := (roomItem{Room: protocol.Room{ID: "room"}}).GetID(); got != "room" {
		t.Fatalf("unexpected room id %q", got)
	}
	if got := (agentItem{AgentSummary: protocol.AgentSummary{ID: "agent"}}).GetID(); got != "agent" {
		t.Fatalf("unexpected agent id %q", got)
	}
	if got := (dmItem{DirectConversation: protocol.DirectConversation{ID: "dm"}}).GetID(); got != "dm" {
		t.Fatalf("unexpected dm id %q", got)
	}
}

func TestPageMessagesAndArtifactsResults(t *testing.T) {
	t.Parallel()

	messages := []protocol.Message{
		{ID: "msg_1"},
		{ID: "msg_2"},
		{ID: "msg_3"},
	}
	messagePage, err := pageMessagesResult(messages, protocol.PageRequest{Limit: 2})
	if err != nil {
		t.Fatalf("pageMessagesResult() error = %v", err)
	}
	if len(messagePage.Messages) != 2 || messagePage.Messages[0].ID != "msg_2" || !messagePage.Page.HasMore {
		t.Fatalf("unexpected message page %#v", messagePage)
	}
	invalidMessagePage, err := pageMessagesResult(messages, protocol.PageRequest{After: "missing", Limit: 1})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expected ErrInvalidCursor, got %v", err)
	}
	if len(invalidMessagePage.Messages) != 0 || invalidMessagePage.Page != (protocol.PageInfo{}) {
		t.Fatalf("expected empty invalid cursor message page, got %#v", invalidMessagePage)
	}

	artifacts := []protocol.Artifact{
		{ID: "art_1"},
		{ID: "art_2"},
		{ID: "art_3"},
	}
	artifactPage, err := pageArtifactsResult(artifacts, protocol.PageRequest{Limit: 2})
	if err != nil {
		t.Fatalf("pageArtifactsResult() error = %v", err)
	}
	if len(artifactPage.Artifacts) != 2 || artifactPage.Artifacts[0].ID != "art_2" || !artifactPage.Page.HasMore {
		t.Fatalf("unexpected artifact page %#v", artifactPage)
	}
	invalidArtifactPage, err := pageArtifactsResult(artifacts, protocol.PageRequest{Before: "missing", Limit: 1})
	if !errors.Is(err, ErrInvalidCursor) {
		t.Fatalf("expected ErrInvalidCursor, got %v", err)
	}
	if len(invalidArtifactPage.Artifacts) != 0 || invalidArtifactPage.Page != (protocol.PageInfo{}) {
		t.Fatalf("expected empty invalid artifact page, got %#v", invalidArtifactPage)
	}
}
