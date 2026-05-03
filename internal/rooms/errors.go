package rooms

import (
	"errors"
	"fmt"
	"net/http"
	"strings"
)

var (
	ErrInvalidRoomID          = errors.New("room id is required")
	ErrInvalidRoomRequest     = errors.New("invalid room request")
	ErrRoomExists             = errors.New("room already exists")
	ErrUnknownRoom            = errors.New("unknown room")
	ErrUnknownThread          = errors.New("unknown thread")
	ErrUnknownAgent           = errors.New("unknown agent")
	ErrInvalidDMID            = errors.New("dm id is required")
	ErrInvalidCursor          = errors.New("invalid cursor")
	ErrInvalidMessage         = errors.New("invalid message request")
	ErrUnknownDM              = errors.New("unknown direct conversation")
	ErrArtifactFilter         = errors.New("artifact filter is required")
	ErrUnknownPairing         = errors.New("unknown pairing")
	ErrPairingClientMissing   = errors.New("pairing client is not configured")
	ErrHumanIngressDisabled   = errors.New("human ingress is disabled")
	ErrDirectMessagesDisabled = errors.New("direct messages are disabled")
	ErrRemotePairing          = errors.New("paired network request failed")
	ErrAgentConflict          = errors.New("agent identity conflict")
	ErrAgentUnauthorized      = errors.New("agent identity requires authentication")
	ErrAgentForbidden         = errors.New("agent identity is forbidden")
)

func unknownRoomError(roomID string) error {
	return &Error{
		status: http.StatusNotFound,
		msg:    fmt.Sprintf("unknown room %q", roomID),
		cause:  ErrUnknownRoom,
	}
}

func roomExistsError(roomID string) error {
	return &Error{
		status: http.StatusConflict,
		msg:    fmt.Sprintf("room %q already exists", roomID),
		cause:  ErrRoomExists,
	}
}

func unknownThreadError(threadID string) error {
	return &Error{
		status: http.StatusNotFound,
		msg:    fmt.Sprintf("unknown thread %q", threadID),
		cause:  ErrUnknownThread,
	}
}

func unknownPairingError(pairingID string) error {
	return &Error{
		status: http.StatusNotFound,
		msg:    fmt.Sprintf("unknown pairing %q", pairingID),
		cause:  ErrUnknownPairing,
	}
}

func remotePairingError(err error) error {
	return &Error{
		status: http.StatusBadGateway,
		msg:    fmt.Sprintf("paired network request failed: %v", err),
		cause:  ErrRemotePairing,
	}
}

func invalidRoomIDError() error {
	return &Error{
		status: http.StatusUnprocessableEntity,
		msg:    ErrInvalidRoomID.Error(),
		cause:  ErrInvalidRoomID,
	}
}

func invalidRoomRequestReasonError(message string) error {
	return &Error{
		status: http.StatusUnprocessableEntity,
		msg:    strings.TrimSpace(message),
		cause:  ErrInvalidRoomRequest,
	}
}

func invalidDMIDError() error {
	return &Error{
		status: http.StatusUnprocessableEntity,
		msg:    ErrInvalidDMID.Error(),
		cause:  ErrInvalidDMID,
	}
}

func invalidCursorReasonError(cursor string) error {
	return &Error{
		status: http.StatusUnprocessableEntity,
		msg:    fmt.Sprintf("invalid cursor %q", strings.TrimSpace(cursor)),
		cause:  ErrInvalidCursor,
	}
}

func invalidMessageRequestError(message string) error {
	return &Error{
		status: http.StatusUnprocessableEntity,
		msg:    strings.TrimSpace(message),
		cause:  ErrInvalidMessage,
	}
}

func unknownDirectConversationError(dmID string) error {
	return &Error{
		status: http.StatusNotFound,
		msg:    fmt.Sprintf("unknown direct conversation %q", dmID),
		cause:  ErrUnknownDM,
	}
}

func unknownAgentError(agentID string) error {
	return &Error{
		status: http.StatusNotFound,
		msg:    fmt.Sprintf("unknown agent %q", agentID),
		cause:  ErrUnknownAgent,
	}
}

func agentConflictError(agentID string) error {
	return &Error{
		status: http.StatusConflict,
		msg:    fmt.Sprintf("agent %q is already registered with different credentials", agentID),
		cause:  ErrAgentConflict,
	}
}

func agentRegisteredError(agentID string) error {
	return &Error{
		status: http.StatusConflict,
		msg:    fmt.Sprintf("agent %q is already registered", agentID),
		cause:  ErrAgentConflict,
	}
}

func agentRequiresTokenError(agentID string) error {
	return &Error{
		status: http.StatusUnauthorized,
		msg:    fmt.Sprintf("agent %q requires its agent token", agentID),
		cause:  ErrAgentUnauthorized,
	}
}

func agentTokenInvalidForAgentError(agentID string) error {
	return &Error{
		status: http.StatusConflict,
		msg:    fmt.Sprintf("agent token is not valid for agent %q", agentID),
		cause:  ErrAgentConflict,
	}
}

func agentForbiddenError(message string) error {
	return &Error{
		status: http.StatusForbidden,
		msg:    strings.TrimSpace(message),
		cause:  ErrAgentForbidden,
	}
}

func agentRegistrationRequiredError(agentID string) error {
	return &Error{
		status: http.StatusUnauthorized,
		msg:    fmt.Sprintf("agent %q must be registered before sending", agentID),
		cause:  ErrAgentUnauthorized,
	}
}

func artifactFilterRequiredError() error {
	return &Error{
		status: http.StatusUnprocessableEntity,
		msg:    ErrArtifactFilter.Error(),
		cause:  ErrArtifactFilter,
	}
}

func humanIngressDisabledError() error {
	return &Error{
		status: http.StatusForbidden,
		msg:    "human ingress is disabled for this network",
		cause:  ErrHumanIngressDisabled,
	}
}

func directMessagesDisabledError() error {
	return &Error{
		status: http.StatusForbidden,
		msg:    "direct messages are disabled for this network",
		cause:  ErrDirectMessagesDisabled,
	}
}

func pairingClientMissingError() error {
	return &Error{
		status: http.StatusServiceUnavailable,
		msg:    ErrPairingClientMissing.Error(),
		cause:  ErrPairingClientMissing,
	}
}

type Error struct {
	status int
	msg    string
	cause  error
}

func (e *Error) Error() string {
	return e.msg
}

func (e *Error) Unwrap() error {
	return e.cause
}

func (e *Error) StatusCode() int {
	return e.status
}
