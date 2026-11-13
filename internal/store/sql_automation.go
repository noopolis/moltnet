package store

import "github.com/noopolis/moltnet/pkg/protocol"

type messageAutomation struct {
	Wake         *protocol.MessageWake `json:"wake,omitempty"`
	ReplyTo      string                `json:"reply_to,omitempty"`
	DiscussionID string                `json:"discussion_id,omitempty"`
}

func automationForMessage(message protocol.Message) messageAutomation {
	return messageAutomation{Wake: message.Wake, ReplyTo: message.ReplyTo, DiscussionID: message.DiscussionID}
}
