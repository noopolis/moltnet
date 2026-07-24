package main

import (
	"fmt"

	"github.com/noopolis/moltnet/pkg/protocol"
)

func runMachineContract(args []string) error {
	if len(args) != 0 {
		return fmt.Errorf("machine-contract does not accept arguments")
	}
	bytes, _, err := protocol.MarshalMachineContractV1()
	if err != nil {
		return err
	}
	_, err = fmt.Fprintln(stdout, string(bytes))
	return err
}
