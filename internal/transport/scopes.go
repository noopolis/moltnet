package transport

import authn "github.com/noopolis/moltnet/internal/auth"

var (
	readScopes     = []authn.Scope{authn.ScopeObserve, authn.ScopeAdmin}
	roomListScopes = []authn.Scope{authn.ScopeObserve, authn.ScopeAdmin, authn.ScopePair}
	networkScopes  = []authn.Scope{authn.ScopeObserve, authn.ScopeAdmin, authn.ScopePair, authn.ScopeAttach}

	// joinRoomScopes is POST /v1/rooms/{roomID}/join's advertised scope set
	// (PLAN.md 7A.3). It used to also list ScopePair, which was misleading:
	// the handler (handleJoinRoom -> JoinRoomContext -> joinActorAuthorized,
	// internal/rooms/join.go) requires a local agent-restricted agent token
	// regardless of what scopes the route advertises, and a pair-scoped
	// token (minted for a peer network, never agent-restricted) can never
	// satisfy that check. NewAgentTokenClaims (internal/auth/policy.go)
	// grants agent tokens ScopeWrite (plus observe/attach, never pair), so
	// ScopeWrite alone is both necessary and sufficient here.
	joinRoomScopes = []authn.Scope{authn.ScopeWrite}
)
