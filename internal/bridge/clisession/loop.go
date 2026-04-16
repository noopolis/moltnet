package clisession

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	bridgeutil "github.com/noopolis/moltnet/internal/bridge"
	"github.com/noopolis/moltnet/internal/bridge/loop"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

type Driver interface {
	Name() string
	DefaultCommand() string
	UsesSessionIDForFirstDelivery() bool
	BuildCommand(config bridgeconfig.Config, delivery Delivery) (CommandSpec, error)
	ExtractRuntimeSessionID(result CommandResult) string
}

type Delivery struct {
	ContextKey       string
	RuntimeSessionID string
	ExistingSession  bool
	Prompt           string
	Bootstrap        bool
	Target           protocol.Target
	MessageID        string
}

type eventStreamer interface {
	StreamEvents(
		ctx context.Context,
		config bridgeconfig.Config,
		handle func(event protocol.Event) error,
	) error
}

type backoffPolicy interface {
	Delay(attempt int) time.Duration
}

type Runner struct {
	config   bridgeconfig.Config
	driver   Driver
	streamer eventStreamer
	backoff  backoffPolicy
	store    *SessionStore
	locks    map[string]*sync.Mutex
	mu       sync.Mutex
}

func NewRunner(
	config bridgeconfig.Config,
	driver Driver,
	streamer eventStreamer,
	backoff backoffPolicy,
) *Runner {
	storePath := strings.TrimSpace(config.Runtime.SessionStorePath)
	if storePath == "" {
		storePath = DefaultSessionStorePath(config.Runtime.WorkspacePath)
	}

	return &Runner{
		config:   config,
		driver:   driver,
		streamer: streamer,
		backoff:  backoff,
		store:    NewSessionStore(storePath),
		locks:    map[string]*sync.Mutex{},
	}
}

func Run(
	ctx context.Context,
	config bridgeconfig.Config,
	driver Driver,
	streamer eventStreamer,
	backoff backoffPolicy,
) error {
	return NewRunner(config, driver, streamer, backoff).Run(ctx)
}

func (r *Runner) Run(ctx context.Context) error {
	attempt := 0
	bootstrapped := false

	for {
		if ctx.Err() != nil {
			return nil
		}

		if !bootstrapped {
			if err := r.sendBootstrapDeliveries(ctx); err != nil {
				attempt++
				observability.Logger(ctx, "bridge."+r.driver.Name(), "agent_id", r.config.Agent.ID, "error", err).
					Warn("CLI bridge bootstrap error")

				select {
				case <-ctx.Done():
					return nil
				case <-time.After(r.backoff.Delay(attempt)):
				}
				continue
			}
			bootstrapped = true
		}

		err := r.streamer.StreamEvents(ctx, r.config, func(event protocol.Event) error {
			if !loop.ShouldHandle(r.config, event) {
				return nil
			}
			return r.sendEventDelivery(ctx, event)
		})
		if err == nil || ctx.Err() != nil {
			return err
		}
		attempt++

		observability.Logger(ctx, "bridge."+r.driver.Name(), "agent_id", r.config.Agent.ID, "error", err).
			Warn("CLI bridge inbound stream error")

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(r.backoff.Delay(attempt)):
		}
	}
}

func (r *Runner) sendBootstrapDeliveries(ctx context.Context) error {
	for _, target := range BootstrapTargets(r.config) {
		prompt := bridgeutil.RenderCompactBootstrapMessage(r.config.Moltnet.NetworkID, target, true)
		if err := r.dispatch(ctx, Delivery{
			ContextKey: SessionKeyForTarget(r.config, target),
			Prompt:     prompt,
			Bootstrap:  true,
			Target:     target,
		}); err != nil {
			return err
		}
	}
	return nil
}

func (r *Runner) sendEventDelivery(ctx context.Context, event protocol.Event) error {
	if event.Message == nil {
		return fmt.Errorf("event has no message")
	}

	return r.dispatch(ctx, Delivery{
		ContextKey: SessionKey(r.config, event.Message),
		Prompt: bridgeutil.RenderCompactInboundMessage(
			r.config.Moltnet.NetworkID,
			event.Message,
			true,
		),
		Target:    event.Message.Target,
		MessageID: event.Message.ID,
	})
}

func (r *Runner) dispatch(ctx context.Context, delivery Delivery) error {
	contextKey := strings.TrimSpace(delivery.ContextKey)
	if contextKey == "" {
		contextKey = "main"
	}

	lock := r.lockFor(contextKey)
	lock.Lock()
	defer lock.Unlock()

	record, exists, err := r.store.Get(contextKey)
	if err != nil {
		return err
	}

	sessionID := record.RuntimeSessionID
	if !exists && r.driver.UsesSessionIDForFirstDelivery() {
		sessionID, err = GenerateUUID()
		if err != nil {
			return err
		}
	}

	delivery.ContextKey = contextKey
	delivery.ExistingSession = exists
	delivery.RuntimeSessionID = sessionID

	spec, err := r.driver.BuildCommand(r.config, delivery)
	if err != nil {
		return err
	}
	if strings.TrimSpace(spec.Command) == "" {
		spec.Command = r.driver.DefaultCommand()
	}
	if strings.TrimSpace(spec.Dir) == "" {
		spec.Dir = strings.TrimSpace(r.config.Runtime.WorkspacePath)
	}
	spec.Env = append(BaseEnv(r.config), spec.Env...)
	if workspacePath := strings.TrimSpace(r.config.Runtime.WorkspacePath); workspacePath != "" {
		if err := os.MkdirAll(filepath.Join(workspacePath, ".moltnet"), 0o700); err != nil {
			return fmt.Errorf("create runtime Moltnet workspace directory: %w", err)
		}
	}

	result, err := RunCommand(ctx, spec)
	if err != nil {
		return err
	}

	extractedSessionID := strings.TrimSpace(r.driver.ExtractRuntimeSessionID(result))
	if extractedSessionID != "" {
		sessionID = extractedSessionID
	}
	if sessionID == "" {
		sessionID, err = GenerateUUID()
		if err != nil {
			return err
		}
	}
	if !exists || extractedSessionID != "" {
		if _, err := r.store.Put(contextKey, sessionID); err != nil {
			return err
		}
	}

	if output := strings.TrimSpace(result.Stdout); output != "" {
		observability.Logger(ctx, "bridge."+r.driver.Name(), "agent_id", r.config.Agent.ID, "output", output).
			Debug("CLI runtime command completed")
	}
	return nil
}

func (r *Runner) lockFor(contextKey string) *sync.Mutex {
	r.mu.Lock()
	defer r.mu.Unlock()

	lock, ok := r.locks[contextKey]
	if !ok {
		lock = &sync.Mutex{}
		r.locks[contextKey] = lock
	}
	return lock
}

func BootstrapTargets(config bridgeconfig.Config) []protocol.Target {
	targets := make([]protocol.Target, 0, len(config.Rooms))
	for _, binding := range config.Rooms {
		if strings.TrimSpace(binding.ID) == "" ||
			!bridgeutil.ShouldReply(binding.Reply) ||
			binding.Read == bridgeconfig.ReadThreadOnly ||
			binding.Read == bridgeconfig.ReadMentions {
			continue
		}

		targets = append(targets, protocol.Target{
			Kind:   protocol.TargetKindRoom,
			RoomID: binding.ID,
		})
	}
	return targets
}

func SessionKey(config bridgeconfig.Config, message *protocol.Message) string {
	if message == nil {
		return SessionKeyFromContext(config, "")
	}
	return SessionKeyFromContext(config, ConversationContextIDForTarget(config.Moltnet.NetworkID, message.Target, message.ID))
}

func SessionKeyForTarget(config bridgeconfig.Config, target protocol.Target) string {
	return SessionKeyFromContext(config, ConversationContextIDForTarget(config.Moltnet.NetworkID, target))
}

func SessionKeyFromContext(config bridgeconfig.Config, contextID string) string {
	trimmed := strings.TrimPrefix(strings.TrimSpace(contextID), "moltnet:")
	if trimmed == "" {
		trimmed = "main"
	}

	prefix := strings.TrimSpace(config.Runtime.SessionPrefix)
	if prefix == "" {
		prefix = "agent:" + strings.TrimSpace(config.Agent.ID)
	}
	return prefix + ":" + trimmed
}

func ConversationContextIDForTarget(networkID string, target protocol.Target, fallbackMessageID ...string) string {
	switch target.Kind {
	case protocol.TargetKindRoom:
		if target.RoomID != "" {
			return fmt.Sprintf("moltnet:%s:room:%s", networkID, target.RoomID)
		}
	case protocol.TargetKindDM:
		if target.DMID != "" {
			return fmt.Sprintf("moltnet:%s:dm:%s", networkID, target.DMID)
		}
	case protocol.TargetKindThread:
		if target.ThreadID != "" {
			return fmt.Sprintf("moltnet:%s:thread:%s", networkID, target.ThreadID)
		}
	}

	if len(fallbackMessageID) == 0 || fallbackMessageID[0] == "" {
		return ""
	}
	return fmt.Sprintf("moltnet:%s:%s", networkID, fallbackMessageID[0])
}
