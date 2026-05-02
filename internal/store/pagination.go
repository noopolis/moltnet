package store

import "github.com/noopolis/moltnet/pkg/protocol"

func pageMessagesResult(messages []protocol.Message, page protocol.PageRequest) (protocol.MessagePage, error) {
	if page.Before == "" && page.After == "" {
		limit := page.Limit
		if limit <= 0 || len(messages) <= limit {
			return protocol.MessagePage{
				Messages: append([]protocol.Message(nil), messages...),
				Page:     protocol.PageInfo{},
			}, nil
		}

		selected := append([]protocol.Message(nil), messages[len(messages)-limit:]...)
		return protocol.MessagePage{
			Messages: selected,
			Page: protocol.PageInfo{
				HasMore:    true,
				NextBefore: selected[0].ID,
			},
		}, nil
	}

	selected, info, err := paginateMessages(messages, page)
	if err != nil {
		return protocol.MessagePage{}, err
	}
	return protocol.MessagePage{
		Messages: selected,
		Page:     info,
	}, nil
}
