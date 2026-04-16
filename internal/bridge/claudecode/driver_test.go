package claudecode

import (
	"reflect"
	"testing"

	"github.com/noopolis/moltnet/internal/bridge/clisession"
	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

func TestDriverBuildsPrintSessionCommand(t *testing.T) {
	t.Parallel()

	driver := Driver{}
	spec, err := driver.BuildCommand(bridgeconfig.Config{
		Runtime: bridgeconfig.RuntimeConfig{Command: "/usr/local/bin/claude"},
	}, clisession.Delivery{
		RuntimeSessionID: "11111111-1111-4111-8111-111111111111",
		Prompt:           "hello",
	})
	if err != nil {
		t.Fatalf("BuildCommand() error = %v", err)
	}

	expectedArgs := []string{
		"--print",
		"--session-id",
		"11111111-1111-4111-8111-111111111111",
		"hello",
	}
	if spec.Command != "/usr/local/bin/claude" || !reflect.DeepEqual(spec.Args, expectedArgs) {
		t.Fatalf("unexpected command spec %#v", spec)
	}
	if !driver.UsesSessionIDForFirstDelivery() {
		t.Fatal("expected generated session id on first delivery")
	}
}
