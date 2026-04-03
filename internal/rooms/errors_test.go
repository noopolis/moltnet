package rooms

import (
	"errors"
	"net/http"
	"testing"
)

func TestRoomErrorsExposeStatusAndSentinels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		err    error
		target error
		status int
	}{
		{err: unknownRoomError("research"), target: ErrUnknownRoom, status: http.StatusNotFound},
		{err: unknownThreadError("thread_1"), target: ErrUnknownThread, status: http.StatusNotFound},
		{err: unknownPairingError("pair_1"), target: ErrUnknownPairing, status: http.StatusNotFound},
		{err: remotePairingError(errors.New("boom")), target: ErrRemotePairing, status: http.StatusBadGateway},
		{err: invalidRoomIDError(), target: ErrInvalidRoomID, status: http.StatusUnprocessableEntity},
		{err: invalidDMIDError(), target: ErrInvalidDMID, status: http.StatusUnprocessableEntity},
		{err: humanIngressDisabledError(), target: ErrHumanIngressDisabled, status: http.StatusForbidden},
		{err: pairingClientMissingError(), target: ErrPairingClientMissing, status: http.StatusServiceUnavailable},
	}

	for _, test := range cases {
		coder, ok := test.err.(interface{ StatusCode() int })
		if !ok {
			t.Fatalf("error %T does not expose status", test.err)
		}
		if coder.StatusCode() != test.status {
			t.Fatalf("unexpected status %d for %v", coder.StatusCode(), test.err)
		}
		if test.err.Error() == "" {
			t.Fatalf("expected non-empty error message for %v", test.target)
		}
		if !errors.Is(test.err, test.target) {
			t.Fatalf("expected %v to match %v", test.err, test.target)
		}
	}
}
