package openclaw

import (
	"time"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

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
