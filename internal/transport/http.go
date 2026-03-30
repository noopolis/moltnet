package transport

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"strconv"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type Service interface {
	Network() protocol.Network
	ListAgents() []protocol.AgentSummary
	ListPairings() []protocol.Pairing
	ListRooms() []protocol.Room
	CreateRoom(request protocol.CreateRoomRequest) (protocol.Room, error)
	ListRoomMessages(roomID string, before string, limit int) (protocol.MessagePage, error)
	ListDirectConversations() []protocol.DirectConversation
	ListDMMessages(dmID string, before string, limit int) (protocol.MessagePage, error)
	SendMessage(request protocol.SendMessageRequest) (protocol.MessageAccepted, error)
	Subscribe(ctx context.Context) <-chan protocol.Event
}

func NewHTTPHandler(service Service) http.Handler {
	mux := http.NewServeMux()
	attachUIRoutes(mux)

	mux.HandleFunc("GET /healthz", func(response http.ResponseWriter, request *http.Request) {
		writeJSON(response, http.StatusOK, map[string]string{
			"status": "ok",
		})
	})

	mux.HandleFunc("GET /v1/network", func(response http.ResponseWriter, request *http.Request) {
		writeJSON(response, http.StatusOK, service.Network())
	})

	mux.HandleFunc("GET /v1/rooms", func(response http.ResponseWriter, request *http.Request) {
		writeJSON(response, http.StatusOK, map[string]any{
			"rooms": service.ListRooms(),
		})
	})

	mux.HandleFunc("GET /v1/agents", func(response http.ResponseWriter, request *http.Request) {
		writeJSON(response, http.StatusOK, map[string]any{
			"agents": service.ListAgents(),
		})
	})

	mux.HandleFunc("GET /v1/pairings", func(response http.ResponseWriter, request *http.Request) {
		writeJSON(response, http.StatusOK, map[string]any{
			"pairings": service.ListPairings(),
		})
	})

	mux.HandleFunc("POST /v1/rooms", func(response http.ResponseWriter, request *http.Request) {
		var payload protocol.CreateRoomRequest
		if err := decodeJSON(request, &payload); err != nil {
			writeError(response, http.StatusBadRequest, err)
			return
		}

		room, err := service.CreateRoom(payload)
		if err != nil {
			writeError(response, http.StatusBadRequest, err)
			return
		}

		writeJSON(response, http.StatusCreated, room)
	})

	mux.HandleFunc("GET /v1/rooms/{roomID}/messages", func(response http.ResponseWriter, request *http.Request) {
		messages, err := service.ListRoomMessages(
			request.PathValue("roomID"),
			readBefore(request),
			readLimit(request),
		)
		if err != nil {
			writeError(response, http.StatusBadRequest, err)
			return
		}

		writeJSON(response, http.StatusOK, messages)
	})

	mux.HandleFunc("GET /v1/dms", func(response http.ResponseWriter, request *http.Request) {
		writeJSON(response, http.StatusOK, map[string]any{
			"dms": service.ListDirectConversations(),
		})
	})

	mux.HandleFunc("GET /v1/dms/{dmID}/messages", func(response http.ResponseWriter, request *http.Request) {
		messages, err := service.ListDMMessages(
			request.PathValue("dmID"),
			readBefore(request),
			readLimit(request),
		)
		if err != nil {
			writeError(response, http.StatusBadRequest, err)
			return
		}

		writeJSON(response, http.StatusOK, messages)
	})

	mux.HandleFunc("POST /v1/messages", func(response http.ResponseWriter, request *http.Request) {
		var payload protocol.SendMessageRequest
		if err := decodeJSON(request, &payload); err != nil {
			writeError(response, http.StatusBadRequest, err)
			return
		}

		accepted, err := service.SendMessage(payload)
		if err != nil {
			writeError(response, http.StatusBadRequest, err)
			return
		}

		writeJSON(response, http.StatusAccepted, accepted)
	})

	mux.HandleFunc("GET /v1/events/stream", func(response http.ResponseWriter, request *http.Request) {
		flusher, ok := response.(http.Flusher)
		if !ok {
			writeError(response, http.StatusInternalServerError, errors.New("streaming unsupported"))
			return
		}

		response.Header().Set("Content-Type", "text/event-stream")
		response.Header().Set("Cache-Control", "no-cache")
		response.Header().Set("Connection", "keep-alive")

		if _, err := fmt.Fprint(response, ": stream-open\n\n"); err != nil {
			return
		}
		flusher.Flush()

		stream := service.Subscribe(request.Context())
		for event := range stream {
			payload, err := json.Marshal(event)
			if err != nil {
				log.Printf("encode event: %v", err)
				continue
			}

			if _, err := fmt.Fprintf(response, "event: %s\n", event.Type); err != nil {
				return
			}

			if _, err := fmt.Fprintf(response, "data: %s\n\n", payload); err != nil {
				return
			}

			flusher.Flush()
		}
	})

	return mux
}

func readLimit(request *http.Request) int {
	value := strings.TrimSpace(request.URL.Query().Get("limit"))
	if value == "" {
		return 100
	}

	limit, err := strconv.Atoi(value)
	if err != nil || limit <= 0 {
		return 100
	}

	if limit > 500 {
		return 500
	}

	return limit
}

func readBefore(request *http.Request) string {
	return strings.TrimSpace(request.URL.Query().Get("before"))
}

func decodeJSON(request *http.Request, target any) error {
	defer request.Body.Close()

	decoder := json.NewDecoder(request.Body)
	decoder.DisallowUnknownFields()

	if err := decoder.Decode(target); err != nil {
		return err
	}

	return nil
}

func writeJSON(response http.ResponseWriter, status int, payload any) {
	response.Header().Set("Content-Type", "application/json")
	response.WriteHeader(status)

	if err := json.NewEncoder(response).Encode(payload); err != nil {
		log.Printf("write json response: %v", err)
	}
}

func writeError(response http.ResponseWriter, status int, err error) {
	writeJSON(response, status, map[string]string{
		"error": err.Error(),
	})
}

func writeMethodNotAllowed(response http.ResponseWriter, allowed ...string) {
	if len(allowed) > 0 {
		response.Header().Set("Allow", strings.Join(allowed, ", "))
	}

	writeError(response, http.StatusMethodNotAllowed, errors.New("method not allowed"))
}
