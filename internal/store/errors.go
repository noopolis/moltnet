package store

import "errors"

var (
	ErrDuplicateMessage   = errors.New("message already exists")
	ErrDMTopologyConflict = errors.New("direct conversation topology conflict")
	ErrRoomExists         = errors.New("room already exists")
	ErrRoomNotFound       = errors.New("room not found")
	ErrInvalidCursor      = errors.New("invalid cursor")
	ErrAgentCredential    = errors.New("agent credential mismatch")
)
