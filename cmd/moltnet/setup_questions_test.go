package main

import (
	"context"
	"strings"
	"testing"
)

// These tests cover askSetupRooms' "rename" and "add more" branches --
// including their empty-answer errors and validateCanonicalRoomID's
// rejection path -- the only two places this wizard mints room ids, and
// previously the least-covered code in this package (17.9%).
//
// promptSelect (setup_prompt_select.go) routes to its plain, line-based
// fallback whenever promptReader is not literally os.Stdin, which
// withPromptAnswers (uninstall_test.go) already arranges: the first line
// answers Q5's own choice (1-based index, or an empty line for the
// index-0 default), and any further lines answer the sub-question each
// branch reads next.

func TestAskSetupRoomsKeepsGeneralOnDefaultAnswer(t *testing.T) {
	withPromptAnswers(t, "1")

	ids, err := captureAskSetupRooms(t)
	if err != nil {
		t.Fatalf("askSetupRooms() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != starterRoomID {
		t.Fatalf("askSetupRooms() = %v, want [%q]", ids, starterRoomID)
	}
}

func TestAskSetupRoomsRenameSucceeds(t *testing.T) {
	withPromptAnswers(t, "2", "team-chat")

	ids, err := captureAskSetupRooms(t)
	if err != nil {
		t.Fatalf("askSetupRooms() error = %v", err)
	}
	if len(ids) != 1 || ids[0] != "team-chat" {
		t.Fatalf("askSetupRooms() = %v, want [%q]", ids, "team-chat")
	}
}

func TestAskSetupRoomsRenameRejectsEmptyAnswer(t *testing.T) {
	withPromptAnswers(t, "2", "")

	_, err := captureAskSetupRooms(t)
	if err == nil {
		t.Fatal("askSetupRooms() error = nil, want an error for an empty rename answer")
	}
	// Specifically the dedicated empty-answer message, not merely whatever
	// validateCanonicalRoomID would also say about "" -- pins the early,
	// more actionable guard rather than accidentally passing via the
	// downstream check alone.
	if !strings.Contains(err.Error(), "a new room name is required") {
		t.Fatalf("askSetupRooms() error = %v, want the dedicated empty-rename message", err)
	}
}

func TestAskSetupRoomsRenameRejectsInvalidRoomID(t *testing.T) {
	withPromptAnswers(t, "2", "Not A Valid Id")

	if _, err := captureAskSetupRooms(t); err == nil {
		t.Fatal("askSetupRooms() error = nil, want an error for a rename answer that fails validateCanonicalRoomID")
	}
}

func TestAskSetupRoomsAddMoreSucceeds(t *testing.T) {
	withPromptAnswers(t, "3", "ops, incidents")

	ids, err := captureAskSetupRooms(t)
	if err != nil {
		t.Fatalf("askSetupRooms() error = %v", err)
	}
	want := []string{starterRoomID, "ops", "incidents"}
	if len(ids) != len(want) {
		t.Fatalf("askSetupRooms() = %v, want %v", ids, want)
	}
	for i, id := range want {
		if ids[i] != id {
			t.Fatalf("askSetupRooms() = %v, want %v", ids, want)
		}
	}
}

func TestAskSetupRoomsAddMoreRejectsEmptyAnswer(t *testing.T) {
	withPromptAnswers(t, "3", "")

	_, err := captureAskSetupRooms(t)
	if err == nil {
		t.Fatal("askSetupRooms() error = nil, want an error when no additional room id is given")
	}
	if !strings.Contains(err.Error(), "at least one additional room id is required") {
		t.Fatalf("askSetupRooms() error = %v, want the dedicated empty-add-more message", err)
	}
}

func TestAskSetupRoomsAddMoreRejectsInvalidRoomID(t *testing.T) {
	withPromptAnswers(t, "3", "ops, Not Valid")

	err := func() error {
		_, err := captureAskSetupRooms(t)
		return err
	}()
	if err == nil {
		t.Fatal("askSetupRooms() error = nil, want an error for an add-more list containing an invalid room id")
	}
	if !strings.Contains(err.Error(), "Not Valid") {
		t.Fatalf("askSetupRooms() error = %v, want it to name the offending id", err)
	}
}

// captureAskSetupRooms runs askSetupRooms with its own stdout captured (it
// prints a checkmark line on success), returning the resolved room ids.
func captureAskSetupRooms(t *testing.T) ([]string, error) {
	t.Helper()
	var ids []string
	var err error
	captureStdout(t, func() {
		ids, err = askSetupRooms(context.Background())
	})
	return ids, err
}
