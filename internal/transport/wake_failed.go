package transport

import (
	"errors"
	"net/http"
	"strings"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// registerWakeFailedRoute wires POST /v1/agents/wake-failed into mux. Split
// out of NewHTTPHandler purely to keep http.go's route list scannable.
func registerWakeFailedRoute(mux *http.ServeMux, policy *authn.Policy, service Service) {
	mux.HandleFunc("POST /v1/agents/wake-failed", authorizedAnyWithVerifier(
		policy,
		service,
		[]authn.Scope{authn.ScopeWrite, authn.ScopePair},
		func(response http.ResponseWriter, request *http.Request) {
			handleReportWakeFailed(response, request, service)
		},
	))
}

// handleReportWakeFailed backs POST /v1/agents/wake-failed: a bridge
// control-delivery retry loop (internal/bridge/loop) reporting a genuinely
// permanent or retry-budget-exhausted wake failure for an event it already
// ACKed on its attachment socket. This is the terminal-failure counterpart
// to a normal control reply — it never tears down the attachment, it just
// records that this wake could not be delivered so the failure is
// ledger-visible instead of a silent skip.
//
// Identity follows the same declared-identity-plus-claims-scope pattern
// internal/transport/attach.go uses for an IDENTIFY frame's agent.id: the
// caller declares which agent it is reporting for, and if the request is
// authenticated to a specific agent (claims.AllowsAgent), the declared
// agent.id must be one the token is scoped to.
func handleReportWakeFailed(response http.ResponseWriter, request *http.Request, service Service) {
	var payload protocol.ReportWakeFailedRequest
	if err := decodeJSON(response, request, &payload); err != nil {
		writeError(response, http.StatusBadRequest, err)
		return
	}

	agentID := strings.TrimSpace(payload.AgentID)
	if agentID == "" {
		writeError(response, http.StatusBadRequest, errors.New("agent_id is required"))
		return
	}
	if payload.Event.Message == nil {
		writeError(response, http.StatusBadRequest, errors.New("event.message is required"))
		return
	}
	if claims, ok := authn.ClaimsFromContext(request.Context()); ok && !claims.AllowsAgent(agentID) {
		writeError(response, http.StatusForbidden, errors.New("caller is not allowed to report wake failures for this agent"))
		return
	}

	var causeErr error
	if message := strings.TrimSpace(payload.Error); message != "" {
		causeErr = errors.New(message)
	}

	service.AgentWakeFailed(
		request.Context(),
		protocol.Actor{Type: "agent", ID: agentID},
		payload.Event,
		causeErr,
		protocol.WakeFailureDetails{
			Attempts:       payload.Attempts,
			Classification: payload.Classification,
		},
	)

	writeJSON(response, http.StatusAccepted, struct{}{})
}
