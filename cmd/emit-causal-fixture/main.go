// Command emit-causal-fixture prints two schema-valid message.accepted
// causal-event.v1 JSONL records to stdout. It is moltnet's row in the root
// repo's cross-repo causal conformance harness (src/ledger/emitters.ts),
// mirroring the simfile/mneme/daimon fixture emitters.
package main

import (
	"fmt"
	"os"
	"strings"

	"github.com/noopolis/moltnet/internal/observability"
)

const runIDEnv = "NOOPOLIS_RUN_ID"
const defaultRunID = "fixture-run-moltnet"

func main() {
	if err := run(os.Stdout); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(stdout *os.File) error {
	runID := strings.TrimSpace(os.Getenv(runIDEnv))
	if runID == "" {
		runID = defaultRunID
	}
	return observability.WriteMessageAcceptedFixture(stdout, runID)
}
