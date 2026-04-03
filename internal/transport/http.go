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
	ListRoomMessagesContext(ctx context.Context, roomID string, page protocol.PageRequest) (protocol.MessagePage, error)
	ListThreadsContext(ctx context.Context, roomID string, page protocol.PageRequest) (protocol.ThreadPage, error)
	ListThreadMessagesContext(ctx context.Context, threadID string, page protocol.PageRequest) (protocol.MessagePage, error)
	ListDirectConversationsContext(ctx context.Context, page protocol.PageRequest) (protocol.DirectConversationPage, error)
	ListDMMessagesContext(ctx context.Context, dmID string, page protocol.PageRequest) (protocol.MessagePage, error)
	ListArtifactsContext(ctx context.Context, filter protocol.ArtifactFilter, page protocol.PageRequest) (protocol.ArtifactPage, error)
	SendMessageContext(ctx context.Context, request protocol.SendMessageRequest) (protocol.MessageAccepted, error)
	Subscribe(ctx context.Context) <-chan protocol.Event
	SubscribeFrom(ctx context.Context, lastEventID string) <-chan protocol.Event
}

func NewHTTPHandler(service Service, policy *authn.Policy) http.Handler {
	mux := http.NewServeMux()
	attachUIRoutes(mux, policy)
	sseLimiter := newStreamLimiter(defaultMaxSSESubscribers)

	mux.Handle("GET /metrics", authorized(policy, authn.ScopeAdmin, http.HandlerFunc(func(response http.ResponseWriter, request *http.Request) {
		observability.DefaultMetrics.ServeHTTP(response, request)
	})))

	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, request *http.Request) {
		if err := service.Health(request.Context()); err != nil {
			observability.DefaultMetrics.RecordStoreHealth(false)
			writeError(response, http.StatusServiceUnavailable, err)
			return
		}
		observability.DefaultMetrics.RecordStoreHealth(true)
		writeJSON(response, http.StatusOK, map[string]string{
			"status": "ok",
		})
	})
	mux.HandleFunc("GET /readyz", func(response http.ResponseWriter, request *http.Request) {
		if err := service.Health(request.Context()); err != nil {
			observability.DefaultMetrics.RecordStoreHealth(false)
			writeError(response, http.StatusServiceUnavailable, err)
			return
		}
		observability.DefaultMetrics.RecordStoreHealth(true)
		writeJSON(response, http.StatusOK, map[string]string{
			"status": "ready",
		})
	})

	mux.HandleFunc("GET /v1/network", authorizedAny(policy, []authn.Scope{authn.ScopeObserve, authn.ScopePair}, func(response http.ResponseWriter, request *http.Request) {
		writeJSON(response, http.StatusOK, service.Network())
	}))

	mux.HandleFunc("GET /v1/rooms", authorizedAny(policy, []authn.Scope{authn.ScopeObserve, authn.ScopePair}, func(response http.ResponseWriter, request *http.Request) {
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

	mux.HandleFunc("GET /v1/rooms/{roomID}", authorized(policy, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
		room, err := service.GetRoom(request.PathValue("roomID"))
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeJSON(response, http.StatusOK, room)
	}))

	mux.HandleFunc("GET /v1/agents", authorizedAny(policy, []authn.Scope{authn.ScopeObserve, authn.ScopePair}, func(response http.ResponseWriter, request *http.Request) {
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

	mux.HandleFunc("GET /v1/agents/{agentID}", authorized(policy, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
		agent, err := service.GetAgent(request.PathValue("agentID"))
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeJSON(response, http.StatusOK, agent)
	}))

	mux.HandleFunc("GET /v1/pairings", authorized(policy, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
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

	mux.HandleFunc("GET /v1/pairings/{pairingID}/network", authorized(policy, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
		network, err := service.PairingNetwork(request.Context(), request.PathValue("pairingID"))
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeJSON(response, http.StatusOK, network)
	}))

	mux.HandleFunc("GET /v1/pairings/{pairingID}/rooms", authorized(policy, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
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

	mux.HandleFunc("GET /v1/pairings/{pairingID}/agents", authorized(policy, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
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

	mux.HandleFunc("POST /v1/rooms", authorized(policy, authn.ScopeAdmin, func(response http.ResponseWriter, request *http.Request) {
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

	mux.HandleFunc("PATCH /v1/rooms/{roomID}/members", authorized(policy, authn.ScopeAdmin, func(response http.ResponseWriter, request *http.Request) {
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

	mux.HandleFunc("GET /v1/rooms/{roomID}/messages", authorized(policy, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
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

	mux.HandleFunc("GET /v1/rooms/{roomID}/threads", authorized(policy, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
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

	mux.HandleFunc("GET /v1/threads/{threadID}/messages", authorized(policy, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
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

	mux.HandleFunc("GET /v1/threads/{threadID}", authorized(policy, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
		thread, err := service.GetThread(request.PathValue("threadID"))
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeJSON(response, http.StatusOK, thread)
	}))

	mux.HandleFunc("GET /v1/dms", authorized(policy, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
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

	mux.HandleFunc("GET /v1/dms/{dmID}", authorized(policy, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
		dm, err := service.GetDirectConversation(request.PathValue("dmID"))
		if err != nil {
			writeError(response, statusForError(err), err)
			return
		}
		writeJSON(response, http.StatusOK, dm)
	}))

	mux.HandleFunc("GET /v1/dms/{dmID}/messages", authorized(policy, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
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

	mux.HandleFunc("GET /v1/artifacts", authorized(policy, authn.ScopeObserve, func(response http.ResponseWriter, request *http.Request) {
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

	mux.HandleFunc("POST /v1/messages", authorizedAny(policy, []authn.Scope{authn.ScopeWrite, authn.ScopePair}, func(response http.ResponseWriter, request *http.Request) {
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

	mux.HandleFunc("GET /v1/attach", authorized(policy, authn.ScopeAttach, func(response http.ResponseWriter, request *http.Request) {
		handleAttachment(response, request, service, policy)
	}))

	mux.HandleFunc("GET /v1/events/stream", authorized(policy, authn.ScopeObserve, handleEventStream(service, sseLimiter)))

	return withObservability(mux)
}
