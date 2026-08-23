package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/noopolis/moltnet/pkg/protocol"
)

// resolveInitNetworkID returns idFlag unchanged when non-empty; otherwise,
// on a TTY, it prompts for one; otherwise it is a hard error (PLAN.md:
// "--id <network-id> sets the network id (interactive prompt on a TTY when
// omitted; hard error when non-interactive)").
//
// The prompt read happens in its own goroutine so it can be raced against
// ctx.Done() (item 3): bufio.Reader.ReadString blocks synchronously on
// os.Stdin with no ctx-aware variant, so there is no way to interrupt the
// read itself — a SIGINT during the prompt instead makes this function
// return ctx.Err() immediately, leaving the read goroutine to exit on its
// own once stdin closes or the process does. That goroutine leak is
// bounded: runCLI is about to exit on the same cancelled ctx either way.
func resolveInitNetworkID(ctx context.Context, idFlag string) (string, error) {
	if idFlag != "" {
		if err := protocol.ValidateMessageID(idFlag); err != nil {
			return "", fmt.Errorf("--id: %w", err)
		}
		return idFlag, nil
	}
	if !isInteractive() {
		return "", fmt.Errorf("moltnet init requires --id when run non-interactively (no TTY to prompt on)")
	}

	fmt.Fprint(stdout, "network id (letters, digits, hyphens; identifies this network to friends): ")

	// os.Stdin is read into a local here, before the goroutine starts,
	// rather than inside it: os.Stdin is a mutable package var, and this
	// function is about to return (on the ctx.Done() branch below) without
	// waiting for the goroutine to actually run, so a caller reassigning
	// os.Stdin concurrently (only ever a test — production never does)
	// would otherwise be a real data race on the var itself, not just on
	// what it points to.
	stdin := os.Stdin

	type readResult struct {
		line string
		err  error
	}
	resultCh := make(chan readResult, 1)
	go func() {
		reader := bufio.NewReader(stdin)
		line, err := reader.ReadString('\n')
		resultCh <- readResult{line: line, err: err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-resultCh:
		if res.err != nil && !errors.Is(res.err, io.EOF) {
			return "", fmt.Errorf("read network id: %w", res.err)
		}
		id := strings.TrimSpace(res.line)
		if id == "" {
			return "", fmt.Errorf("network id is required")
		}
		if err := protocol.ValidateMessageID(id); err != nil {
			return "", fmt.Errorf("network id: %w", err)
		}
		return id, nil
	}
}

// defaultNetworkNameForID derives a human-readable display name from a
// network id when --name is not given, e.g. "acme-friends" -> "Acme
// Friends Moltnet".
func defaultNetworkNameForID(id string) string {
	words := strings.FieldsFunc(id, func(r rune) bool { return r == '-' || r == '_' })
	for index, word := range words {
		if word == "" {
			continue
		}
		words[index] = strings.ToUpper(word[:1]) + word[1:]
	}
	name := strings.Join(words, " ")
	if name == "" {
		name = id
	}
	return name + " Moltnet"
}
