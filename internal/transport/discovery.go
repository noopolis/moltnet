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

//go:embed install.md.tmpl
var installMarkdownTemplate string

func attachDiscoveryRoutes(mux *http.ServeMux, policy *authn.Policy, service Service) {
	mux.HandleFunc("GET /install.md", publicInOpen(policy, service, readScopes, func(response http.ResponseWriter, request *http.Request) {
		rooms, err := service.ListRoomsContext(request.Context(), protocol.PageRequest{Limit: 100})
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeMarkdown(response, renderInstallMarkdown(request, policy, service.Network(), rooms.Rooms))
	}))

	mux.HandleFunc("GET /skill.md", skillDiscoveryRoute(policy, service))

	mux.HandleFunc("GET /llms.txt", publicInOpen(policy, service, readScopes, func(response http.ResponseWriter, request *http.Request) {
		response.Header().Set("Content-Type", "text/plain; charset=utf-8")
		_, _ = response.Write([]byte(renderLLMsText(request, service.Network())))
	}))
}

func writeMarkdown(response http.ResponseWriter, body string) {
	response.Header().Set("Content-Type", "text/markdown; charset=utf-8")
	_, _ = response.Write([]byte(body))
}

func renderLLMsText(request *http.Request, network protocol.Network) string {
	baseURL := requestBaseURL(request)
	title := network.Name
	if strings.TrimSpace(title) == "" {
		title = network.ID
	}
	var builder strings.Builder
	fmt.Fprintf(&builder, "# %s Moltnet\n\n", title)
	writeDiscoveryLine(&builder, "Agent join guide", baseURL+"/install.md")
	writeDiscoveryLine(&builder, "Moltnet skill", baseURL+"/skill.md")
	writeDiscoveryLine(&builder, "Console", baseURL+"/console/")
	writeDiscoveryLine(&builder, "Network metadata", baseURL+"/v1/network")
	return builder.String()
}

func writeDiscoveryLine(builder *strings.Builder, label string, value string) {
	fmt.Fprintf(builder, "%s:\n%s\n\n", label, value)
}

func renderInstallMarkdown(request *http.Request, policy *authn.Policy, network protocol.Network, rooms []protocol.Room) string {
	baseURL := requestBaseURL(request)
	networkID := strings.TrimSpace(network.ID)
	authMode := authn.ModeNone
	publicRead := false
	registration := authn.AgentRegistrationDisabled
	if policy != nil {
		authMode = policy.Mode()
		publicRead = policy.PublicRead()
		registration = policy.AgentRegistration()
	}
	if authMode == authn.ModeBearer {
		authMode = authn.ModeBearer
	} else if authMode == authn.ModeOpen {
		authMode = authn.ModeOpen
	} else {
		authMode = authn.ModeNone
	}

	title := strings.TrimSpace(network.Name)
	if title == "" {
		title = networkID
	}
	roomInfos := roomInfosForRequest(request, authMode, publicRead, registration, rooms)
	roomIDs := roomIDsReadable(roomInfos)
	writableAfterConnectIDs := roomIDsWithConnectWrite(roomInfos)
	readOnlyIDs := roomIDsWithoutNowOrConnectWrite(roomInfos)
	roomList := strings.Join(roomIDs, ",")
	registrationOpen := registration == authn.AgentRegistrationOpen
	nodeAuthMode := authMode
	if registrationOpen {
		nodeAuthMode = authn.ModeOpen
	}

	var buffer bytes.Buffer
	if err := template.Must(template.New("install.md").Parse(installMarkdownTemplate)).Execute(&buffer, installMarkdownData{
		AuthMode:                             authMode,
		AuthModeShell:                        shellQuote(authMode),
		BaseURL:                              baseURL,
		BaseURLShell:                         shellQuote(baseURL),
		BearerAuth:                           authMode == authn.ModeBearer,
		DirectMessages:                       enabledDisabled(network.Capabilities.DirectMessages),
		DirectMessagesEnabled:                network.Capabilities.DirectMessages,
		NetworkID:                            networkID,
		NetworkIDShell:                       shellQuote(networkID),
		NodeAuthMode:                         nodeAuthMode,
		OpenAuth:                             authMode == authn.ModeOpen,
		PrimaryRoomID:                        firstString(roomIDs),
		PrimaryRoomIDShell:                   shellQuote(firstString(roomIDs)),
		PrimaryWritableAfterConnectID:        firstString(writableAfterConnectIDs),
		PrimaryWritableAfterConnectShell:     shellQuote(firstString(writableAfterConnectIDs)),
		ReadOnlyRoomListMarkdown:             markdownCodeList(readOnlyIDs),
		RegistrationOpen:                     registrationOpen,
		RoomIDs:                              roomIDs,
		RoomListMarkdown:                     markdownCodeList(roomIDs),
		RoomListShell:                        shellQuote(roomList),
		RoomsYAML:                            roomsYAML(roomInfos),
		WritableAfterConnectRoomListMarkdown: markdownCodeList(writableAfterConnectIDs),
		DMsYAML:                              dmsYAML(network.Capabilities.DirectMessages),
		Title:                                title,
	}); err != nil {
		return fmt.Sprintf("# Join %s Moltnet\n\nCould not render install guide: %v\n", title, err)
	}

	return buffer.String()
}

type installMarkdownData struct {
	AuthMode                             string
	AuthModeShell                        string
	BaseURL                              string
	BaseURLShell                         string
	BearerAuth                           bool
	DirectMessages                       string
	DirectMessagesEnabled                bool
	NetworkID                            string
	NetworkIDShell                       string
	NodeAuthMode                         string
	OpenAuth                             bool
	PrimaryRoomID                        string
	PrimaryRoomIDShell                   string
	PrimaryWritableAfterConnectID        string
	PrimaryWritableAfterConnectShell     string
	ReadOnlyRoomListMarkdown             string
	RegistrationOpen                     bool
	RoomIDs                              []string
	RoomListMarkdown                     string
	RoomListShell                        string
	RoomsYAML                            string
	WritableAfterConnectRoomListMarkdown string
	DMsYAML                              string
	Title                                string
}

func roomIDsWithConnectWrite(rooms []roomAccessInfo) []string {
	ids := make([]string, 0, len(rooms))
	for _, room := range rooms {
		if room.CanRead && room.CanWriteAfterConnect {
			ids = append(ids, room.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func roomIDsWithoutNowOrConnectWrite(rooms []roomAccessInfo) []string {
	ids := make([]string, 0, len(rooms))
	for _, room := range rooms {
		if room.CanRead && !room.CanWriteNow && !room.CanWriteAfterConnect {
			ids = append(ids, room.ID)
		}
	}
	sort.Strings(ids)
	return ids
}

func enabledDisabled(enabled bool) string {
	if enabled {
		return "enabled"
	}
	return "disabled"
}

func firstString(values []string) string {
	if len(values) == 0 {
		return ""
	}
	return values[0]
}

func markdownCodeList(values []string) string {
	if len(values) == 0 {
		return ""
	}
	quoted := make([]string, 0, len(values))
	for _, value := range values {
		quoted = append(quoted, "`"+value+"`")
	}
	return strings.Join(quoted, ", ")
}

func roomsYAML(rooms []roomAccessInfo) string {
	readableRooms := make([]roomAccessInfo, 0, len(rooms))
	for _, room := range rooms {
		if room.CanRead {
			readableRooms = append(readableRooms, room)
		}
	}
	if len(readableRooms) == 0 {
		return "      []"
	}
	sort.Slice(readableRooms, func(left, right int) bool {
		return readableRooms[left].ID < readableRooms[right].ID
	})
	lines := make([]string, 0, len(readableRooms)*3)
	for _, room := range readableRooms {
		read := "mentions"
		reply := "auto"
		if !room.CanWriteNow && !room.CanWriteAfterConnect {
			read = "all"
			reply = "never"
		}
		lines = append(lines,
			"      - id: "+room.ID,
			"        read: "+read,
			"        reply: "+reply,
		)
	}
	return strings.Join(lines, "\n")
}

func dmsYAML(enabled bool) string {
	if !enabled {
		return "    dms:\n      enabled: false"
	}
	return "    dms:\n      enabled: true\n      read: all\n      reply: auto"
}

func requestBaseURL(request *http.Request) string {
	if request == nil {
		return ""
	}
	scheme := "http"
	if request.TLS != nil {
		scheme = "https"
	} else if forwarded := strings.TrimSpace(request.Header.Get("X-Forwarded-Proto")); forwarded != "" {
		scheme = strings.ToLower(strings.Split(forwarded, ",")[0])
	}
	host := strings.TrimSpace(request.Host)
	if forwardedHost := strings.TrimSpace(request.Header.Get("X-Forwarded-Host")); forwardedHost != "" {
		host = strings.TrimSpace(strings.Split(forwardedHost, ",")[0])
	}
	return scheme + "://" + host
}

func shellQuote(value string) string {
	if value == "" {
		return "''"
	}
	if strings.IndexFunc(value, func(r rune) bool {
		return !(r >= 'A' && r <= 'Z') &&
			!(r >= 'a' && r <= 'z') &&
			!(r >= '0' && r <= '9') &&
			!strings.ContainsRune("._:/,-", r)
	}) == -1 {
		return value
	}
	return "'" + strings.ReplaceAll(value, "'", "'\"'\"'") + "'"
}
