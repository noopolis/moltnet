package openclaw

import (
	"strconv"
	"strings"
)

type gatewayDeviceAuthPayloadParams struct {
	DeviceID     string
	ClientID     string
	ClientMode   string
	Role         string
	Scopes       []string
	SignedAtMS   int64
	Token        string
	Nonce        string
	Platform     string
	DeviceFamily string
}

func buildGatewayDeviceAuthPayloadV3(params gatewayDeviceAuthPayloadParams) string {
	return strings.Join([]string{
		"v3",
		params.DeviceID,
		params.ClientID,
		params.ClientMode,
		params.Role,
		strings.Join(params.Scopes, ","),
		strconv.FormatInt(params.SignedAtMS, 10),
		params.Token,
		params.Nonce,
		normalizeDeviceMetadataForAuth(params.Platform),
		normalizeDeviceMetadataForAuth(params.DeviceFamily),
	}, "|")
}

func normalizeDeviceMetadataForAuth(value string) string {
	return strings.TrimSpace(value)
}
