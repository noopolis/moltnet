package acp

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"sync"
)

type launchSpec struct {
	command string
	args    []string
	dir     string
	env     []string
}

type connection struct {
	input    *os.File
	output   *os.File
	process  *os.Process
	write    chan struct{}
	done     chan struct{}
	reaped   chan struct{}
	readDone chan struct{}
	stopOnce sync.Once
	err      error // Read only after done closes.

	mu         sync.Mutex
	nextID     uint64
	pending    map[string]chan rpcMessage
	cancelling map[string]bool
}

func launch(ctx context.Context, spec launchSpec) (*connection, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}

	childInput, input, err := os.Pipe()
	if err != nil {
		return nil, fmt.Errorf("create ACP stdin: %w", err)
	}
	defer childInput.Close()
	output, childOutput, err := os.Pipe()
	if err != nil {
		input.Close()
		return nil, fmt.Errorf("create ACP stdout: %w", err)
	}
	defer childOutput.Close()

	cmd := exec.Command(spec.command, spec.args...)
	configureProcess(cmd)
	cmd.Dir = spec.dir
	cmd.Env = append(cmd.Environ(), spec.env...)
	cmd.Stdin = childInput
	cmd.Stdout = childOutput
	cmd.Stderr = os.Stderr
	if err := cmd.Start(); err != nil {
		input.Close()
		output.Close()
		return nil, fmt.Errorf("start ACP agent: %w", err)
	}

	conn := &connection{
		input: input, output: output, process: cmd.Process,
		write: make(chan struct{}, 1),
		done:  make(chan struct{}), reaped: make(chan struct{}), readDone: make(chan struct{}),
		pending: make(map[string]chan rpcMessage), cancelling: make(map[string]bool),
	}
	stopOnCancel := context.AfterFunc(ctx, func() { conn.stop(ctx.Err()) })
	go func() {
		_ = cmd.Wait()
		close(conn.reaped)
	}()
	go func() {
		defer close(conn.readDone)
		defer stopOnCancel()
		conn.stop(conn.readMessages())
	}()
	return conn, nil
}

func (c *connection) stop(err error) {
	c.stopOnce.Do(func() {
		c.err = err
		close(c.done)
		killProcess(c.process)
		_ = c.input.Close()
		_ = c.output.Close()
	})
}

func (c *connection) close() {
	c.stop(fmt.Errorf("ACP connection closed"))
	<-c.reaped
	<-c.readDone
}

func (c *connection) isClosed() bool {
	select {
	case <-c.done:
		return true
	default:
		return false
	}
}
