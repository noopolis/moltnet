package main

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
)

// promptLineCtx is promptLine's (prompt_line.go) ctx-aware counterpart:
// every typed question in this wizard (askSetupNetworkID's Q2, and the two
// sub-prompts askSetupRooms's "rename"/"add more" branches read,
// setup_questions.go) needs the same interruptibility guarantee every
// select question already has via promptSelectInteractive's ctx.Done()
// case, and resolveInitNetworkID's own interactive --id prompt already
// solves exactly this (init.go) — races the underlying line read against
// ctx.Done() in a goroutine, since bufio.Reader.ReadString has no ctx-aware
// variant of its own.
//
// P1-2: before this existed, a SIGINT at any of these typed prompts
// cancelled ctx (runCLI's signal.NotifyContext) but the process stayed
// blocked in ReadString regardless — it only noticed cancellation once the
// operator pressed Enter, and by then the typed line had already been
// silently consumed as the answer to that question rather than treated as
// an interrupt. Racing the read the same way resolveInitNetworkID and
// promptSelectInteractive already do closes that gap: on ctx.Done(), this
// returns immediately with ctx.Err(), and the still-blocked read goroutine
// is left to exit on its own once the process does (bounded the same way,
// for the same reason those two call sites already accept).
//
// The underlying io.Reader is captured into a local bufio.Reader before the
// goroutine starts, exactly like resolveInitNetworkID captures os.Stdin
// into a local first: promptReader is a mutable package var, and this
// function can return (on the ctx.Done() branch) before the goroutine ever
// runs, so starting the goroutine from a value already read out of the var
// avoids a data race against a test reassigning it concurrently.
func promptLineCtx(ctx context.Context, question string) (string, error) {
	fmt.Fprint(stdout, question)
	reader := bufio.NewReader(promptReader)

	type readResult struct {
		line string
		err  error
	}
	resultCh := make(chan readResult, 1)
	go func() {
		line, err := reader.ReadString('\n')
		resultCh <- readResult{line: line, err: err}
	}()

	select {
	case <-ctx.Done():
		return "", ctx.Err()
	case res := <-resultCh:
		if res.err != nil && !errors.Is(res.err, io.EOF) {
			return "", fmt.Errorf("read input: %w", res.err)
		}
		return strings.TrimSpace(res.line), res.err
	}
}
