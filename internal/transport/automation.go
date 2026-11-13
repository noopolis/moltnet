package transport

import (
	"context"
	"errors"
	"net/http"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type automationService interface {
	RoomAutomation(context.Context, string) (protocol.RoomAutomation, error)
	SetAgentWake(context.Context, string, protocol.AgentWakeSetting) error
	StartDiscussion(context.Context, string, protocol.StartDiscussionRequest) (protocol.RoomAutomation, error)
	StopDiscussion(context.Context, string) error
	SetAgentWebhook(context.Context, string, string, protocol.WebhookSetting) error
	TestAgentWebhook(context.Context, string, string) error
	AutomationWakeEligible(context.Context, string, string, string) (bool, error)
}

func registerAutomationRoutes(mux *http.ServeMux, policy *authn.Policy, service Service) {
	control, ok := service.(automationService)
	if !ok {
		return
	}
	// Both read and mutation controls require an unrestricted owner credential.
	owner := func(handler http.HandlerFunc) http.HandlerFunc {
		return authorizedWithVerifier(policy, service, authn.ScopeAdmin, func(w http.ResponseWriter, r *http.Request) {
			claims, ok := authn.ClaimsFromContext(r.Context())
			if !ok || !claims.Operator() || claims.AgentToken() || len(claims.AgentIDs()) != 0 || claims.Network() != "" {
				writeError(w, http.StatusForbidden, errors.New("仅本机主人可以控制 AI 讨论"))
				return
			}
			w.Header().Set("Cache-Control", "no-store")
			if r.Method != "GET" && !sameOriginConsolePost(r) && r.Header.Get("Authorization") == "" {
				writeError(w, http.StatusForbidden, errors.New("requires a same-origin request"))
				return
			}
			handler(w, r)
		})
	}
	base := "/v1/rooms/{roomID}/automation"
	mux.HandleFunc("GET "+base+"/discussion", authorizedAnyWithVerifier(policy, service, readScopes, func(w http.ResponseWriter, r *http.Request) {
		view, err := control.RoomAutomation(r.Context(), r.PathValue("roomID"))
		if err != nil {
			writeError(w, statusForError(err), err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, map[string]any{"discussion": view.Discussion})
	}))
	mux.HandleFunc("GET "+base, owner(func(w http.ResponseWriter, r *http.Request) {
		view, err := control.RoomAutomation(r.Context(), r.PathValue("roomID"))
		if err != nil {
			writeError(w, statusForError(err), err)
			return
		}
		writeJSON(w, 200, view)
	}))
	mux.HandleFunc("PUT "+base+"/agents/{agentID}", owner(func(w http.ResponseWriter, r *http.Request) {
		var request protocol.AgentWakeSetting
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, 400, err)
			return
		}
		request.AgentID = r.PathValue("agentID")
		if err := control.SetAgentWake(r.Context(), r.PathValue("roomID"), request); err != nil {
			writeError(w, 422, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	}))
	mux.HandleFunc("POST "+base+"/discussion", owner(func(w http.ResponseWriter, r *http.Request) {
		var request protocol.StartDiscussionRequest
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, 400, err)
			return
		}
		view, err := control.StartDiscussion(r.Context(), r.PathValue("roomID"), request)
		if err != nil {
			writeError(w, 422, err)
			return
		}
		writeJSON(w, 201, view)
	}))
	mux.HandleFunc("POST "+base+"/stop", owner(func(w http.ResponseWriter, r *http.Request) {
		if err := control.StopDiscussion(r.Context(), r.PathValue("roomID")); err != nil {
			writeError(w, 422, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"stopped": true})
	}))
	mux.HandleFunc("PUT "+base+"/agents/{agentID}/webhook", owner(func(w http.ResponseWriter, r *http.Request) {
		var request protocol.WebhookSetting
		if err := decodeJSON(w, r, &request); err != nil {
			writeError(w, 400, err)
			return
		}
		if err := control.SetAgentWebhook(r.Context(), r.PathValue("roomID"), r.PathValue("agentID"), request); err != nil {
			writeError(w, 422, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"saved": true})
	}))
	mux.HandleFunc("POST "+base+"/agents/{agentID}/webhook/test", owner(func(w http.ResponseWriter, r *http.Request) {
		if err := control.TestAgentWebhook(r.Context(), r.PathValue("roomID"), r.PathValue("agentID")); err != nil {
			writeError(w, 422, err)
			return
		}
		writeJSON(w, 200, map[string]bool{"accepted": true})
	}))
	mux.HandleFunc("GET "+base+"/wake", authorizedAnyWithVerifier(policy, service, readScopes, func(w http.ResponseWriter, r *http.Request) {
		id := r.URL.Query().Get("agent_id")
		if claims, ok := authn.ClaimsFromContext(r.Context()); ok && claims.AgentToken() {
			ids := claims.AgentIDs()
			if len(ids) != 1 || ids[0] != id {
				writeError(w, 403, errors.New("agent identity mismatch"))
				return
			}
		}
		allowed, err := control.AutomationWakeEligible(r.Context(), r.PathValue("roomID"), r.URL.Query().Get("message_id"), id)
		if err != nil {
			writeError(w, statusForError(err), err)
			return
		}
		w.Header().Set("Cache-Control", "no-store")
		writeJSON(w, 200, map[string]bool{"allowed": allowed})
	}))
}
