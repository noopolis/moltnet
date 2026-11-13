package rooms

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func (s *Service) RoomAutomation(ctx context.Context, id string) (protocol.RoomAutomation, error) {
	if s.automation == nil {
		return protocol.RoomAutomation{}, &Error{status: http.StatusNotFound, msg: "本机 AI 控制未启用"}
	}
	room, err := s.GetRoomContext(ctx, id)
	if err != nil {
		return protocol.RoomAutomation{}, err
	}
	return s.automation.View(id, room.Members), nil
}

func (s *Service) SetAgentWake(ctx context.Context, roomID string, setting protocol.AgentWakeSetting) error {
	room, err := s.GetRoomContext(ctx, roomID)
	if err != nil {
		return err
	}
	if s.automation == nil || !containsMember(room.Members, setting.AgentID) {
		return errors.New("请选择本房间已有的 AI")
	}
	return s.automation.SetMode(roomID, setting.AgentID, setting.Mode)
}

func containsMember(members []string, id string) bool {
	for _, member := range members {
		if member == id {
			return true
		}
	}
	return false
}

func (s *Service) StartDiscussion(ctx context.Context, roomID string, request protocol.StartDiscussionRequest) (protocol.RoomAutomation, error) {
	if s.automation == nil {
		return protocol.RoomAutomation{}, errors.New("本机 AI 控制未启用")
	}
	room, err := s.GetRoomContext(ctx, roomID)
	if err != nil {
		return protocol.RoomAutomation{}, err
	}
	for _, id := range request.Participants {
		if !containsMember(room.Members, id) {
			return protocol.RoomAutomation{}, fmt.Errorf("%s 不属于此房间", id)
		}
		agent, err := s.GetAgentContext(ctx, id)
		if err != nil {
			return protocol.RoomAutomation{}, err
		}
		if !agent.Connected && !s.automation.HasWebhook(id) {
			return protocol.RoomAutomation{}, fmt.Errorf("%s 未接通自动唤醒，请先连接监听器或配置 Webhook", id)
		}
	}
	id, promptID := newPrefixedID("discussion"), newPrefixedID("msg")
	if err := s.automation.Start(roomID, id, promptID, request); err != nil {
		return protocol.RoomAutomation{}, err
	}
	_, err = s.SendMessageContext(ctx, protocol.SendMessageRequest{
		ID: promptID, DiscussionID: id, From: protocol.Actor{Type: "human", ID: "operator", Name: "我"},
		Target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: roomID},
		Parts:  []protocol.Part{{Kind: "text", Text: request.Topic}},
	})
	if err != nil {
		_ = s.automation.Stop(roomID, "启动失败")
		return protocol.RoomAutomation{}, err
	}
	return s.RoomAutomation(ctx, roomID)
}

func (s *Service) StopDiscussion(ctx context.Context, roomID string) error {
	if _, err := s.GetRoomContext(ctx, roomID); err != nil {
		return err
	}
	if s.automation == nil {
		return errors.New("本机 AI 控制未启用")
	}
	return s.automation.Stop(roomID, "主人已停止讨论")
}

func (s *Service) SetAgentWebhook(ctx context.Context, roomID, agentID string, setting protocol.WebhookSetting) error {
	room, err := s.GetRoomContext(ctx, roomID)
	if err != nil {
		return err
	}
	if s.automation == nil || !containsMember(room.Members, agentID) {
		return errors.New("请选择本房间已有的 AI")
	}
	return s.automation.SetWebhook(agentID, setting)
}

func (s *Service) TestAgentWebhook(ctx context.Context, roomID, agentID string) error {
	room, err := s.GetRoomContext(ctx, roomID)
	if err != nil {
		return err
	}
	if s.automation == nil || !containsMember(room.Members, agentID) {
		return errors.New("请选择本房间已有的 AI")
	}
	return s.automation.TestWebhook(ctx, agentID)
}

func (s *Service) AutomationWakeEligible(ctx context.Context, roomID, messageID, agentID string) (bool, error) {
	if s.automation == nil {
		return false, nil
	}
	// One ingress per agent: an enabled, tested webhook owns delivery rather
	// than also starting the agent's attached CLI bridge.
	if s.automation.HasWebhook(agentID) {
		return false, nil
	}
	page, err := s.ListRoomMessagesContext(ctx, roomID, protocol.PageRequest{Limit: 100})
	if err != nil {
		return false, err
	}
	for _, message := range page.Messages {
		if message.ID == messageID {
			return s.automation.Eligible(message, agentID), nil
		}
	}
	return false, nil
}
