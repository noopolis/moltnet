package store

import "errors"

var (
	ErrDuplicateMessage = errors.New("message already exists")
	ErrRoomExists       = errors.New("room already exists")
	ErrRoomNotFound     = errors.New("room not found")
	ErrInvalidCursor    = errors.New("invalid cursor")
)
