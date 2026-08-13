package transport

import authn "github.com/noopolis/moltnet/internal/auth"

var (
	readScopes     = []authn.Scope{authn.ScopeObserve, authn.ScopeAdmin}
	roomListScopes = []authn.Scope{authn.ScopeObserve, authn.ScopeAdmin, authn.ScopePair}
	networkScopes  = []authn.Scope{authn.ScopeObserve, authn.ScopeAdmin, authn.ScopePair, authn.ScopeAttach}
)
