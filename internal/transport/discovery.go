package transport

import (
	"bytes"
	_ "embed"
	"fmt"
	"net"
	"net/http"
	"sort"
	"strings"
	"text/template"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

//go:embed install.md.tmpl
var installMarkdownTemplate string

// installMarkdownHandler is the shared GET /install.md body. It is only
// ever wrapped differently by loopback-ness (attachDiscoveryRoutes) — the
// join guide it renders already adapts its content to auth mode, public
// read, and registration policy regardless of whether the caller reaching
// it authenticated.
func installMarkdownHandler(policy *authn.Policy, service Service) http.HandlerFunc {
	return func(response http.ResponseWriter, request *http.Request) {
		rooms, err := service.ListRoomsContext(request.Context(), protocol.PageRequest{Limit: 100})
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeMarkdown(response, renderInstallMarkdown(request, policy, service.Network(), rooms.Rooms))
	}
}

// installMarkdownRoute wraps next (installMarkdownHandler) so GET
// /install.md — the join guide, no secrets — is readable without a token
// from a caller that reached this process directly from this machine, with
// no reverse proxy in between (requestIsDirectLoopback), regardless of auth
// mode; every other caller goes through the same publicInOpen gate every
// other discovery route uses (open only when auth.public_read is set).
//
// Both branches route the request through requestWithAuthMode before
// calling next. This matters even on the anonymous branch: an earlier
// version of this carve-out registered the raw handler directly, which left
// authn.ModeFromContext defaulting to ModeNone for the whole render —
// including the downstream room-listing call the renderer makes — so the
// renderer treated every anonymous loopback caller as being on a fully
// unauthenticated network. On a bearer-protected network that meant a
// private room (e.g. "secret-ops") showed up in the anonymous install.md
// output even though GET /v1/rooms correctly 401'd the same caller.
// Carrying the real auth mode/public-read/registration through the context
// makes the renderer apply the network's actual room-visibility rules
// (internal/rooms/access_policy.go's canReadRoom) to this caller exactly as
// it would to any other unauthenticated one — see
// TestInstallMarkdownAnonymousLoopbackNeverLeaksPrivateRooms.
func installMarkdownRoute(policy *authn.Policy, service Service, next http.HandlerFunc) http.HandlerFunc {
	gated := publicInOpen(policy, service, readScopes, next)
	return func(response http.ResponseWriter, request *http.Request) {
		if requestIsDirectLoopback(policy, request) {
			request = requestWithAuthMode(policy, request)
			if claims, ok := loopbackClaims(policy, service, request); ok {
				next(response, request.WithContext(authn.WithClaims(request.Context(), claims)))
				return
			}
			next(response, request)
			return
		}
		gated(response, request)
	}
}

// loopbackClaims authenticates request's bearer token, if any, against
// readScopes for installMarkdownRoute's loopback carve-out (P2, PLAN.md
// phase 6c review). Before this fix the carve-out always rendered the
// anonymous branch on a direct-loopback request, even when it carried a
// perfectly valid operator/observer token — so an operator on loopback saw
// "Readable rooms: none declared" while the exact same token routed through
// a reverse proxy (the non-loopback path, `gated`) rendered the full list.
// This authenticates first and only falls back to the anonymous render
// (ok=false, never an error) when credentials are absent, malformed, don't
// match any configured/agent credential, or lack a read scope — the same
// set of cases a non-loopback caller with no Authorization header would
// anonymously fall back for.
func loopbackClaims(policy *authn.Policy, verifier agentTokenVerifier, request *http.Request) (authn.Claims, bool) {
	token, ok, err := authn.RequestToken(request)
	if err != nil || !ok {
		return authn.Claims{}, false
	}
	claims, ok, err := authenticateBearerToken(policy, verifier, request.Context(), token)
	if err != nil || !ok {
		return authn.Claims{}, false
	}
	if !claims.AllowsAny(readScopes) {
		return authn.Claims{}, false
	}
	return claims, true
}

// requestIsDirectLoopback reports whether request reached this process
// directly from a process on this machine, with nothing forwarding for it
// (PLAN.md phase 6a review, P1-2).
//
// This is deliberately a property of the request, not of server.listen_addr:
// a loopback TCP bind only restricts who can open the socket directly, and
// this repo's own guides (guides/public-open-networks.md,
// guides/securing-remote-agents.md) tell operators to bind loopback and put
// a reverse proxy in front for exactly that reason. Under that topology
// every request the app process sees genuinely does come from 127.0.0.1 —
// from the proxy, on behalf of the entire internet. A carve-out keyed on
// "the bind is loopback" would serve this join guide to anyone the proxy
// forwards for, with no warning. So this checks the request itself: the
// immediate peer must be loopback, it must carry no X-Forwarded-For or
// X-Real-IP (the standard signals a proxy sat in front of this request),
// and the operator must not have set server.trust_forwarded_proto — that
// flag is the operator's own declaration that a proxy is in the path, so
// its presence in config means this request could be proxied even when
// this particular hop happens to carry neither header.
func requestIsDirectLoopback(policy *authn.Policy, request *http.Request) bool {
	if request == nil {
		return false
	}
	if policy != nil && policy.TrustForwardedProto() {
		return false
	}
	if strings.TrimSpace(request.Header.Get("X-Forwarded-For")) != "" {
		return false
	}
	if strings.TrimSpace(request.Header.Get("X-Real-IP")) != "" {
		return false
	}
	return isLoopbackRemoteAddr(request.RemoteAddr)
}

// isLoopbackRemoteAddr reports whether remoteAddr (an http.Request.RemoteAddr
// value, "host:port") names a loopback address. An unparseable or empty
// value is treated as non-loopback — the conservative default, since this
// function only ever gates an anonymous-access carve-out.
func isLoopbackRemoteAddr(remoteAddr string) bool {
	host, _, err := net.SplitHostPort(strings.TrimSpace(remoteAddr))
	if err != nil {
		host = strings.TrimSpace(remoteAddr)
	}
	host = strings.Trim(host, "[]")
	if host == "" {
		return false
	}
	ip := net.ParseIP(host)
	if ip == nil {
		return false
	}
	return ip.IsLoopback()
}

func attachDiscoveryRoutes(mux *http.ServeMux, policy *authn.Policy, service Service) {
	mux.HandleFunc("GET /install.md", installMarkdownRoute(policy, service, installMarkdownHandler(policy, service)))

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
	lines := make([]string, 0, len(readableRooms)*2)
	for _, room := range readableRooms {
		wake := "mentions"
		if !room.CanWriteNow && !room.CanWriteAfterConnect {
			wake = "never"
		}
		lines = append(lines,
			"      - id: "+room.ID,
			"        wake: "+wake,
		)
	}
	return strings.Join(lines, "\n")
}

func dmsYAML(enabled bool) string {
	if !enabled {
		return "    dms:\n      enabled: false"
	}
	return "    dms:\n      enabled: true\n      wake: all"
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
