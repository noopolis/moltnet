package app

import (
	"reflect"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
	"github.com/noopolis/moltnet/pkg/nodeconfig"
	"github.com/noopolis/moltnet/pkg/protocol"
)

// This guard lives in app because it can inspect app's unexported file-config
// types while importing the public protocol, bridge, and node configuration
// packages without creating an import cycle.
func TestSecretFieldsUseSecretString(t *testing.T) {
	types := []reflect.Type{
		reflect.TypeOf(protocol.Room{}),
		reflect.TypeOf(protocol.Pairing{}),
		reflect.TypeOf(protocol.PairingRelay{}),
		reflect.TypeOf(protocol.CreateRoomRequest{}),
		reflect.TypeOf(protocol.Event{}),
		reflect.TypeOf(rawConfigFile{}),
		reflect.TypeOf(RoomConfig{}),
		reflect.TypeOf(rawAuthTokenConfig{}),
		reflect.TypeOf(bridgeconfig.Config{}),
		reflect.TypeOf(nodeconfig.Config{}),
	}
	for _, typ := range types {
		walkSecretFields(t, typ, typ.String(), make(map[reflect.Type]bool))
	}

	value, ok := reflect.TypeOf(rawAuthTokenConfig{}).FieldByName("Value")
	if !ok || value.Type != reflect.TypeOf(protocol.SecretString("")) {
		t.Fatal("rawAuthTokenConfig.Value must use protocol.SecretString")
	}
}

func walkSecretFields(t *testing.T, typ reflect.Type, path string, seen map[reflect.Type]bool) {
	t.Helper()
	if typ.Kind() == reflect.Pointer || typ.Kind() == reflect.Slice {
		walkSecretFields(t, typ.Elem(), path, seen)
		return
	}
	if typ.Kind() != reflect.Struct || !isMoltnetType(typ) || seen[typ] {
		return
	}
	seen[typ] = true

	for index := 0; index < typ.NumField(); index++ {
		field := typ.Field(index)
		fieldPath := path + "." + field.Name
		if hasSecretName(field.Name) && !allowedSecretName(field.Name) && isPlaintextSecretType(field.Type) {
			t.Errorf("%s has plaintext type %s; use protocol.SecretString", fieldPath, field.Type)
		}
		walkSecretFields(t, field.Type, fieldPath, seen)
	}
}

func hasSecretName(name string) bool {
	lower := strings.ToLower(name)
	for _, marker := range []string{"token", "secret", "credential", "key", "password"} {
		if strings.Contains(lower, marker) {
			return true
		}
	}
	return false
}

func allowedSecretName(name string) bool {
	switch name {
	case "CredentialKey":
		// CredentialKey identifies a credential; it is not credential material.
		return true
	case "TokenEnv":
		// TokenEnv is an environment-variable name, never the token value.
		return true
	case "TokenPath":
		// TokenPath is a filesystem location, never the token value.
		return true
	}
	return false
}

func isPlaintextSecretType(typ reflect.Type) bool {
	stringType := reflect.TypeOf("")
	return typ == stringType ||
		typ == reflect.TypeOf([]byte(nil)) ||
		typ == reflect.TypeOf((*string)(nil)) ||
		typ == reflect.TypeOf([]string(nil))
}

func isMoltnetType(typ reflect.Type) bool {
	return strings.HasPrefix(typ.PkgPath(), "github.com/noopolis/moltnet/") ||
		typ.PkgPath() == "github.com/noopolis/moltnet/internal/app"
}
