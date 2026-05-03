package transport

import (
	"context"
	"net/http"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type Service interface {
	Health(ctx context.Context) error
	Network() protocol.Network
	GetAgent(agentID string) (protocol.AgentSummary, error)
	GetDirectConversation(dmID string) (protocol.DirectConversation, error)
	GetRoom(roomID string) (protocol.Room, error)
	GetThread(threadID string) (protocol.Thread, error)
	ListAgentsContext(ctx context.Context, page protocol.PageRequest) (protocol.AgentPage, error)
	ListPairingsContext(ctx context.Context, page protocol.PageRequest) (protocol.PairingPage, error)
	PairingNetwork(ctx context.Context, pairingID string) (protocol.Network, error)
	PairingRoomsContext(ctx context.Context, pairingID string, page protocol.PageRequest) (protocol.RoomPage, error)
	PairingAgentsContext(ctx context.Context, pairingID string, page protocol.PageRequest) (protocol.AgentPage, error)
	ListRoomsContext(ctx context.Context, page protocol.PageRequest) (protocol.RoomPage, error)
	CreateRoomContext(ctx context.Context, request protocol.CreateRoomRequest) (protocol.Room, error)
	UpdateRoomMembers(ctx context.Context, roomID string, request protocol.UpdateRoomMembersRequest) (protocol.Room, error)
	RegisterAgentContext(ctx context.Context, request protocol.RegisterAgentRequest) (protocol.AgentRegistration, error)
	ListRoomMessagesContext(ctx context.Context, roomID string, page protocol.PageRequest) (protocol.MessagePage, error)
	ListThreadsContext(ctx context.Context, roomID string, page protocol.PageRequest) (protocol.ThreadPage, error)
	ListThreadMessagesContext(ctx context.Context, threadID string, page protocol.PageRequest) (protocol.MessagePage, error)
	ListDirectConversationsContext(ctx context.Context, page protocol.PageRequest) (protocol.DirectConversationPage, error)
	ListDMMessagesContext(ctx context.Context, dmID string, page protocol.PageRequest) (protocol.MessagePage, error)
	ListArtifactsContext(ctx context.Context, filter protocol.ArtifactFilter, page protocol.PageRequest) (protocol.ArtifactPage, error)
	SendMessageContext(ctx context.Context, request protocol.SendMessageRequest) (protocol.MessageAccepted, error)
	AuthenticateAgentTokenContext(ctx context.Context, token string) (authn.Claims, bool, error)
	Subscribe(ctx context.Context) <-chan protocol.Event
	SubscribeFrom(ctx context.Context, lastEventID string) <-chan protocol.Event
}

func NewHTTPHandler(service Service, policy *authn.Policy) http.Handler {
	mux := http.NewServeMux()
	attachUIRoutes(mux, policy, service)
	sseLimiter := newStreamLimiter(defaultMaxSSESubscribers)
	attachments := newAttachmentRegistry()

	mux.Handle("GET /metrics", authorizedWithVerifier(policy, service, authn.ScopeAdmin, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		observability.DefaultMetrics.ServeHTTP(response, request)
	})))

	mux.HandleFunc("GET /healthz", anonymousAllowedInOpen(policy, service, func(response http.ResponseWriter, request *http.Request) {
		if err := service.Health(request.Context()); err != nil {
			observability.DefaultMetrics.RecordStoreHealth(false)
			writeError(response, http.StatusServiceUnavailable, err)
			return
		}
		observability.DefaultMetrics.RecordStoreHealth(true)
		writeJSON(response, http.StatusOK, map[string]string{
			"status": "ok",
		})
	}))
	mux.HandleFunc("GET /readyz", anonymousAllowedInOpen(policy, service, func(response http.ResponseWriter, request *http.Request) {
		if err := service.Health(request.Context()); err != nil {
			observability.DefaultMetrics.RecordStoreHealth(false)
			writeError(response, http.StatusServiceUnavailable, err)
			return
		}
		observability.DefaultMetrics.RecordStoreHealth(true)
		writeJSON(response, http.StatusOK, map[string]string{
			"status": "ready",
		})
	}))

	mux.HandleFunc("GET /v1/network", publicInOpen(policy, service, []authn.Scope{authn.ScopeObserve, authn.ScopePair}, func(response http.ResponseWriter, request *http.Request) {
		writeJSON(response, http.StatusOK, service.Network())
	}))

	mux.HandleFunc("GET /v1/rooms", publicInOpen(policy, service, []authn.Scope{authn.ScopeObserve, authn.ScopePair}, func(response http.ResponseWriter, request *http.Request) {
		pageRequest, err := readPageRequest(request)
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err)
			return
		}
		page, err := service.ListRoomsContext(request.Context(), pageRequest)
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeJSON(response, http.StatusOK, page)
	}))

	mux.HandleFunc("GET /v1/rooms/{roomID}", publicInOpen(policy, service, []authn.Scope{authn.ScopeObserve}, func(response http.ResponseWriter, request *http.Request) {
		room, err := service.GetRoom(request.PathValue("roomID"))
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeJSON(response, http.StatusOK, room)
	}))

	mux.HandleFunc("GET /v1/agents", publicInOpen(policy, service, []authn.Scope{authn.ScopeObserve, authn.ScopePair}, func(response http.ResponseWriter, request *http.Request) {
		pageRequest, err := readPageRequest(request)
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err)
			return
		}
		page, err := service.ListAgentsContext(request.Context(), pageRequest)
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeJSON(response, http.StatusOK, page)
	}))

	mux.HandleFunc("GET /v1/agents/{agentID}", publicInOpen(policy, service, []authn.Scope{authn.ScopeObserve}, func(response http.ResponseWriter, request *http.Request) {
		agent, err := service.GetAgent(request.PathValue("agentID"))
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeJSON(response, http.StatusOK, agent)
	}))

	mux.HandleFunc("POST /v1/agents/register", handleRegisterAgent(service, policy))

	mux.HandleFunc("GET /v1/pairings", authorizedWithVerifier(policy, service, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
		pageRequest, err := readPageRequest(request)
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err)
			return
		}
		page, err := service.ListPairingsContext(request.Context(), pageRequest)
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeJSON(response, http.StatusOK, page)
	}))

	mux.HandleFunc("GET /v1/pairings/{pairingID}/network", authorizedWithVerifier(policy, service, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
		network, err := service.PairingNetwork(request.Context(), request.PathValue("pairingID"))
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeJSON(response, http.StatusOK, network)
	}))

	mux.HandleFunc("GET /v1/pairings/{pairingID}/rooms", authorizedWithVerifier(policy, service, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
		pageRequest, err := readPageRequest(request)
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err)
			return
		}
		rooms, err := service.PairingRoomsContext(request.Context(), request.PathValue("pairingID"), pageRequest)
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeJSON(response, http.StatusOK, rooms)
	}))

	mux.HandleFunc("GET /v1/pairings/{pairingID}/agents", authorizedWithVerifier(policy, service, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
		pageRequest, err := readPageRequest(request)
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err)
			return
		}
		agents, err := service.PairingAgentsContext(request.Context(), request.PathValue("pairingID"), pageRequest)
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeJSON(response, http.StatusOK, agents)
	}))

	mux.HandleFunc("POST /v1/rooms", authorizedWithVerifier(policy, service, authn.ScopeAdmin, func(response http.ResponseWriter, request *http.Request) {
		var payload protocol.CreateRoomRequest
		if err := decodeJSON(response, request, &payload); err != nil {
			writeError(response, http.StatusBadRequest, err)
			return
		}

		room, err := service.CreateRoomContext(request.Context(), payload)
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}

		writeJSON(response, http.StatusCreated, room)
	}))

	mux.HandleFunc("PATCH /v1/rooms/{roomID}/members", authorizedWithVerifier(policy, service, authn.ScopeAdmin, func(response http.ResponseWriter, request *http.Request) {
		var payload protocol.UpdateRoomMembersRequest
		if err := decodeJSON(response, request, &payload); err != nil {
			writeError(response, http.StatusBadRequest, err)
			return
		}

		room, err := service.UpdateRoomMembers(request.Context(), request.PathValue("roomID"), payload)
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}

		writeJSON(response, http.StatusOK, room)
	}))

	mux.HandleFunc("GET /v1/rooms/{roomID}/messages", publicInOpen(policy, service, []authn.Scope{authn.ScopeObserve}, func(response http.ResponseWriter, request *http.Request) {
		pageRequest, err := readPageRequest(request)
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err)
			return
		}
		messages, err := service.ListRoomMessagesContext(request.Context(), request.PathValue("roomID"), pageRequest)
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}

		writeJSON(response, http.StatusOK, messages)
	}))

	mux.HandleFunc("GET /v1/rooms/{roomID}/threads", publicInOpen(policy, service, []authn.Scope{authn.ScopeObserve}, func(response http.ResponseWriter, request *http.Request) {
		pageRequest, err := readPageRequest(request)
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err)
			return
		}
		threads, err := service.ListThreadsContext(request.Context(), request.PathValue("roomID"), pageRequest)
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}

		writeJSON(response, http.StatusOK, threads)
	}))

	mux.HandleFunc("GET /v1/threads/{threadID}/messages", publicInOpen(policy, service, []authn.Scope{authn.ScopeObserve}, func(response http.ResponseWriter, request *http.Request) {
		pageRequest, err := readPageRequest(request)
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err)
			return
		}
		messages, err := service.ListThreadMessagesContext(request.Context(), request.PathValue("threadID"), pageRequest)
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}

		writeJSON(response, http.StatusOK, messages)
	}))

	mux.HandleFunc("GET /v1/threads/{threadID}", publicInOpen(policy, service, []authn.Scope{authn.ScopeObserve}, func(response http.ResponseWriter, request *http.Request) {
		thread, err := service.GetThread(request.PathValue("threadID"))
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeJSON(response, http.StatusOK, thread)
	}))

	mux.HandleFunc("GET /v1/dms", authorizedWithVerifier(policy, service, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
		pageRequest, err := readPageRequest(request)
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err)
			return
		}
		dms, err := service.ListDirectConversationsContext(request.Context(), pageRequest)
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeJSON(response, http.StatusOK, dms)
	}))

	mux.HandleFunc("GET /v1/dms/{dmID}", authorizedWithVerifier(policy, service, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
		dm, err := service.GetDirectConversation(request.PathValue("dmID"))
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeJSON(response, http.StatusOK, dm)
	}))

	mux.HandleFunc("GET /v1/dms/{dmID}/messages", authorizedWithVerifier(policy, service, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
		pageRequest, err := readPageRequest(request)
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err)
			return
		}
		messages, err := service.ListDMMessagesContext(request.Context(), request.PathValue("dmID"), pageRequest)
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}

		writeJSON(response, http.StatusOK, messages)
	}))

	mux.HandleFunc("GET /v1/artifacts", authorizedWithVerifier(policy, service, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
		pageRequest, err := readPageRequest(request)
		if err != nil {
			writeError(response, http.StatusUnprocessableEntity, err)
			return
		}
		artifacts, err := service.ListArtifactsContext(request.Context(), protocol.ArtifactFilter{
			RoomID:   strings.TrimSpace(request.URL.Query().Get("room_id")),
			ThreadID: strings.TrimSpace(request.URL.Query().Get("thread_id")),
			DMID:     strings.TrimSpace(request.URL.Query().Get("dm_id")),
		}, pageRequest)
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}

		writeJSON(response, http.StatusOK, artifacts)
	}))

	mux.HandleFunc("POST /v1/messages", authorizedAnyWithVerifier(policy, service, []authn.Scope{authn.ScopeWrite, authn.ScopePair}, func(response http.ResponseWriter, request *http.Request) {
		var payload protocol.SendMessageRequest
		if err := decodeJSON(response, request, &payload); err != nil {
			writeError(response, http.StatusBadRequest, err)
			return
		}

		accepted, err := service.SendMessageContext(request.Context(), payload)
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}

		writeJSON(response, http.StatusAccepted, accepted)
	}))

	mux.HandleFunc("GET /v1/attach", authorizedAttach(policy, service, func(response http.ResponseWriter, request *http.Request) {
		handleAttachment(response, request, service, policy, attachments)
	}))

	mux.HandleFunc("GET /v1/events/stream", authorizedEventStream(policy, service, service, sseLimiter))

	return withObservability(mux)
}
