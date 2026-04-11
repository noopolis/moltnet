package openclaw

import (
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"crypto/x509"
	"encoding/base64"
	"encoding/hex"
	"encoding/json"
	"encoding/pem"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

type gatewayDeviceIdentity struct {
	DeviceID      string
	PublicKeyPEM  string
	PrivateKeyPEM string
}

type storedGatewayDeviceIdentity struct {
	Version       int    `json:"version"`
	DeviceID      string `json:"device_id"`
	PublicKeyPEM  string `json:"public_key_pem"`
	PrivateKeyPEM string `json:"private_key_pem"`
	CreatedAtMS   int64  `json:"created_at_ms"`
}

func resolveGatewayDeviceIdentityPath(config bridgeconfig.Config) string {
	if homePath := strings.TrimSpace(config.Runtime.HomePath); homePath != "" {
		return filepath.Join(homePath, ".moltnet", "openclaw-device.json")
	}

	sum := sha256.Sum256([]byte(strings.TrimSpace(config.Runtime.GatewayURL) + "|" + strings.TrimSpace(config.Agent.ID)))
	return filepath.Join(os.TempDir(), "moltnet-openclaw-device-"+hex.EncodeToString(sum[:8])+".json")
}

func loadOrCreateGatewayDeviceIdentity(path string) (gatewayDeviceIdentity, error) {
	if identity, err := loadGatewayDeviceIdentity(path); err == nil {
		return identity, nil
	}

	publicKey, privateKey, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		return gatewayDeviceIdentity{}, fmt.Errorf("generate openclaw gateway device keypair: %w", err)
	}

	publicKeyDER, err := x509.MarshalPKIXPublicKey(publicKey)
	if err != nil {
		return gatewayDeviceIdentity{}, fmt.Errorf("marshal openclaw gateway public key: %w", err)
	}
	privateKeyDER, err := x509.MarshalPKCS8PrivateKey(privateKey)
	if err != nil {
		return gatewayDeviceIdentity{}, fmt.Errorf("marshal openclaw gateway private key: %w", err)
	}

	identity := gatewayDeviceIdentity{
		DeviceID:      fingerprintGatewayPublicKey(publicKey),
		PublicKeyPEM:  string(pem.EncodeToMemory(&pem.Block{Type: "PUBLIC KEY", Bytes: publicKeyDER})),
		PrivateKeyPEM: string(pem.EncodeToMemory(&pem.Block{Type: "PRIVATE KEY", Bytes: privateKeyDER})),
	}
	if err := storeGatewayDeviceIdentity(path, identity); err != nil {
		return gatewayDeviceIdentity{}, err
	}

	return identity, nil
}

func loadGatewayDeviceIdentity(path string) (gatewayDeviceIdentity, error) {
	bytes, err := os.ReadFile(path)
	if err != nil {
		return gatewayDeviceIdentity{}, fmt.Errorf("read openclaw gateway device identity: %w", err)
	}

	var stored storedGatewayDeviceIdentity
	if err := json.Unmarshal(bytes, &stored); err != nil {
		return gatewayDeviceIdentity{}, fmt.Errorf("decode openclaw gateway device identity: %w", err)
	}
	if stored.Version != 1 {
		return gatewayDeviceIdentity{}, fmt.Errorf("unsupported openclaw gateway device identity version %d", stored.Version)
	}
	if strings.TrimSpace(stored.PublicKeyPEM) == "" || strings.TrimSpace(stored.PrivateKeyPEM) == "" {
		return gatewayDeviceIdentity{}, fmt.Errorf("openclaw gateway device identity is incomplete")
	}

	publicKey, err := parseGatewayPublicKey(stored.PublicKeyPEM)
	if err != nil {
		return gatewayDeviceIdentity{}, err
	}
	deviceID := fingerprintGatewayPublicKey(publicKey)

	return gatewayDeviceIdentity{
		DeviceID:      deviceID,
		PublicKeyPEM:  stored.PublicKeyPEM,
		PrivateKeyPEM: stored.PrivateKeyPEM,
	}, nil
}

func storeGatewayDeviceIdentity(path string, identity gatewayDeviceIdentity) error {
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create openclaw gateway device identity dir: %w", err)
	}

	stored := storedGatewayDeviceIdentity{
		Version:       1,
		DeviceID:      identity.DeviceID,
		PublicKeyPEM:  identity.PublicKeyPEM,
		PrivateKeyPEM: identity.PrivateKeyPEM,
		CreatedAtMS:   time.Now().UnixMilli(),
	}
	bytes, err := json.MarshalIndent(stored, "", "  ")
	if err != nil {
		return fmt.Errorf("encode openclaw gateway device identity: %w", err)
	}
	if err := os.WriteFile(path, append(bytes, '\n'), 0o600); err != nil {
		return fmt.Errorf("write openclaw gateway device identity: %w", err)
	}
	return nil
}

func signGatewayDevicePayload(privateKeyPEM string, payload string) (string, error) {
	privateKey, err := parseGatewayPrivateKey(privateKeyPEM)
	if err != nil {
		return "", err
	}
	signature := ed25519.Sign(privateKey, []byte(payload))
	return base64.RawURLEncoding.EncodeToString(signature), nil
}

func publicKeyRawBase64URLFromPEM(publicKeyPEM string) (string, error) {
	publicKey, err := parseGatewayPublicKey(publicKeyPEM)
	if err != nil {
		return "", err
	}
	return base64.RawURLEncoding.EncodeToString(publicKey), nil
}

func fingerprintGatewayPublicKey(publicKey ed25519.PublicKey) string {
	sum := sha256.Sum256(publicKey)
	return hex.EncodeToString(sum[:])
}

func parseGatewayPublicKey(publicKeyPEM string) (ed25519.PublicKey, error) {
	block, _ := pem.Decode([]byte(publicKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("decode openclaw gateway public key PEM")
	}

	key, err := x509.ParsePKIXPublicKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse openclaw gateway public key: %w", err)
	}
	publicKey, ok := key.(ed25519.PublicKey)
	if !ok {
		return nil, fmt.Errorf("openclaw gateway public key is not ed25519")
	}
	return publicKey, nil
}

func parseGatewayPrivateKey(privateKeyPEM string) (ed25519.PrivateKey, error) {
	block, _ := pem.Decode([]byte(privateKeyPEM))
	if block == nil {
		return nil, fmt.Errorf("decode openclaw gateway private key PEM")
	}

	key, err := x509.ParsePKCS8PrivateKey(block.Bytes)
	if err != nil {
		return nil, fmt.Errorf("parse openclaw gateway private key: %w", err)
	}
	privateKey, ok := key.(ed25519.PrivateKey)
	if !ok {
		return nil, fmt.Errorf("openclaw gateway private key is not ed25519")
	}
	return privateKey, nil
}
