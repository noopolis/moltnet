package rooms

import (
	"errors"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestValidateUpdateRoomMembersRequest(t *testing.T) {
	t.Parallel()

	if err := validateUpdateRoomMembersRequest(protocol.UpdateRoomMembersRequest{
		Add: []string{"writer"},
	}); err != nil {
		t.Fatalf("validateUpdateRoomMembersRequest() error = %v", err)
	}

	err := validateUpdateRoomMembersRequest(protocol.UpdateRoomMembersRequest{
		Add: []string{"bad\nmember"},
	})
	if err == nil {
		t.Fatal("expected invalid room member error")
	}
	if !errors.Is(err, ErrInvalidRoomRequest) {
		t.Fatalf("expected ErrInvalidRoomRequest, got %v", err)
	}
}
