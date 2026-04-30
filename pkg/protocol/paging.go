package protocol

import "fmt"

type PageRequest struct {
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

type PageHasMoreMode int

const (
	PageHasMoreAnyDirection PageHasMoreMode = iota
	PageHasMoreCursorDirection
)

func ValidatePageRequest(page PageRequest) error {
	if page.Before != "" && page.After != "" {
		return fmt.Errorf("before and after cannot both be set")
	}
	if page.Before != "" {
		if err := ValidateMessageID(page.Before); err != nil {
			return fmt.Errorf("before %w", err)
		}
	}
	if page.After != "" {
		if err := ValidateMessageID(page.After); err != nil {
			return fmt.Errorf("after %w", err)
		}
	}
	return nil
}

func PaginateByID[T any](values []T, page PageRequest, id func(T) string) ([]T, PageInfo, error) {
	return PaginateByIDWithMode(values, page, id, PageHasMoreAnyDirection)
}

func PaginateByIDWithMode[T any](
	values []T,
	page PageRequest,
	id func(T) string,
	hasMoreMode PageHasMoreMode,
) ([]T, PageInfo, error) {
	if err := ValidatePageRequest(page); err != nil {
		return nil, PageInfo{}, err
	}

	limit := page.Limit
	if limit <= 0 {
		limit = len(values)
	}

	start := 0
	end := len(values)
	if page.After != "" {
		value, ok := indexAfter(values, page.After, id)
		if !ok {
			return nil, PageInfo{}, fmt.Errorf("invalid cursor %q", page.After)
		}
		start = value
	}
	if page.Before != "" {
		value, ok := indexBefore(values, page.Before, id)
		if !ok {
			return nil, PageInfo{}, fmt.Errorf("invalid cursor %q", page.Before)
		}
		end = value
	}
	if end < start {
		end = start
	}

	windowStart := start
	windowEnd := end
	if windowEnd-windowStart > limit {
		if page.Before != "" && page.After == "" {
			windowStart = windowEnd - limit
		} else {
			windowEnd = windowStart + limit
		}
	}

	selected := append([]T(nil), values[windowStart:windowEnd]...)
	info := PageInfo{HasMore: pageHasMore(hasMoreMode, page, windowStart, windowEnd, len(values))}
	if windowStart > 0 && len(selected) > 0 {
		info.NextBefore = id(selected[0])
	}
	if windowEnd < len(values) && len(selected) > 0 {
		info.NextAfter = id(selected[len(selected)-1])
	}

	return selected, info, nil
}

func pageHasMore(mode PageHasMoreMode, page PageRequest, windowStart int, windowEnd int, valueCount int) bool {
	if mode != PageHasMoreCursorDirection {
		return windowStart > 0 || windowEnd < valueCount
	}
	if page.After != "" {
		return windowEnd < valueCount
	}
	return windowStart > 0
}

func indexAfter[T any](values []T, after string, id func(T) string) (int, bool) {
	for index, value := range values {
		if id(value) == after {
			return index + 1, true
		}
	}
	return 0, false
}

func indexBefore[T any](values []T, before string, id func(T) string) (int, bool) {
	for index, value := range values {
		if id(value) == before {
			return index, true
		}
	}
	return len(values), false
}

type RoomPage struct {
	Rooms []Room   `json:"rooms"`
	Page  PageInfo `json:"page"`
}

type AgentPage struct {
	Agents []AgentSummary `json:"agents"`
	Page   PageInfo       `json:"page"`
}

type DirectConversationPage struct {
	DMs  []DirectConversation `json:"dms"`
	Page PageInfo             `json:"page"`
}

type ThreadPage struct {
	Threads []Thread `json:"threads"`
	Page    PageInfo `json:"page"`
}

type PairingPage struct {
	Pairings []Pairing `json:"pairings"`
	Page     PageInfo  `json:"page"`
}
