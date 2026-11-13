package automation

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

var ErrDiscussionConflict = errors.New("discussion conflict")

func (m *Manager) Start(roomID, id, promptID string, request protocol.StartDiscussionRequest) error {
	request.Topic = strings.TrimSpace(request.Topic)
	if request.Topic == "" || len(request.Topic) > 8000 {
		return errors.New("请输入讨论主题，最多 8000 字节")
	}
	if len(request.Participants) < 2 || len(request.Participants) > 8 {
		return errors.New("请选择 2 到 8 个 AI")
	}
	if len(protocol.UniqueTrimmedStrings(request.Participants)) != len(request.Participants) {
		return errors.New("讨论成员不能重复")
	}
	if request.MaxRounds < 1 || request.MaxRounds > 20 {
		return errors.New("讨论轮数必须在 1 到 20 之间")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	room := m.room(roomID)
	if expired(room.Discussion) {
		room.Discussion.Active = false
		room.Discussion.Reason = "讨论等待超时"
	}
	if room.Discussion != nil && room.Discussion.Active {
		return errors.New("已有讨论进行中，请先停止")
	}
	old := room.Discussion
	room.Discussion = &protocol.Discussion{ID: id, Active: true, Topic: request.Topic, Participants: append([]string(nil), request.Participants...), MaxRounds: request.MaxRounds, ExpectedAgent: request.Participants[0], ExpectedReplyTo: promptID, UpdatedAt: time.Now().UTC()}
	if err := m.save(); err != nil {
		room.Discussion = old
		return err
	}
	return nil
}

// Apply serializes state transitions with the durable message append. The
// append callback must not call back into Manager. Duplicate appends roll back
// the proposed turn; crash recovery pauses discussions instead of replaying runs.
func (m *Manager) Apply(message *protocol.Message, members []string, appendMessage func() (bool, error)) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	room := m.room(message.Target.RoomID)
	var previous *protocol.Discussion
	if room.Discussion != nil {
		copy := *room.Discussion
		previous = &copy
	}
	message.Wake = &protocol.MessageWake{Recipients: []string{}}
	d := room.Discussion
	if expired(d) {
		d.Active, d.Reason = false, "超过 10 分钟未收到回复，讨论已停止"
	}
	if message.DiscussionID != "" {
		if d == nil || !d.Active || d.ID != message.DiscussionID || expired(d) {
			return fmt.Errorf("%w: 讨论已停止或已失效", ErrDiscussionConflict)
		}
		initial := message.ID == d.ExpectedReplyTo && message.From.Type == "human" && message.From.ID == "operator" && message.Origin.ReceivedVia == ""
		if !initial {
			if message.From.Type != "agent" || !message.From.CredentialBound || message.From.ID != d.ExpectedAgent || message.ReplyTo != d.ExpectedReplyTo || message.Origin.ReceivedVia != "" {
				return fmt.Errorf("%w: 此回复不属于当前讨论轮次，请重新读取讨论状态", ErrDiscussionConflict)
			}
			d.CompletedTurns++
			d.UpdatedAt = time.Now().UTC()
			if d.CompletedTurns >= d.MaxRounds*len(d.Participants) {
				d.Active, d.Reason, d.ExpectedAgent = false, "已达到设定轮数", ""
			} else {
				d.ExpectedAgent = d.Participants[d.CompletedTurns%len(d.Participants)]
				d.ExpectedReplyTo = message.ID
			}
		}
		message.Wake.DiscussionID = d.ID
		message.Wake.Turn, message.Wake.MaxTurns = d.CompletedTurns+1, d.MaxRounds*len(d.Participants)
		if d.Active {
			message.Wake.Recipients = []string{d.ExpectedAgent}
		}
	} else {
		if d != nil && d.Active && message.From.Type == "agent" && message.From.ID == d.ExpectedAgent {
			return fmt.Errorf("%w: 当前轮次必须携带 reply_to 和 discussion_id", ErrDiscussionConflict)
		}
		owner := message.From.Type == "human" && message.From.ID == "operator" && message.NetworkID == m.networkID && message.Origin.ReceivedVia == ""
		if owner && d != nil && d.Active {
			d.Active, d.Reason = false, "主人发送了新消息，讨论已停止"
		}
		for _, id := range members {
			if d != nil && d.Active {
				break
			}
			mode := room.Settings[id]
			if mode == "" {
				mode = "owner"
			}
			mentioned := contains(message.Mentions, protocol.AgentFQID(m.networkID, id)) || contains(message.Mentions, id)
			if (mode == "owner" && owner && (len(message.Mentions) == 0 || mentioned)) || (mode == "mentions" && mentioned) {
				if id != message.From.ID {
					message.Wake.Recipients = append(message.Wake.Recipients, id)
				}
			}
		}
	}
	if err := m.save(); err != nil {
		room.Discussion = previous
		return err
	}
	inserted, err := appendMessage()
	if err != nil || !inserted {
		room.Discussion = previous
		if saveErr := m.save(); saveErr != nil {
			return fmt.Errorf("rollback automation state: %w", saveErr)
		}
	}
	return err
}

func expired(d *protocol.Discussion) bool {
	return d != nil && d.Active && time.Since(d.UpdatedAt) >= 10*time.Minute
}

func (m *Manager) Eligible(message protocol.Message, agentID string) bool {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.eligible(message, agentID)
}

func (m *Manager) eligible(message protocol.Message, agentID string) bool {
	if message.Wake == nil || !contains(message.Wake.Recipients, agentID) {
		return false
	}
	room := m.room(message.Target.RoomID)
	if message.Wake.DiscussionID != "" {
		d := room.Discussion
		return d != nil && d.Active && d.ID == message.Wake.DiscussionID && d.ExpectedAgent == agentID && d.ExpectedReplyTo == message.ID && time.Since(d.UpdatedAt) < 10*time.Minute
	}
	return room.Settings[agentID] != "never"
}
