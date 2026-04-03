package protocol

import "fmt"

type PageRequest struct {
	Before string `json:"before,omitempty"`
	After  string `json:"after,omitempty"`
	Limit  int    `json:"limit,omitempty"`
}

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
