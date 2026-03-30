package app

import (
	"context"
	"fmt"
	"log"
	"net/http"
	"time"

	"github.com/noopolis/moltnet/internal/events"
	"github.com/noopolis/moltnet/internal/rooms"
	"github.com/noopolis/moltnet/internal/store"
	"github.com/noopolis/moltnet/internal/transport"
)

type App struct {
	config Config
	server *http.Server
}

func New(config Config) *App {
	broker := events.NewBroker()
	memoryStore := store.NewMemoryStore()

	service := rooms.NewService(rooms.ServiceConfig{
		AllowHumanIngress: config.AllowHumanIngress,
		NetworkID:         config.NetworkID,
		NetworkName:       config.NetworkName,
		Pairings:          config.Pairings,
		Version:           config.Version,
		Store:             memoryStore,
		Messages:          memoryStore,
		Broker:            broker,
	})

	handler := transport.NewHTTPHandler(service)

	server := &http.Server{
		Addr:              config.ListenAddr,
		Handler:           handler,
		ReadHeaderTimeout: 5 * time.Second,
	}

	return &App{
		config: config,
		server: server,
	}
}

func (a *App) Run(ctx context.Context) error {
	errorCh := make(chan error, 1)

	go func() {
		log.Printf("moltnet listening on %s", a.config.ListenAddr)
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
		if err == nil || err == http.ErrServerClosed {
			return nil
		}

		return fmt.Errorf("run moltnet: %w", err)
	}
}
