package machine

import (
	"context"
	"strings"

	"github.com/noopolis/moltnet/pkg/clientconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const maxProviderDMs = protocol.MaxMembersPerRequest

type attachmentAuthority struct {
	agentName, memberID, networkID string
	rooms                          []roomAuthority
	dmConfigured, dmEnabled        bool
	peers                          []string
}

type roomAuthority struct {
	id       string
	canWrite *bool
	access   *roomAccessAuthority
}

type roomAccessAuthority struct{ canRead, canWrite bool }

type resolvedTarget struct {
	machine protocol.MachineTarget
	target  protocol.Target
	dm      *protocol.DirectConversation
}

type targetResolver struct {
	authority attachmentAuthority
	provider  Provider
}

// projectAuthority deliberately omits every transport, credential, runtime, and
// wake/config field. It also owns every pointer and slice it retains.
func projectAuthority(attachment clientconfig.AttachmentConfig) attachmentAuthority {
	authority := attachmentAuthority{
		agentName: attachment.AgentName, memberID: attachment.MemberID, networkID: attachment.NetworkID,
		dmConfigured: attachment.DMs != nil,
		peers:        append([]string(nil), attachment.OutboundDMPEers...),
		rooms:        make([]roomAuthority, len(attachment.Rooms)),
	}
	if attachment.DMs != nil {
		authority.dmEnabled = attachment.DMs.Enabled
	}
	for index, binding := range attachment.Rooms {
		copy := roomAuthority{id: binding.ID}
		if binding.CanWrite != nil {
			value := *binding.CanWrite
			copy.canWrite = &value
		}
		if binding.Access != nil {
			copy.access = &roomAccessAuthority{canRead: binding.Access.CanRead, canWrite: binding.Access.CanWrite}
		}
		authority.rooms[index] = copy
	}
	return authority
}

func (resolver targetResolver) resolve(ctx context.Context, target protocol.MachineTarget, write bool) (resolvedTarget, error) {
	if err := target.Validate(); err != nil {
		return resolvedTarget{}, err
	}
	if !localMemberID(resolver.authority.memberID) || !localNetworkID(resolver.authority.networkID) {
		return resolvedTarget{}, errProviderInvalid
	}
	switch target.Kind {
	case protocol.MachineTargetKindRoom:
		return resolver.resolveRoom(ctx, target, write)
	case protocol.MachineTargetKindDM:
		return resolver.resolveDM(ctx, target)
	default:
		return resolvedTarget{}, errProviderInvalid
	}
}

func (resolver targetResolver) resolveRoom(ctx context.Context, target protocol.MachineTarget, write bool) (resolvedTarget, error) {
	binding, ok := declaredRoom(resolver.authority.rooms, target.ID)
	if !ok || !bindingAllows(binding, write) {
		return resolvedTarget{}, errProviderDenied
	}
	room, err := resolver.provider.GetRoom(ctx, target.ID)
	if err != nil {
		return resolvedTarget{}, err
	}
	if room.ID != target.ID || protocol.ValidateRoomID(room.ID) != nil || room.NetworkID != resolver.authority.networkID ||
		!localNetworkID(room.NetworkID) || room.Access == nil || !roomAllows(room, write) ||
		!validRoomAuthority(room) {
		return resolvedTarget{}, errProviderDenied
	}
	if roomRequiresMember(room, write) && semanticMemberCount(room.Members, resolver.authority.networkID, resolver.authority.memberID) != 1 {
		return resolvedTarget{}, errProviderDenied
	}
	return resolvedTarget{machine: target, target: protocol.Target{Kind: protocol.TargetKindRoom, RoomID: room.ID}}, nil
}

func (resolver targetResolver) resolveDM(ctx context.Context, target protocol.MachineTarget) (resolvedTarget, error) {
	if !resolver.authority.dmConfigured || !resolver.authority.dmEnabled || target.ID == resolver.authority.memberID || !localMemberID(target.ID) {
		return resolvedTarget{}, errProviderDenied
	}
	if !validOutboundPeers(resolver.authority.peers, resolver.authority.memberID) || !hasExactlyOnce(resolver.authority.peers, target.ID) {
		return resolvedTarget{}, errProviderDenied
	}
	page, err := resolver.provider.ListDMs(ctx)
	if err != nil {
		return resolvedTarget{}, err
	}
	if page.Page.HasMore || page.Page.NextBefore != "" || page.Page.NextAfter != "" || len(page.DMs) > maxProviderDMs {
		return resolvedTarget{}, errProviderDenied
	}
	var selected *protocol.DirectConversation
	for index := range page.DMs {
		dm := page.DMs[index]
		if !validListedDM(dm, resolver.authority.networkID, resolver.authority.memberID) {
			return resolvedTarget{}, errProviderDenied
		}
		if !hasExactlyOnce(dm.ParticipantIDs, target.ID) {
			continue
		}
		if selected != nil {
			return resolvedTarget{}, errProviderDenied
		}
		copy := dm
		copy.ParticipantIDs = append([]string(nil), dm.ParticipantIDs...)
		selected = &copy
	}
	if selected == nil {
		return resolvedTarget{}, errProviderDenied
	}
	return resolvedTarget{machine: target, target: protocol.Target{Kind: protocol.TargetKindDM, DMID: selected.ID,
		ParticipantIDs: append([]string(nil), selected.ParticipantIDs...)}, dm: selected}, nil
}

func declaredRoom(rooms []roomAuthority, id string) (roomAuthority, bool) {
	var found *roomAuthority
	for index := range rooms {
		if rooms[index].id != id {
			continue
		}
		if found != nil {
			return roomAuthority{}, false
		}
		copy := rooms[index]
		found = &copy
	}
	if found == nil {
		return roomAuthority{}, false
	}
	return *found, true
}

func bindingAllows(binding roomAuthority, write bool) bool {
	if binding.access != nil && ((write && !binding.access.canWrite) || (!write && !binding.access.canRead)) {
		return false
	}
	return !write || binding.canWrite == nil || *binding.canWrite
}

func roomAllows(room protocol.Room, write bool) bool {
	return (!write && room.Access.CanRead) || (write && room.Access.CanWrite)
}

func validRoomAuthority(room protocol.Room) bool {
	return strings.TrimSpace(room.Visibility) == room.Visibility && strings.TrimSpace(room.WritePolicy) == room.WritePolicy &&
		protocol.ValidateRoomVisibility(room.Visibility) == nil && protocol.ValidateRoomWritePolicy(room.WritePolicy) == nil &&
		validRoomMembers(room.Members, room.NetworkID)
}

func roomRequiresMember(room protocol.Room, write bool) bool {
	if !write {
		return room.Visibility != protocol.RoomVisibilityPublic
	}
	return room.WritePolicy != protocol.RoomWritePolicyRegisteredAgents && room.WritePolicy != protocol.RoomWritePolicyOperators
}

func validRoomMembers(members []string, network string) bool {
	if len(members) > protocol.MaxMembersPerRequest {
		return false
	}
	seen := make(map[string]struct{}, len(members))
	for _, member := range members {
		key, ok := canonicalRoomMember(member, network)
		if !ok {
			return false
		}
		if _, exists := seen[key]; exists {
			return false
		}
		seen[key] = struct{}{}
	}
	return true
}

func canonicalRoomMember(member, defaultNetwork string) (string, bool) {
	if member == "" || strings.TrimSpace(member) != member {
		return "", false
	}
	if scopedNetwork, id, ok := protocol.ParseScopedAgentID(member); ok {
		if localNetworkID(scopedNetwork) && localMemberID(id) {
			return scopedNetwork + "\x00" + id, true
		}
		return "", false
	}
	if fqidNetwork, id, ok := protocol.ParseAgentFQID(member); ok {
		if localNetworkID(fqidNetwork) && localMemberID(id) {
			return fqidNetwork + "\x00" + id, true
		}
		return "", false
	}
	if localNetworkID(defaultNetwork) && localMemberID(member) {
		return defaultNetwork + "\x00" + member, true
	}
	return "", false
}

func semanticMemberCount(values []string, network, wanted string) int {
	count := 0
	for _, value := range values {
		key, ok := canonicalRoomMember(value, network)
		if ok && key == network+"\x00"+wanted {
			count++
		}
	}
	return count
}

func validListedDM(dm protocol.DirectConversation, network, self string) bool {
	if dm.ID == "" || protocol.ValidateMessageID(dm.ID) != nil || dm.NetworkID == "" || dm.NetworkID != network ||
		len(dm.ParticipantIDs) != 2 || !localMemberID(self) {
		return false
	}
	if !localMemberID(dm.ParticipantIDs[0]) || !localMemberID(dm.ParticipantIDs[1]) || dm.ParticipantIDs[0] == dm.ParticipantIDs[1] {
		return false
	}
	return hasExactlyOnce(dm.ParticipantIDs, self)
}

func hasExactlyOnce(values []string, wanted string) bool {
	count := 0
	for _, value := range values {
		if value == wanted {
			count++
		}
	}
	return count == 1
}

func validOutboundPeers(peers []string, self string) bool {
	if len(peers) > clientconfig.MaxOutboundDMPeers {
		return false
	}
	seen := make(map[string]struct{}, len(peers))
	for _, peer := range peers {
		if !localMemberID(peer) || peer == self {
			return false
		}
		if _, duplicate := seen[peer]; duplicate {
			return false
		}
		seen[peer] = struct{}{}
	}
	return true
}

func localMemberID(value string) bool {
	if value == "" || strings.TrimSpace(value) != value {
		return false
	}
	if _, _, scoped := protocol.ParseScopedAgentID(value); scoped {
		return false
	}
	if _, _, fqid := protocol.ParseAgentFQID(value); fqid {
		return false
	}
	return protocol.ValidateMemberID(value) == nil
}

func localNetworkID(value string) bool {
	return value != "" && strings.TrimSpace(value) == value && protocol.ValidateMessageID(value) == nil
}
