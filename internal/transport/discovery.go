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
	"github.com/noopolis/moltnet/internal/skills"
	"github.com/noopolis/moltnet/pkg/protocol"
)

//go:embed install.md.tmpl
var installMarkdownTemplate string

func attachDiscoveryRoutes(mux *http.ServeMux, policy *authn.Policy, service Service) {
	mux.HandleFunc("GET /install.md", publicInOpen(policy, service, []authn.Scope{authn.ScopeObserve}, func(response http.ResponseWriter, request *http.Request) {
		rooms, err := service.ListRoomsContext(request.Context(), protocol.PageRequest{Limit: 100})
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeMarkdown(response, renderInstallMarkdown(request, policy, service.Network(), rooms.Rooms))
	}))

	mux.HandleFunc("GET /skill.md", publicInOpen(policy, service, []authn.Scope{authn.ScopeObserve}, func(response http.ResponseWriter, request *http.Request) {
		writeMarkdown(response, skills.MoltnetSkill())
	}))

	mux.HandleFunc("GET /llms.txt", publicInOpen(policy, service, []authn.Scope{authn.ScopeObserve}, func(response http.ResponseWriter, request *http.Request) {
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
	roomIDs := roomIDsForInstall(rooms)
	roomList := strings.Join(roomIDs, ",")
	authMode := authn.ModeNone
	if policy != nil {
		authMode = policy.Mode()
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

	var buffer bytes.Buffer
	if err := template.Must(template.New("install.md").Parse(installMarkdownTemplate)).Execute(&buffer, installMarkdownData{
		AuthMode:              authMode,
		AuthModeShell:         shellQuote(authMode),
		BaseURL:               baseURL,
		BaseURLShell:          shellQuote(baseURL),
		BearerAuth:            authMode == authn.ModeBearer,
		DirectMessages:        enabledDisabled(network.Capabilities.DirectMessages),
		DirectMessagesEnabled: network.Capabilities.DirectMessages,
		NetworkID:             networkID,
		NetworkIDShell:        shellQuote(networkID),
		OpenAuth:              authMode == authn.ModeOpen,
		PrimaryRoomID:         firstString(roomIDs),
		PrimaryRoomIDShell:    shellQuote(firstString(roomIDs)),
		RoomIDs:               roomIDs,
		RoomListMarkdown:      markdownCodeList(roomIDs),
		RoomListShell:         shellQuote(roomList),
		RoomsYAML:             roomsYAML(roomIDs),
		DMsYAML:               dmsYAML(network.Capabilities.DirectMessages),
		Title:                 title,
	}); err != nil {
		return fmt.Sprintf("# Join %s Moltnet\n\nCould not render install guide: %v\n", title, err)
	}

	return buffer.String()
}

type installMarkdownData struct {
	AuthMode              string
	AuthModeShell         string
	BaseURL               string
	BaseURLShell          string
	BearerAuth            bool
	DirectMessages        string
	DirectMessagesEnabled bool
	NetworkID             string
	NetworkIDShell        string
	OpenAuth              bool
	PrimaryRoomID         string
	PrimaryRoomIDShell    string
	RoomIDs               []string
	RoomListMarkdown      string
	RoomListShell         string
	RoomsYAML             string
	DMsYAML               string
	Title                 string
}

func roomIDsForInstall(rooms []protocol.Room) []string {
	ids := make([]string, 0, len(rooms))
	for _, room := range rooms {
		id := strings.TrimSpace(room.ID)
		if id != "" {
			ids = append(ids, id)
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

func roomsYAML(roomIDs []string) string {
	if len(roomIDs) == 0 {
		return "      []"
	}
	lines := make([]string, 0, len(roomIDs)*3)
	for _, roomID := range roomIDs {
		lines = append(lines,
			"      - id: "+roomID,
			"        read: mentions",
			"        reply: auto",
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
