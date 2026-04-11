package openclaw

import (
	"context"
	"encoding/json"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/gorilla/websocket"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

const (
	gatewayProtocolVersion = 3
	gatewayRequestTimeout  = 15 * time.Second
)

type gatewayRequestFrame struct {
	Type   string `json:"type"`
	ID     string `json:"id"`
	Method string `json:"method"`
	Params any    `json:"params,omitempty"`
}

type gatewayResponseFrame struct {
	Type    string          `json:"type"`
	ID      string          `json:"id"`
	OK      bool            `json:"ok"`
	Payload json.RawMessage `json:"payload,omitempty"`
	Error   *gatewayError   `json:"error,omitempty"`
}

type gatewayEventFrame struct {
	Type    string          `json:"type"`
	Event   string          `json:"event"`
	Payload json.RawMessage `json:"payload,omitempty"`
}

type gatewayError struct {
	Code    string `json:"code,omitempty"`
	Message string `json:"message,omitempty"`
}

type gatewayConnectParams struct {
	MinProtocol int `json:"minProtocol"`
	MaxProtocol int `json:"maxProtocol"`
	Client      struct {
		ID           string `json:"id"`
		DisplayName  string `json:"displayName,omitempty"`
		Version      string `json:"version"`
		Platform     string `json:"platform"`
		DeviceFamily string `json:"deviceFamily,omitempty"`
		Mode         string `json:"mode"`
		InstanceID   string `json:"instanceId,omitempty"`
	} `json:"client"`
	Auth   *gatewayConnectAuth   `json:"auth,omitempty"`
	Role   string                `json:"role,omitempty"`
	Scopes []string              `json:"scopes,omitempty"`
	Device *gatewayConnectDevice `json:"device,omitempty"`
}

type gatewayConnectAuth struct {
	Token string `json:"token,omitempty"`
}

type gatewayConnectDevice struct {
	ID        string `json:"id"`
	PublicKey string `json:"publicKey"`
	Signature string `json:"signature"`
	SignedAt  int64  `json:"signedAt"`
	Nonce     string `json:"nonce"`
}

type gatewayChallengePayload struct {
	Nonce string `json:"nonce"`
}

func sendGatewayChat(
	ctx context.Context,
	config bridgeconfig.Config,
	sessionKey string,
	message string,
	idempotencyKey string,
) error {
	conn, _, err := websocket.DefaultDialer.DialContext(ctx, config.Runtime.GatewayURL, nil)
	if err != nil {
		return fmt.Errorf("connect openclaw gateway %s: %w", config.Runtime.GatewayURL, err)
	}
	defer conn.Close()

	if err := connectGateway(ctx, conn, config); err != nil {
		return err
	}

	_, err = requestGateway(ctx, conn, "chat.send", map[string]any{
		"deliver":        false,
		"idempotencyKey": idempotencyKey,
		"message":        message,
		"sessionKey":     sessionKey,
	})
	if err != nil {
		return err
	}

	return nil
}

func connectGateway(
	ctx context.Context,
	conn *websocket.Conn,
	config bridgeconfig.Config,
) error {
	for {
		frameType, raw, err := readGatewayFrame(ctx, conn)
		if err != nil {
			return err
		}

		if frameType != "event" {
			continue
		}

		var event gatewayEventFrame
		if err := json.Unmarshal(raw, &event); err != nil {
			return fmt.Errorf("decode openclaw gateway event: %w", err)
		}
		if event.Event != "connect.challenge" {
			continue
		}

		var challenge gatewayChallengePayload
		if len(event.Payload) > 0 {
			if err := json.Unmarshal(event.Payload, &challenge); err != nil {
				return fmt.Errorf("decode openclaw gateway connect challenge: %w", err)
			}
		}
		nonce := strings.TrimSpace(challenge.Nonce)
		if nonce == "" {
			return fmt.Errorf("openclaw gateway connect challenge missing nonce")
		}

		connectParams := gatewayConnectParams{
			MinProtocol: gatewayProtocolVersion,
			MaxProtocol: gatewayProtocolVersion,
			Role:        "operator",
			Scopes:      []string{"operator.admin", "operator.read", "operator.write"},
		}
		connectParams.Client.ID = "gateway-client"
		connectParams.Client.DisplayName = "moltnet-bridge"
		connectParams.Client.Version = "moltnet-bridge"
		connectParams.Client.Platform = "moltnet"
		connectParams.Client.DeviceFamily = "bridge"
		connectParams.Client.Mode = "backend"
		connectParams.Client.InstanceID = fmt.Sprintf("moltnet-bridge-%s", strings.TrimSpace(config.Agent.ID))
		token := resolveGatewayToken(config)
		if token != "" {
			connectParams.Auth = &gatewayConnectAuth{Token: token}
		}
		device, err := buildGatewayConnectDevice(config, connectParams, token, nonce)
		if err != nil {
			return err
		}
		connectParams.Device = device

		_, err = requestGateway(ctx, conn, "connect", connectParams)
		return err
	}
}

func requestGateway(
	ctx context.Context,
	conn *websocket.Conn,
	method string,
	params any,
) (json.RawMessage, error) {
	requestID := fmt.Sprintf("%s-%d", strings.ReplaceAll(method, ".", "-"), time.Now().UnixNano())
	if err := writeGatewayFrame(ctx, conn, gatewayRequestFrame{
		Type:   "req",
		ID:     requestID,
		Method: method,
		Params: params,
	}); err != nil {
		return nil, err
	}

	for {
		frameType, raw, err := readGatewayFrame(ctx, conn)
		if err != nil {
			return nil, err
		}

		switch frameType {
		case "event":
			continue
		case "res":
			var response gatewayResponseFrame
			if err := json.Unmarshal(raw, &response); err != nil {
				return nil, fmt.Errorf("decode openclaw gateway response: %w", err)
			}
			if response.ID != requestID {
				continue
			}
			if !response.OK {
				if response.Error != nil && strings.TrimSpace(response.Error.Message) != "" {
					return nil, fmt.Errorf(
						"openclaw gateway %s failed (%s): %s",
						method,
						strings.TrimSpace(response.Error.Code),
						strings.TrimSpace(response.Error.Message),
					)
				}
				return nil, fmt.Errorf("openclaw gateway %s failed", method)
			}
			return response.Payload, nil
		default:
			continue
		}
	}
}

func readGatewayFrame(ctx context.Context, conn *websocket.Conn) (string, json.RawMessage, error) {
	if err := conn.SetReadDeadline(resolveGatewayDeadline(ctx)); err != nil {
		return "", nil, fmt.Errorf("set openclaw gateway read deadline: %w", err)
	}

	_, raw, err := conn.ReadMessage()
	if err != nil {
		return "", nil, fmt.Errorf("read openclaw gateway frame: %w", err)
	}

	var envelope struct {
		Type string `json:"type"`
	}
	if err := json.Unmarshal(raw, &envelope); err != nil {
		return "", nil, fmt.Errorf("decode openclaw gateway envelope: %w", err)
	}

	return envelope.Type, raw, nil
}

func writeGatewayFrame(ctx context.Context, conn *websocket.Conn, frame gatewayRequestFrame) error {
	if err := conn.SetWriteDeadline(resolveGatewayDeadline(ctx)); err != nil {
		return fmt.Errorf("set openclaw gateway write deadline: %w", err)
	}
	if err := conn.WriteJSON(frame); err != nil {
		return fmt.Errorf("write openclaw gateway frame: %w", err)
	}
	return nil
}

func resolveGatewayDeadline(ctx context.Context) time.Time {
	if deadline, ok := ctx.Deadline(); ok {
		return deadline
	}
	return time.Now().Add(gatewayRequestTimeout)
}

func resolveGatewayToken(config bridgeconfig.Config) string {
	if token := strings.TrimSpace(config.Runtime.Token); token != "" {
		return token
	}
	return strings.TrimSpace(os.Getenv("OPENCLAW_GATEWAY_TOKEN"))
}

func buildGatewayConnectDevice(
	config bridgeconfig.Config,
	params gatewayConnectParams,
	token string,
	nonce string,
) (*gatewayConnectDevice, error) {
	identity, err := loadOrCreateGatewayDeviceIdentity(resolveGatewayDeviceIdentityPath(config))
	if err != nil {
		return nil, err
	}

	signedAtMS := time.Now().UnixMilli()
	payload := buildGatewayDeviceAuthPayloadV3(gatewayDeviceAuthPayloadParams{
		DeviceID:     identity.DeviceID,
		ClientID:     params.Client.ID,
		ClientMode:   params.Client.Mode,
		Role:         params.Role,
		Scopes:       params.Scopes,
		SignedAtMS:   signedAtMS,
		Token:        token,
		Nonce:        nonce,
		Platform:     params.Client.Platform,
		DeviceFamily: params.Client.DeviceFamily,
	})
	signature, err := signGatewayDevicePayload(identity.PrivateKeyPEM, payload)
	if err != nil {
		return nil, err
	}
	publicKey, err := publicKeyRawBase64URLFromPEM(identity.PublicKeyPEM)
	if err != nil {
		return nil, err
	}

	return &gatewayConnectDevice{
		ID:        identity.DeviceID,
		PublicKey: publicKey,
		Signature: signature,
		SignedAt:  signedAtMS,
		Nonce:     nonce,
	}, nil
}
