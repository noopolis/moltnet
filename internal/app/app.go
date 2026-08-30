package app

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"path/filepath"
	"strings"
	"sync"
	"time"

	authn "github.com/noopolis/moltnet/internal/auth"
	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/observability"
	"github.com/noopolis/moltnet/internal/pairings"
	"github.com/noopolis/moltnet/internal/pairings/relay"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/internal/transport"
)

const (
	defaultReadTimeout  = 15 * time.Second
	defaultWriteTimeout = 30 * time.Second
	defaultIdleTimeout  = 60 * time.Second
)

type App struct {
	config       Config
	server       *http.Server
	relayClients []*relay.Client
	relayWG      sync.WaitGroup
	closers      []io.Closer
}

type serviceStore interface {
	store.RoomStore
	store.MessageStore
	store.AttachmentDeliveryStore
}

func New(config Config) (*App, error) {
	if err := validateRoomCredentials(config.Rooms, config.Auth.Mode); err != nil {
		return nil, err
	}
	roomStore, err := buildStore(config)
	if err != nil {
		return nil, err
	}
	broker := events.NewBroker(roomStore)

	var causalWriter *observability.CausalWriter
	var transcriptWriter *observability.TranscriptWriter
	if path := strings.TrimSpace(config.CausalEventsPath); path != "" {
		causalWriter, err = observability.NewCausalFileWriter(path)
		if err != nil {
			return nil, err
		}
		transcriptWriter, err = observability.NewTranscriptFileWriter(filepath.Join(filepath.Dir(path), "transcript.json"), config.NetworkID)
		if err != nil {
			_ = causalWriter.Close()
			return nil, err
		}
	}

	pairingClient := pairings.NewClient()
	relayClients := newRelayClients(config, pairingClient)
	service := rooms.NewService(rooms.ServiceConfig{
		AllowHumanIngress:         config.AllowHumanIngress,
		CausalWriter:              causalWriter,
		TranscriptWriter:          transcriptWriter,
		DebugEvents:               config.DebugEvents,
		DisableDirectMessages:     config.DisableDirectMessages,
		RequirePairNetworkBinding: config.Auth.RequirePairNetworkBinding,
		NetworkID:                 config.NetworkID,
		NetworkName:               config.NetworkName,
		Pairings:                  config.Pairings,
		Version:                   config.Version,
		Store:                     roomStore,
		Messages:                  roomStore,
		Broker:                    broker,
		PairingClient:             pairingClient,
	})

	config.Auth.Tokens = bindPairingNetworks(config.Auth.Tokens, config.Pairings)
	policy, err := authn.NewPolicy(config.Auth)
	if err != nil {
		return nil, fmt.Errorf("build auth policy: %w", err)
	}

	handler := transport.NewHTTPHandler(service, policy, transport.HTTPConfig{
		Console: transport.ConsoleConfig{
			Analytics: transport.ConsoleAnalyticsConfig{
				Provider:      config.Console.Analytics.Provider,
				MeasurementID: config.Console.Analytics.MeasurementID,
			},
		},
	})
	for _, relayClient := range relayClients {
		relayClient.SetHandler(handler)
	}

	server := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
		ReadTimeout:       defaultReadTimeout,
		WriteTimeout:      defaultWriteTimeout,
		IdleTimeout:       defaultIdleTimeout,
	}

	instance := &App{
		config:       config,
		server:       server,
		relayClients: relayClients,
	}
	if closer, ok := roomStore.(io.Closer); ok {
		instance.closers = append(instance.closers, closer)
	}
	if closer, ok := any(service).(io.Closer); ok {
		instance.closers = append(instance.closers, closer)
	}
	if causalWriter != nil {
		instance.closers = append(instance.closers, causalWriter)
	}
	if transcriptWriter != nil {
		instance.closers = append(instance.closers, transcriptWriter)
	}

	applyRequest, err := applyRequestFromConfig(config)
	if err != nil {
		return nil, err
	}
	applyContext := authn.WithMode(context.Background(), config.Auth.Mode)
	if _, err := service.ApplyConfigContext(applyContext, applyRequest); err != nil {
		return nil, err
	}

	return instance, nil
}

func newRelayClients(config Config, pairingClient *pairings.Client) []*relay.Client {
	clients := make([]*relay.Client, 0, len(config.Pairings))
	for _, pairing := range config.Pairings {
		if pairing.Relay == nil || strings.TrimSpace(pairing.Relay.URL) == "" {
			continue
		}
		relayToken := strings.TrimSpace(pairing.Relay.Token.Reveal())
		if relayToken == "" {
			relayToken = pairing.Token.Reveal()
		}
		client := relay.NewClient(relayEndpoint(pairing.Relay.URL, pairing.Relay.Room), relayToken, pairing.Token.Reveal(), config.NetworkID)
		pairingClient.RegisterRelay(pairing.ID, client)
		clients = append(clients, client)
	}
	return clients
}

func relayEndpoint(baseURL string, room string) string {
	return strings.TrimRight(strings.TrimSpace(baseURL), "/") +
		"/parties/relay-room/" + url.PathEscape(strings.TrimSpace(room))
}

func (a *App) Handler() http.Handler {
	return a.server.Handler
}

func (a *App) Close() error {
	a.close()
	return nil
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
	for _, relayClient := range a.relayClients {
		if relayClient == nil {
			continue
		}
		a.relayWG.Add(1)
		go func(client *relay.Client) {
			defer a.relayWG.Done()
			if err := client.Run(ctx); err != nil && ctx.Err() == nil {
				observability.Logger(ctx, "app", "error", err).Error("run relay client")
			}
		}(relayClient)
	}

	if warning := NonLoopbackAnonymousWriteWarning(a.config); warning != "" {
		observability.Logger(context.Background(), "app", "listen_addr", a.config.ListenAddr).
			Warn(warning)
	}

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
	for _, relayClient := range a.relayClients {
		if relayClient != nil {
			relayClient.Close()
		}
	}
	a.relayWG.Wait()
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
