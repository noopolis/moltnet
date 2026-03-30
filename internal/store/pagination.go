package store

import "github.com/noopolis/moltnet/pkg/protocol"

func pageMessages(messages []protocol.Message, before string, limit int) protocol.MessagePage {
	selected := make([]protocol.Message, 0)

	if limit <= 0 {
		limit = len(messages)
	}

	end := len(messages)
	if before != "" {
		for index, message := range messages {
			if message.ID == before {
				end = index
				break
			}
		}
	}

	if end < 0 {
		end = 0
	}

	start := 0
	if limit < end {
		start = end - limit
	}

	selected = append(selected, messages[start:end]...)

	page := protocol.PageInfo{
		HasMore: start > 0,
	}
	if page.HasMore && len(selected) > 0 {
		page.NextBefore = selected[0].ID
	}

	return protocol.MessagePage{
		Messages: selected,
		Page:     page,
	}
}
