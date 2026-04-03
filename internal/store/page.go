package store

import "github.com/noopolis/moltnet/pkg/protocol"

type identified interface {
	GetID() string
}

type roomItem struct{ protocol.Room }
type agentItem struct{ protocol.AgentSummary }
type dmItem struct{ protocol.DirectConversation }

func (r roomItem) GetID() string  { return r.Room.ID }
func (a agentItem) GetID() string { return a.AgentSummary.ID }
func (d dmItem) GetID() string    { return d.DirectConversation.ID }

func paginateByID[T identified](values []T, page protocol.PageRequest) ([]T, protocol.PageInfo, error) {
	if err := protocol.ValidatePageRequest(page); err != nil {
		return nil, protocol.PageInfo{}, ErrInvalidCursor
	}

	limit := page.Limit
	if limit <= 0 {
		limit = len(values)
	}

	if page.After != "" {
		start, ok := indexAfter(values, page.After)
		if !ok {
			return nil, protocol.PageInfo{}, ErrInvalidCursor
		}
		end := start + limit
		if end > len(values) {
			end = len(values)
		}
		selected := append([]T(nil), values[start:end]...)
		info := protocol.PageInfo{
			HasMore: end < len(values),
		}
		if info.HasMore && len(selected) > 0 {
			info.NextAfter = selected[len(selected)-1].GetID()
		}
		if start > 0 && len(selected) > 0 {
			info.NextBefore = selected[0].GetID()
		}
		return selected, info, nil
	}

	end := len(values)
	if page.Before != "" {
		value, ok := indexBefore(values, page.Before)
		if !ok {
			return nil, protocol.PageInfo{}, ErrInvalidCursor
		}
		end = value
	}
	start := end - limit
	if start < 0 {
		start = 0
	}

	selected := append([]T(nil), values[start:end]...)
	info := protocol.PageInfo{
		HasMore: start > 0,
	}
	if start > 0 && len(selected) > 0 {
		info.NextBefore = selected[0].GetID()
	}
	if end < len(values) && len(selected) > 0 {
		info.NextAfter = selected[len(selected)-1].GetID()
	}

	return selected, info, nil
}

func indexAfter[T identified](values []T, after string) (int, bool) {
	for index, value := range values {
		if value.GetID() == after {
			return index + 1, true
		}
	}
	return 0, false
}

func indexBefore[T identified](values []T, before string) (int, bool) {
	for index, value := range values {
		if value.GetID() == before {
			return index, true
		}
	}
	return len(values), false
}
