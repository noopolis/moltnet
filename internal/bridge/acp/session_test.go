package acp

import (
	"context"
	"errors"
	"net"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/noopolis/moltnet/internal/bridge/clisession"
)

func TestACPResumesStoredSessionAfterRestart(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	config, trace := fixtureConfig(t, "load", "")
	delivery := clisession.Delivery{ContextKey: "room:research", Prompt: "first prompt"}
	first, err := newExecutor(ctx, &config)
	if err != nil {
		t.Fatal(err)
	}
	defer first.close()
	id, err := first.Turn(ctx, delivery)
	if err != nil || id == "" {
		t.Fatalf("create session: id=%q err=%v", id, err)
	}
	first.close()

	second, err := newExecutor(ctx, &config)
	if err != nil {
		t.Fatal(err)
	}
	defer second.close()
	delivery.ExistingSession = true
	delivery.RuntimeSessionID = id
	delivery.Prompt = "continued prompt"
	if got, err := second.Turn(ctx, delivery); err != nil || got != id {
		t.Fatalf("resume session: id=%q err=%v", got, err)
	}
	want := []fixtureEvent{
		{Method: "initialize"}, {Method: "session/new", SessionID: id},
		{Method: "session/prompt", SessionID: id, Text: "first prompt"},
		{Method: "initialize"}, {Method: "session/load", SessionID: id},
		{Method: "session/prompt", SessionID: id, Text: "continued prompt"},
	}
	if got := readFixtureTrace(t, trace); !slices.Equal(got, want) {
		t.Fatalf("session lifecycle = %+v, want %+v", got, want)
	}
}

func TestACPCorrelatesConcurrentResponses(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	executor, signal := executorWithSignal(t, ctx)
	defer executor.close()

	roomResult := make(chan error, 1)
	go func() {
		_, err := executor.Turn(ctx, clisession.Delivery{ContextKey: "room:research", Prompt: "hold"})
		roomResult <- err
	}()
	awaitFixture(t, signal)
	if _, err := executor.Turn(ctx, clisession.Delivery{ContextKey: "dm:writer", Prompt: "release"}); err != nil {
		t.Fatalf("DM received another session's error: %v", err)
	}
	var rpcErr *rpcError
	if err := <-roomResult; !errors.As(err, &rpcErr) || rpcErr.Message != "fixture prompt failure" {
		t.Fatalf("room error = %v", err)
	}
}

func TestACPCancelsPromptAndKeepsSession(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	signal := fixtureSignal(t, ctx)
	config, trace := fixtureConfig(t, "persistent", signal.Addr().String())
	executor, err := newExecutor(ctx, &config)
	if err != nil {
		t.Fatal(err)
	}
	defer executor.close()

	turnCtx, cancelTurn := context.WithCancel(ctx)
	defer cancelTurn()
	turnResult := make(chan error, 1)
	go func() {
		_, err := executor.Turn(turnCtx, clisession.Delivery{ContextKey: "room:research", Prompt: "hold"})
		turnResult <- err
	}()
	awaitFixture(t, signal)
	cancelTurn()
	if err := <-turnResult; !errors.Is(err, context.Canceled) {
		t.Fatalf("cancelled prompt returned %v", err)
	}

	if id, err := executor.Turn(ctx, clisession.Delivery{ContextKey: "room:research", Prompt: "continue"}); err != nil || id != "" {
		t.Fatalf("continue in persistent session: id=%q err=%v", id, err)
	}
	got := readFixtureTrace(t, trace)
	const id = "fixture-session-1"
	want := []fixtureEvent{
		{Method: "initialize"}, {Method: "session/new", SessionID: id},
		{Method: "session/prompt", SessionID: id, Text: "hold"},
		{Method: "session/cancel", SessionID: id},
		{Method: "session/prompt", SessionID: id, Text: "continue"},
	}
	if !slices.Equal(got, want) {
		t.Fatalf("cancel lifecycle = %+v, want %+v", got, want)
	}
}

func TestACPChildExitRestartsAndProtocolMismatchFails(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	config, trace := fixtureConfig(t, "persistent", "")
	executor, err := newExecutor(ctx, &config)
	if err != nil {
		t.Fatal(err)
	}
	defer executor.close()
	if _, err := executor.Turn(ctx, clisession.Delivery{ContextKey: "room", Prompt: "exit"}); err == nil || !strings.Contains(err.Error(), "ACP agent closed stdout") {
		t.Fatalf("child exit = %v", err)
	}
	if _, err := executor.Turn(ctx, clisession.Delivery{ContextKey: "room", Prompt: "recover"}); err != nil {
		t.Fatal(err)
	}
	got := readFixtureTrace(t, trace)
	if len(got) != 6 || got[3].Method != "initialize" || got[5].Text != "recover" {
		t.Fatalf("child exit was not followed by a new agent: %+v", got)
	}

	brokenConfig, _ := fixtureConfig(t, "bad-version", "")
	broken, err := newExecutor(ctx, &brokenConfig)
	if err != nil {
		t.Fatal(err)
	}
	defer broken.close()
	if _, err := broken.Turn(ctx, clisession.Delivery{ContextKey: "room"}); err == nil || !strings.Contains(err.Error(), "negotiated protocol version 2") {
		t.Fatalf("protocol mismatch = %v", err)
	}
}

func executorWithSignal(t *testing.T, ctx context.Context) (*executor, *net.TCPListener) {
	t.Helper()
	signal := fixtureSignal(t, ctx)
	config, _ := fixtureConfig(t, "persistent", signal.Addr().String())
	executor, err := newExecutor(ctx, &config)
	if err != nil {
		t.Fatal(err)
	}
	return executor, signal
}

func fixtureSignal(t *testing.T, ctx context.Context) *net.TCPListener {
	t.Helper()
	signal, err := net.ListenTCP("tcp", &net.TCPAddr{IP: net.IPv4(127, 0, 0, 1)})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { signal.Close() })
	deadline, _ := ctx.Deadline()
	if err := signal.SetDeadline(deadline); err != nil {
		t.Fatal(err)
	}
	return signal
}

func awaitFixture(t *testing.T, signal *net.TCPListener) {
	t.Helper()
	connection, err := signal.Accept()
	if err != nil {
		t.Fatalf("waiting for fixture prompt: %v", err)
	}
	connection.Close()
}
