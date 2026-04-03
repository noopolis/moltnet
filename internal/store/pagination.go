package store

import "github.com/noopolis/moltnet/pkg/protocol"

type messageItem struct{ protocol.Message }

func (m messageItem) GetID() string { return m.Message.ID }

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

	items := make([]messageItem, 0, len(messages))
	for _, message := range messages {
		items = append(items, messageItem{Message: message})
	}
	selected, info, err := paginateByID(items, page)
	if err != nil {
		return protocol.MessagePage{}, err
	}
	values := make([]protocol.Message, 0, len(selected))
	for _, item := range selected {
		values = append(values, item.Message)
	}
	return protocol.MessagePage{
		Messages: values,
		Page:     info,
	}, nil
}
