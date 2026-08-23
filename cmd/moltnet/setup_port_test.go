package main

import (
	"fmt"
	"net"
	"strings"
	"testing"
)

// withTestDefaultPort overrides setupDefaultPort to a harmless high port for
// the test's duration. This repo's HARD RULE forbids ever binding the real
// 127.0.0.1:8787 (the user's live moltnet server holds it), so every test in
// this file that needs to observe preflightListenAddr's "default port is
// held" branch does so against this stand-in port instead of the real one.
func withTestDefaultPort(t *testing.T) {
	t.Helper()
	previous := setupDefaultPort
	setupDefaultPort = 18787
	t.Cleanup(func() { setupDefaultPort = previous })
}

// TestPreflightListenAddrDefaultPortFree is the common case: nothing else
// on the machine holds the "default" port, so preflightListenAddr must
// return it unchanged.
func TestPreflightListenAddrDefaultPortFree(t *testing.T) {
	withTestDefaultPort(t)
	if !portCurrentlyFree(t, "127.0.0.1", setupDefaultPort) {
		t.Skip("the stand-in default port is already held on this machine; skipping the free-port assertion")
	}

	addr, err := preflightListenAddr("127.0.0.1")
	if err != nil {
		t.Fatalf("preflightListenAddr() error = %v", err)
	}
	if addr != fmt.Sprintf("127.0.0.1:%d", setupDefaultPort) {
		t.Fatalf("preflightListenAddr() = %q, want the default port", addr)
	}
}

// TestPreflightListenAddrFallsBackWhenPortHeld is the test the port
// preflight exists for (SETUP.md, and this unit's own test plan): with the
// default port already bound on the requested host, preflightListenAddr
// must pick a different, actually-bindable port on that same host rather
// than reporting success for a port a second network could never actually
// use.
func TestPreflightListenAddrFallsBackWhenPortHeld(t *testing.T) {
	withTestDefaultPort(t)
	listener, err := net.Listen("tcp", fmt.Sprintf("127.0.0.1:%d", setupDefaultPort))
	if err != nil {
		t.Skipf("could not hold the stand-in default port for this test: %v", err)
	}
	defer listener.Close()

	addr, err := preflightListenAddr("127.0.0.1")
	if err != nil {
		t.Fatalf("preflightListenAddr() error = %v", err)
	}
	if addr == fmt.Sprintf("127.0.0.1:%d", setupDefaultPort) {
		t.Fatalf("preflightListenAddr() returned the held default port %q", addr)
	}
	if !strings.HasPrefix(addr, "127.0.0.1:") {
		t.Fatalf("preflightListenAddr() = %q, want it to stay on the requested host", addr)
	}

	// The returned address must itself actually be bindable right now --
	// otherwise this "preflight" would just be reporting a second
	// unusable port instead of the first one.
	confirm, err := net.Listen("tcp", addr)
	if err != nil {
		t.Fatalf("preflightListenAddr()'s own chosen address %q did not bind: %v", addr, err)
	}
	confirm.Close()
}

// TestPreflightListenAddrProbesRequestedFamily is SETUP.md's second
// port-selection consequence: a free 127.0.0.1:<port> does not prove
// 0.0.0.0:<port> is bindable, so the probe must target whichever host Q4
// actually chose, not loopback as a stand-in for it. This is asserted here
// by checking the returned address's host, not by trying to force a real
// 0.0.0.0 bind conflict (which would require binding a privileged-adjacent,
// machine-wide address in a test — riskier than this test needs to be).
func TestPreflightListenAddrProbesRequestedFamily(t *testing.T) {
	withTestDefaultPort(t)
	addr, err := preflightListenAddr("0.0.0.0")
	if err != nil {
		t.Fatalf("preflightListenAddr(\"0.0.0.0\") error = %v", err)
	}
	if !strings.HasPrefix(addr, "0.0.0.0:") {
		t.Fatalf("preflightListenAddr(\"0.0.0.0\") = %q, want it to keep the requested host", addr)
	}
}

// portCurrentlyFree reports whether host:port can be bound right now,
// releasing it immediately -- used only to decide whether
// TestPreflightListenAddrDefaultPortFree's assumption holds on this
// machine, never as part of the code under test.
func portCurrentlyFree(t *testing.T, host string, port int) bool {
	t.Helper()
	listener, err := net.Listen("tcp", fmt.Sprintf("%s:%d", host, port))
	if err != nil {
		return false
	}
	_ = listener.Close()
	return true
}
