package protocol

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
)

const MachineContractVersionV1 = "moltnet.machine-contract.v1"

type MachineContractVector struct {
	Name      string `json:"name"`
	Direction string `json:"direction"`
	Line      string `json:"line"`
	SHA256    string `json:"sha256"`
}

type MachineContractField struct {
	Name     string   `json:"name"`
	Type     string   `json:"type"`
	Required bool     `json:"required"`
	Nullable bool     `json:"nullable,omitempty"`
	Limit    string   `json:"limit,omitempty"`
	Enum     []string `json:"enum,omitempty"`
}

type MachineContractShape struct {
	Name      string                    `json:"name"`
	Fields    []MachineContractField    `json:"fields"`
	Relations []MachineContractRelation `json:"relations,omitempty"`
}

type MachineContractRelation struct {
	Kind   string   `json:"kind"`
	Fields []string `json:"fields"`
	Value  string   `json:"value,omitempty"`
}

type MachineContractGrammar struct {
	Pattern   string   `json:"pattern,omitempty"`
	MinBytes  int      `json:"min_bytes,omitempty"`
	MaxBytes  int      `json:"max_bytes,omitempty"`
	Trimmed   bool     `json:"trimmed,omitempty"`
	LocalOnly bool     `json:"local_only,omitempty"`
	Formats   []string `json:"formats,omitempty"`
}

type MachineContractFraming struct {
	Encoding        string `json:"encoding"`
	RecordSeparator string `json:"record_separator"`
	StrictJSON      bool   `json:"strict_json"`
}

type MachineContract struct {
	Version         string                            `json:"version"`
	ProtocolVersion string                            `json:"protocol_version"`
	Framing         MachineContractFraming            `json:"framing"`
	Operations      []string                          `json:"operations"`
	Enums           map[string][]string               `json:"enums"`
	Grammars        map[string]MachineContractGrammar `json:"grammars"`
	Limits          map[string]int                    `json:"limits"`
	Shapes          []MachineContractShape            `json:"shapes"`
	Vectors         []MachineContractVector           `json:"vectors"`
}

func MachineContractV1() (MachineContract, error) {
	vectors, err := machineContractVectors()
	if err != nil {
		return MachineContract{}, err
	}
	return MachineContract{
		Version:         MachineContractVersionV1,
		ProtocolVersion: MachineProtocolV1,
		Framing: MachineContractFraming{
			Encoding:        "utf-8-json",
			RecordSeparator: "\n",
			StrictJSON:      true,
		},
		Operations: append([]string(nil), machineOperationValues...),
		Enums:      machineContractEnums(),
		Grammars:   machineContractGrammars(),
		Limits:     machineContractLimits(),
		Shapes:     machineContractShapes(),
		Vectors:    vectors,
	}, nil
}

func machineContractVectors() ([]MachineContractVector, error) {
	requests := machineRequestFixtures()
	responses := machineResponseFixtures()
	vectors := make([]MachineContractVector, 0, len(requests)+len(responses))
	appendVector := func(name, direction, line string) {
		digest := sha256.Sum256([]byte(line))
		vectors = append(vectors, MachineContractVector{
			Name: name, Direction: direction, Line: line, SHA256: hex.EncodeToString(digest[:]),
		})
	}
	for _, fixture := range requests {
		line, err := EncodeMachineRequestLine(fixture.request)
		if err != nil {
			return nil, err
		}
		appendVector(fixture.name, "request", line)
	}
	for _, fixture := range responses {
		line, err := EncodeMachineResponseLine(fixture.response)
		if err != nil {
			return nil, err
		}
		appendVector(fixture.name, "response", line)
	}
	return vectors, nil
}

func MarshalMachineContractV1() ([]byte, string, error) {
	contract, err := MachineContractV1()
	if err != nil {
		return nil, "", err
	}
	bytes, err := json.Marshal(contract)
	if err != nil {
		return nil, "", err
	}
	digest := sha256.Sum256(bytes)
	return bytes, hex.EncodeToString(digest[:]), nil
}
