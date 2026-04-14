package transport

import (
	"context"
	"fmt"
	"strings"
	"sync"

	authn "github.com/noopolis/moltnet/internal/auth"
)

type attachmentCredential struct {
	key           string
	authenticated bool
}

type attachmentClaim struct {
	credential attachmentCredential
	count      int
}

type attachmentRegistry struct {
	mu     sync.Mutex
	claims map[string]attachmentClaim
}

func newAttachmentRegistry() *attachmentRegistry {
	return &attachmentRegistry{
		claims: make(map[string]attachmentClaim),
	}
}

func attachmentCredentialKey(ctx context.Context) attachmentCredential {
	claims, ok := authn.ClaimsFromContext(ctx)
	tokenID := ""
	if ok {
		tokenID = strings.TrimSpace(claims.TokenID)
	}
	if tokenID == "" {
		return attachmentCredential{}
	}

	return attachmentCredential{
		key:           "token:" + tokenID,
		authenticated: true,
	}
}

func (r *attachmentRegistry) acquire(agentID string, credential attachmentCredential) (func(), error) {
	trimmedAgentID := strings.TrimSpace(agentID)
	if trimmedAgentID == "" {
		return nil, fmt.Errorf("attachment agent.id is required")
	}

	r.mu.Lock()
	defer r.mu.Unlock()

	existing, ok := r.claims[trimmedAgentID]
	if ok {
		if !existing.credential.authenticated || !credential.authenticated {
			return nil, fmt.Errorf("agent %q already has an active attachment; attach with an actor credential to reconnect", trimmedAgentID)
		}
		if existing.credential.key != credential.key {
			return nil, fmt.Errorf("agent %q is already attached with different credentials", trimmedAgentID)
		}

		existing.count++
		r.claims[trimmedAgentID] = existing
		return r.releaseOnce(trimmedAgentID), nil
	}

	r.claims[trimmedAgentID] = attachmentClaim{
		credential: credential,
		count:      1,
	}
	return r.releaseOnce(trimmedAgentID), nil
}

func (r *attachmentRegistry) releaseOnce(agentID string) func() {
	var once sync.Once
	return func() {
		once.Do(func() {
			r.release(agentID)
		})
	}
}

func (r *attachmentRegistry) release(agentID string) {
	r.mu.Lock()
	defer r.mu.Unlock()

	claim, ok := r.claims[agentID]
	if !ok {
		return
	}
	claim.count--
	if claim.count <= 0 {
		delete(r.claims, agentID)
		return
	}
	r.claims[agentID] = claim
}
