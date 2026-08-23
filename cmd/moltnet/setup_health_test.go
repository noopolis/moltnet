package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

// TestProbeSetupNetworkHealthMatchingID is the "not just /healthz" half of
// SETUP.md's health-probe requirement: a 200 response whose body id matches
// the network id must report healthy.
func TestProbeSetupNetworkHealthMatchingID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/network" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		_ = json.NewEncoder(w).Encode(setupNetworkProbeResponse{ID: "acme"})
	}))
	defer server.Close()

	if !probeSetupNetworkHealth(context.Background(), server.URL, "acme") {
		t.Fatal("expected a healthy probe for a matching network id")
	}
}

// TestProbeSetupNetworkHealthWrongIDNeverSucceeds proves the probe checks
// identity, not just reachability: a 200 response naming a *different*
// network id must never report healthy, however long it retries — exactly
// the case SETUP.md calls out (a second network on the same port).
func TestProbeSetupNetworkHealthWrongIDNeverSucceeds(t *testing.T) {
	previousTimeout, previousInterval := setupHealthProbeTimeout, setupHealthProbeInterval
	setupHealthProbeTimeout, setupHealthProbeInterval = 100_000_000, 20_000_000 // 100ms / 20ms, keeping the test fast.
	t.Cleanup(func() { setupHealthProbeTimeout, setupHealthProbeInterval = previousTimeout, previousInterval })

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewEncoder(w).Encode(setupNetworkProbeResponse{ID: "someone-elses-network"})
	}))
	defer server.Close()

	if probeSetupNetworkHealth(context.Background(), server.URL, "acme") {
		t.Fatal("expected the probe to never report healthy for a mismatched network id")
	}
}

// TestProbeSetupNetworkHealthUnreachableTimesOut confirms the probe gives
// up (rather than blocking forever) when nothing answers at all — the
// state a fresh `service install` is briefly in before its process has
// actually bound the port.
func TestProbeSetupNetworkHealthUnreachableTimesOut(t *testing.T) {
	previousTimeout, previousInterval := setupHealthProbeTimeout, setupHealthProbeInterval
	setupHealthProbeTimeout, setupHealthProbeInterval = 100_000_000, 20_000_000
	t.Cleanup(func() { setupHealthProbeTimeout, setupHealthProbeInterval = previousTimeout, previousInterval })

	if probeSetupNetworkHealth(context.Background(), "http://127.0.0.1:1", "acme") {
		t.Fatal("expected the probe to fail against an address nothing is listening on")
	}
}
