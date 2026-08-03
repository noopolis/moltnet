package relay

import (
	"bytes"
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
)

type goldenFramesFixture struct {
	MaxHeaderBytes  int                 `json:"maxHeaderBytes"`
	MaxPayloadBytes int                 `json:"maxPayloadBytes"`
	Vectors         []goldenFrameVector `json:"vectors"`
}

type goldenFrameVector struct {
	Name        string `json:"name"`
	FrameBase64 string `json:"frameBase64"`
	Valid       bool   `json:"valid"`
}

func TestGoldenFrames(t *testing.T) {
	fixture := loadGoldenFrames(t)
	if maxHeaderSize != fixture.MaxHeaderBytes {
		t.Fatalf("maxHeaderSize = %d, fixture maxHeaderBytes = %d", maxHeaderSize, fixture.MaxHeaderBytes)
	}
	if maxRelayPayloadBytes != fixture.MaxPayloadBytes {
		t.Fatalf("maxRelayPayloadBytes = %d, fixture maxPayloadBytes = %d", maxRelayPayloadBytes, fixture.MaxPayloadBytes)
	}

	for _, vector := range fixture.Vectors {
		t.Run(vector.Name, func(t *testing.T) {
			raw, err := base64.StdEncoding.DecodeString(vector.FrameBase64)
			if err != nil {
				t.Fatalf("decode frameBase64: %v", err)
			}
			headerBytes, payload := splitGoldenFrame(raw)
			var header frameHeader
			if err := json.Unmarshal(headerBytes, &header); err != nil {
				t.Fatalf("decode fixture header: %v", err)
			}

			got, err := makeFrame(header, payload)
			if !vector.Valid {
				want := fmt.Sprintf("relay header exceeds %d bytes", maxHeaderSize)
				if err == nil || err.Error() != want {
					t.Fatalf("makeFrame() error = %v, want %q", err, want)
				}
				return
			}
			if err != nil {
				t.Fatalf("makeFrame(): %v", err)
			}
			if !bytes.Equal(got, raw) {
				t.Fatal("makeFrame() did not reproduce the fixture's exact frame bytes")
			}
		})
	}
}

func TestGoldenPayloadBoundary(t *testing.T) {
	fixture := loadGoldenFrames(t)
	client := NewClient("", "relay-token", "pairing-token", "network-a")

	_, _, err := client.Call(context.Background(), "POST", "/v1/messages", make([]byte, fixture.MaxPayloadBytes))
	if !errors.Is(err, ErrNotConnected) {
		t.Fatalf("Call() at payload cap error = %v, want ErrNotConnected after accepting payload", err)
	}

	_, _, err = client.Call(context.Background(), "POST", "/v1/messages", make([]byte, fixture.MaxPayloadBytes+1))
	if !errors.Is(err, ErrPayloadTooLarge) {
		t.Fatalf("Call() over payload cap error = %v, want ErrPayloadTooLarge", err)
	}
}

func loadGoldenFrames(t *testing.T) goldenFramesFixture {
	t.Helper()
	// This relative path couples the Go package to relay/. Revisit it alongside
	// the relay's self-contained test fixture if relay/ is extracted later.
	path := filepath.Join("..", "..", "..", "relay", "testdata", "golden_frames.json")
	raw, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read golden fixture: %v", err)
	}
	var fixture goldenFramesFixture
	if err := json.Unmarshal(raw, &fixture); err != nil {
		t.Fatalf("decode golden fixture: %v", err)
	}
	return fixture
}

func splitGoldenFrame(raw []byte) ([]byte, []byte) {
	if headerEnd := bytes.IndexByte(raw, '\n'); headerEnd >= 0 {
		return raw[:headerEnd], raw[headerEnd+1:]
	}
	return raw, nil
}
