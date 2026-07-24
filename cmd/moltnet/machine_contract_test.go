package main

import (
	"bytes"
	"context"
	"strings"
	"testing"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func TestMachineContractCommandPrintsCanonicalArtifact(t *testing.T) {
	want, _, err := protocol.MarshalMachineContractV1()
	if err != nil {
		t.Fatal(err)
	}
	var output bytes.Buffer
	withMachineStdout(t, &output, func() {
		err = run(context.Background(), []string{"machine-contract"}, "test")
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := output.String(); got != string(want)+"\n" {
		t.Fatalf("machine-contract output drifted: got %d bytes want %d", len(got), len(want)+1)
	}
	if err := runMachineContract([]string{"unexpected"}); err == nil ||
		!strings.Contains(err.Error(), "does not accept arguments") {
		t.Fatalf("unexpected argument error = %v", err)
	}
	if !strings.Contains(buildUsage(), "moltnet machine-contract") {
		t.Fatal("top-level usage omits machine-contract")
	}
}
