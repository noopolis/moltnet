package main

import (
	"fmt"
	"net"
)

// setupDefaultPort mirrors templates.go's defaultInitListenAddr's port —
// 8787, the port both Q4 choices imply (SETUP.md's "Port selection —
// decided, not asked"). Named separately here (not exported from
// templates.go, which this change must not edit) since setup_port.go needs
// only the port half of that constant, not the whole "127.0.0.1:8787"
// address — the host half varies with Q4's answer, which is the entire
// reason this file exists.
//
// A var, not a const: this repo's own HARD RULE forbids ever binding the
// real 127.0.0.1:8787 from a test (the user's live moltnet server holds
// it), so setup_port_test.go overrides this to a harmless high port for
// every test that needs to probe an actually-held "default" port, rather
// than risk a real collision — or, worse, a real successful bind — against
// 8787 itself.
var setupDefaultPort = 8787

// preflightListenAddr picks a listen_addr for host (either "127.0.0.1", for
// Q4's loopback answer, or "0.0.0.0", for its widened-bind answer) that is
// actually bindable on this machine, so `moltnet setup` never reports
// success for a second network on a machine where one already owns 8787.
//
// It tries setupDefaultPort on host first — the common case, and the one
// that keeps a first-ever network's URL exactly the one every doc and
// example already shows. Only when that bind fails does it fall back to an
// OS-assigned ephemeral port on the same host, discovered the standard way
// (":0"): SETUP.md is explicit that a free 127.0.0.1:<port> does not prove
// 0.0.0.0:<port> is bindable, so the probe always targets the same host
// family Q4 actually chose, never loopback as a proxy for it.
//
// TODO(setup-p2): this is a probe-then-release check, not a held reservation
// -- the port is free at the moment of the check but nothing stops another
// process (including a second, concurrent `moltnet setup` run) from
// claiming it before the `init`/service-start child actually binds it a few
// steps later. Closing that race fully would mean holding the listener open
// until the child process can inherit its file descriptor, which is a much
// larger change than this preflight; SETUP.md does not call the race out
// explicitly, and every other command in this CLI that touches listen_addr
// has the same gap, so this is a pre-existing, not newly introduced,
// limitation.
//
// TODO(setup-p2): this binds and releases a real port even on the
// --print-commands and non-TTY-refusal paths, both advertised as doing
// nothing -- a real (if brief and harmless) side effect on two paths a
// caller might reasonably assume are side-effect-free.
func preflightListenAddr(host string) (string, error) {
	if port, ok := probeBindable(host, setupDefaultPort); ok {
		return net.JoinHostPort(host, fmt.Sprintf("%d", port)), nil
	}

	port, err := freeEphemeralPort(host)
	if err != nil {
		return "", fmt.Errorf("find a free port on %s (default %d is already taken): %w", host, setupDefaultPort, err)
	}
	return net.JoinHostPort(host, fmt.Sprintf("%d", port)), nil
}

// probeBindable reports whether host:port can be bound right now, by
// binding it and immediately closing the listener again.
func probeBindable(host string, port int) (int, bool) {
	listener, err := net.Listen("tcp", net.JoinHostPort(host, fmt.Sprintf("%d", port)))
	if err != nil {
		return 0, false
	}
	_ = listener.Close()
	return port, true
}

// freeEphemeralPort asks the OS for a free port on host by binding port 0
// (the standard "give me any free port" request) and reading back whatever
// it assigned.
func freeEphemeralPort(host string) (int, error) {
	listener, err := net.Listen("tcp", net.JoinHostPort(host, "0"))
	if err != nil {
		return 0, err
	}
	defer listener.Close()

	addr, ok := listener.Addr().(*net.TCPAddr)
	if !ok {
		return 0, fmt.Errorf("unexpected listener address type %T", listener.Addr())
	}
	return addr.Port, nil
}
