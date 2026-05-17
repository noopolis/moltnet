package transport

import (
	"bytes"
	_ "embed"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"text/template"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

//go:embed skill.md.tmpl
var skillMarkdownTemplate string

var skillRouteScopes = []authn.Scope{
	authn.ScopeObserve,
	authn.ScopeWrite,
	authn.ScopeAdmin,
	authn.ScopeAttach,
}

func skillDiscoveryRoute(policy *authn.Policy, service Service) http.HandlerFunc {
	next := func(response http.ResponseWriter, request *http.Request) {
		rooms, roomsVisible, err := visibleSkillRooms(request, policy, service)
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeMarkdown(response, renderSkillMarkdown(request, policy, service.Network(), rooms, roomsVisible))
	}

	if policy == nil || !policy.Enabled() {
		return next
	}
	if policy.PublicRead() {
		return optionalAuthInOpen(policy, service, skillRouteScopes, next)
	}
	return authorizedAnyWithVerifier(policy, service, skillRouteScopes, next)
}

func visibleSkillRooms(
	request *http.Request,
	policy *authn.Policy,
	service Service,
) ([]protocol.Room, bool, error) {
	if policy == nil || !policy.Enabled() || policy.PublicRead() {
		page, err := service.ListRoomsContext(request.Context(), protocol.PageRequest{Limit: 100})
		return page.Rooms, true, err
	}

	claims, ok := authn.ClaimsFromContext(request.Context())
	if !ok || !claims.AllowsAny([]authn.Scope{authn.ScopeObserve, authn.ScopeAdmin}) {
		return nil, false, nil
	}
	page, err := service.ListRoomsContext(request.Context(), protocol.PageRequest{Limit: 100})
	return page.Rooms, true, err
}

func renderSkillMarkdown(
	request *http.Request,
	policy *authn.Policy,
	network protocol.Network,
	rooms []protocol.Room,
	roomsVisible bool,
) string {
	data := buildSkillMarkdownData(request, policy, network, rooms, roomsVisible)
	var buffer bytes.Buffer
	if err := template.Must(template.New("skill.md").Parse(skillMarkdownTemplate)).Execute(&buffer, data); err != nil {
		return fmt.Sprintf("# %s Moltnet Skill\n\nCould not render skill: %v\n", data.Title, err)
	}
	return buffer.String()
}

func buildSkillMarkdownData(
	request *http.Request,
	policy *authn.Policy,
	network protocol.Network,
	rooms []protocol.Room,
	roomsVisible bool,
) skillMarkdownData {
	authMode := skillAuthMode(policy)
	publicRead := authMode == authn.ModeNone
	registration := authn.AgentRegistrationDisabled
	if policy != nil {
		publicRead = policy.PublicRead()
		registration = policy.AgentRegistration()
	}
	registrationOpen := registration == authn.AgentRegistrationOpen
	claims, hasClaims := authn.ClaimsFromContext(request.Context())
	canReadProtected := authMode == authn.ModeNone ||
		(hasClaims && claims.AllowsAny([]authn.Scope{authn.ScopeObserve, authn.ScopeAdmin}))
	canRead := authMode == authn.ModeNone || publicRead ||
		canReadProtected
	canSend := authMode == authn.ModeNone || (hasClaims && claims.Allows(authn.ScopeWrite))
	canAdmin := authMode == authn.ModeNone || (hasClaims && claims.Allows(authn.ScopeAdmin))
	baseURL := requestBaseURL(request)
	networkID := strings.TrimSpace(network.ID)
	roomInfos := roomInfosForRequest(request, authMode, publicRead, registration, rooms)
	readRoomIDs := roomIDsReadable(roomInfos)
	roomIDs := readRoomIDs
	writeNowRoomIDs := roomIDsWritableNow(roomInfos)
	writeAfterConnectRoomIDs := roomIDsWithConnectWrite(roomInfos)
	canSendAfterConnect := registrationOpen && !canSend && len(writeAfterConnectRoomIDs) > 0
	roomList := strings.Join(roomIDs, ",")
	roomTarget := "room:<room-id>"
	if primary := firstString(readRoomIDs); primary != "" {
		roomTarget = "room:" + primary
	}
	writeRoomTarget := "room:<room-id>"
	if primary := firstString(writeNowRoomIDs); primary != "" {
		writeRoomTarget = "room:" + primary
	} else if primary := firstString(writeAfterConnectRoomIDs); primary != "" {
		writeRoomTarget = "room:" + primary
	}

	title := strings.TrimSpace(network.Name)
	if title == "" {
		title = networkID
	}

	return skillMarkdownData{
		AccessLabel:                          skillAccessLabel(publicRead, hasClaims, canRead, canSend, canAdmin),
		AccessSummary:                        skillAccessSummary(publicRead, hasClaims, canRead, canSend, canAdmin),
		AuthMode:                             authMode,
		BaseURL:                              baseURL,
		BaseURLShell:                         shellQuote(baseURL),
		BearerAuth:                           authMode == authn.ModeBearer,
		CanAdmin:                             canAdmin,
		CanRead:                              canRead,
		CanSendAfterConnect:                  canSendAfterConnect,
		CanSendNow:                           len(writeNowRoomIDs) > 0,
		CanReadDirectMessages:                network.Capabilities.DirectMessages && canReadProtected,
		CanSendDirectMessages:                network.Capabilities.DirectMessages && canSend,
		DirectMessages:                       enabledDisabled(network.Capabilities.DirectMessages),
		DirectMessagesEnabled:                network.Capabilities.DirectMessages,
		HasReadRoomTarget:                    len(readRoomIDs) > 0 || !roomsVisible,
		HasRooms:                             len(roomIDs) > 0,
		HasWriteRoomTarget:                   len(writeNowRoomIDs) > 0 || len(writeAfterConnectRoomIDs) > 0 || !roomsVisible,
		NetworkID:                            networkID,
		NetworkIDShell:                       shellQuote(networkID),
		OpenAuth:                             authMode == authn.ModeOpen,
		RegistrationOpen:                     registrationOpen,
		ReadRoomListMarkdown:                 markdownCodeList(readRoomIDs),
		RoomListMarkdown:                     markdownCodeList(roomIDs),
		RoomListOrPlaceholderShell:           skillRoomListShell(roomList),
		RoomsVisible:                         roomsVisible,
		RoomTargetShell:                      shellQuote(roomTarget),
		Title:                                title,
		WritableAfterConnectRoomListMarkdown: markdownCodeList(writeAfterConnectRoomIDs),
		WritableNowRoomListMarkdown:          markdownCodeList(writeNowRoomIDs),
		WriteRoomTargetShell:                 shellQuote(writeRoomTarget),
	}
}

type skillMarkdownData struct {
	AccessLabel                          string
	AccessSummary                        string
	AuthMode                             string
	BaseURL                              string
	BaseURLShell                         string
	BearerAuth                           bool
	CanAdmin                             bool
	CanReadDirectMessages                bool
	CanSendDirectMessages                bool
	CanRead                              bool
	CanSendAfterConnect                  bool
	CanSendNow                           bool
	DirectMessages                       string
	DirectMessagesEnabled                bool
	HasReadRoomTarget                    bool
	HasRooms                             bool
	HasWriteRoomTarget                   bool
	NetworkID                            string
	NetworkIDShell                       string
	OpenAuth                             bool
	RegistrationOpen                     bool
	ReadRoomListMarkdown                 string
	RoomListMarkdown                     string
	RoomListOrPlaceholderShell           string
	RoomsVisible                         bool
	RoomTargetShell                      string
	Title                                string
	WritableAfterConnectRoomListMarkdown string
	WritableNowRoomListMarkdown          string
	WriteRoomTargetShell                 string
}

func skillAuthMode(policy *authn.Policy) string {
	if policy == nil || !policy.Enabled() {
		return authn.ModeNone
	}
	return policy.Mode()
}

func skillAccessLabel(publicRead bool, hasClaims bool, canRead bool, canSend bool, canAdmin bool) string {
	switch {
	case publicRead && !hasClaims:
		return "public read"
	case canAdmin:
		return "admin"
	case canRead && canSend:
		return "read/write"
	case canRead:
		return "read-only"
	case canSend:
		return "write-only"
	default:
		return "limited"
	}
}

func skillAccessSummary(publicRead bool, hasClaims bool, canRead bool, canSend bool, canAdmin bool) string {
	switch {
	case publicRead && !hasClaims:
		return "public read access; registration availability is listed below"
	case canAdmin:
		return "admin access"
	case canRead && canSend:
		return "read and write access"
	case canRead:
		return "read-only access"
	case canSend:
		return "write-only access"
	default:
		return "limited access"
	}
}

func skillRoomListShell(roomList string) string {
	if strings.TrimSpace(roomList) == "" {
		return "'<room-id>'"
	}
	return shellQuote(roomList)
}

type roomAccessInfo struct {
	ID                   string
	Visibility           string
	WritePolicy          string
	CanRead              bool
	CanWriteNow          bool
	CanWriteAfterConnect bool
	Reason               string
}

func roomInfosForRequest(
	request *http.Request,
	authMode string,
	publicRead bool,
	registration string,
	rooms []protocol.Room,
) []roomAccessInfo {
	var claims authn.Claims
	hasClaims := false
	if request != nil {
		claims, hasClaims = authn.ClaimsFromContext(request.Context())
	}
	registrationOpen := registration == authn.AgentRegistrationOpen
	canAdminWrite := hasClaims && claims.Allows(authn.ScopeAdmin) && claims.Allows(authn.ScopeWrite)
	canStaticWrite := hasClaims && claims.StaticToken() && claims.Allows(authn.ScopeWrite)
	canAnonymousRead := authMode == authn.ModeNone || publicRead
	canObserve := authMode == authn.ModeNone ||
		(hasClaims && claims.AllowsAny([]authn.Scope{authn.ScopeObserve, authn.ScopeAdmin}))

	infos := make([]roomAccessInfo, 0, len(rooms))
	for _, room := range rooms {
		id := strings.TrimSpace(room.ID)
		if id == "" {
			continue
		}
		visibility := strings.TrimSpace(room.Visibility)
		if visibility == "" && publicRead {
			visibility = "public"
		}
		if visibility == "" {
			visibility = "private"
		}
		writePolicy := strings.TrimSpace(room.WritePolicy)
		if writePolicy == "" {
			writePolicy = "members"
		}

		info := roomAccessInfo{
			ID:          id,
			Visibility:  visibility,
			WritePolicy: writePolicy,
			CanRead:     canObserve || (canAnonymousRead && visibility == "public"),
			Reason:      strings.TrimSpace(accessReason(room.Access)),
		}
		if room.Access != nil {
			info.CanRead = room.Access.CanRead
			info.CanWriteNow = room.Access.CanWrite
		} else {
			info.CanWriteNow = inferredRoomWrite(claims, hasClaims, canAdminWrite, canStaticWrite, room)
		}
		info.CanWriteAfterConnect = !hasClaims && registrationOpen && writePolicy == "registered_agents"
		infos = append(infos, info)
	}
	return infos
}

func inferredRoomWrite(
	claims authn.Claims,
	hasClaims bool,
	canAdminWrite bool,
	canStaticWrite bool,
	room protocol.Room,
) bool {
	if canAdminWrite {
		return true
	}
	writePolicy := strings.TrimSpace(room.WritePolicy)
	if writePolicy == "" {
		writePolicy = "members"
	}
	if writePolicy == "operators" {
		return canStaticWrite
	}
	if hasClaims && claims.AgentToken() && writePolicy == "registered_agents" {
		return true
	}
	if hasClaims && claims.Allows(authn.ScopeWrite) && claims.HasAgentRestriction() {
		for _, member := range room.Members {
			if claims.AllowsAgent(member) {
				return true
			}
		}
	}
	return false
}

func accessReason(access *protocol.RoomAccess) string {
	if access == nil {
		return ""
	}
	return access.Reason
}

func roomIDsReadable(rooms []roomAccessInfo) []string {
	ids := make([]string, 0, len(rooms))
	for _, room := range rooms {
		if room.CanRead {
			ids = append(ids, room.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func roomIDsWritableNow(rooms []roomAccessInfo) []string {
	ids := make([]string, 0, len(rooms))
	for _, room := range rooms {
		if room.CanRead && room.CanWriteNow {
			ids = append(ids, room.ID)
		}
	}
	sort.Strings(ids)
	return ids
}
