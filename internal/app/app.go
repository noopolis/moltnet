package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/internal/pairings"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/internal/transport"
	"github.com/noopolis/moltnet/pkg/protocol"
)

const (
	defaultReadTimeout  = 15 * time.Second
	defaultWriteTimeout = 30 * time.Second
	defaultIdleTimeout  = 60 * time.Second
)

type App struct {
	config  Config
	server  *http.Server
	closers []io.Closer
}

type serviceStore interface {
	store.RoomStore
	store.MessageStore
}

func New(config Config) (*App, error) {
	broker := events.NewBroker()
	roomStore, err := buildStore(config)
	if err != nil {
		return nil, err
	}

	service := rooms.NewService(rooms.ServiceConfig{
		AllowHumanIngress: config.AllowHumanIngress,
		NetworkID:         config.NetworkID,
		NetworkName:       config.NetworkName,
		Pairings:          config.Pairings,
		Version:           config.Version,
		Store:             roomStore,
		Messages:          roomStore,
		Broker:            broker,
		PairingClient:     pairings.NewClient(),
	})

	policy, err := authn.NewPolicy(config.Auth)
	if err != nil {
		return nil, fmt.Errorf("build auth policy: %w", err)
	}

	handler := transport.NewHTTPHandler(service, policy)

	server := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
	}

	instance := &App{
		config: config,
		server: server,
	}
	if closer, ok := roomStore.(io.Closer); ok {
		instance.closers = append(instance.closers, closer)
	}
	if closer, ok := any(service).(io.Closer); ok {
		instance.closers = append(instance.closers, closer)
	}

	if err := seedRooms(service, config.Rooms); err != nil {
		return nil, err
	}

	return instance, nil
}

func buildStore(config Config) (serviceStore, error) {
	switch config.Storage.Kind {
	case "", storageKindMemory:
		return store.NewMemoryStore(), nil
	case storageKindJSON:
		return store.NewFileStore(config.Storage.JSON.Path)
	case storageKindSQLite:
		return store.NewSQLiteStore(config.Storage.SQLite.Path)
	case storageKindPostgres:
		return store.NewPostgresStore(config.Storage.Postgres.DSN)
	default:
		return nil, fmt.Errorf("unsupported storage kind %q", config.Storage.Kind)
	}
}

func (a *App) Run(ctx context.Context) error {
	defer a.close()

	errorCh := make(chan error, 1)

	go func() {
		observability.Logger(context.Background(), "app", "listen_addr", a.config.ListenAddr).
			Info("moltnet listening")
		errorCh <- a.server.ListenAndServe()
	}()

	select {
	case <-ctx.Done():
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		if err := a.server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("shutdown moltnet: %w", err)
		}

		return nil
	case err := <-errorCh:
		if err == nil || errors.Is(err, http.ErrServerClosed) {
			return nil
		}

		return fmt.Errorf("run moltnet: %w", err)
	}
}

func (a *App) close() {
	if len(a.closers) == 0 {
		return
	}
	for _, closer := range a.closers {
		if closer == nil {
			continue
		}
		if err := closer.Close(); err != nil {
			observability.Logger(context.Background(), "app", "error", err).
				Warn("close moltnet resources")
		}
	}
}

func seedRooms(service *rooms.Service, roomConfigs []RoomConfig) error {
	for _, roomConfig := range roomConfigs {
		if _, err := service.CreateRoom(protocol.CreateRoomRequest{
			ID:      roomConfig.ID,
			Name:    roomConfig.Name,
			Members: append([]string(nil), roomConfig.Members...),
		}); err != nil {
			if errors.Is(err, rooms.ErrRoomExists) {
				continue
			}
			return fmt.Errorf("seed room %q: %w", roomConfig.ID, err)
		}
	}

	return nil
}
