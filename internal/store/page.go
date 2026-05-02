package store

import "github.com/noopolis/moltnet/pkg/protocol"

func paginateMessages(messages []protocol.Message, page protocol.PageRequest) ([]protocol.Message, protocol.PageInfo, error) {
	selected, info, err := protocol.PaginateByIDWithMode(
		messages,
		page,
		func(message protocol.Message) string { return message.ID },
		protocol.PageHasMoreCursorDirection,
	)
	if err != nil {
		return nil, protocol.PageInfo{}, ErrInvalidCursor
	}
	return selected, info, nil
}
