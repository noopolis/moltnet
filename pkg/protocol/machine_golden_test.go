package protocol

import (
	"bytes"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestMachineContractArtifactMatchesCanonicalMarshal(t *testing.T) {
	t.Parallel()

	want, _, err := MarshalMachineContractV1()
	if err != nil {
		t.Fatal(err)
	}
	got, err := os.ReadFile(filepath.Join("testdata", "moltnet.machine-contract.v1.json"))
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(got, want) {
		t.Fatal("machine contract artifact drifted from provider source")
	}
}

func TestMachineContractNodeConsumer(t *testing.T) {
	node, err := exec.LookPath("node")
	if err != nil {
		t.Fatal("node is required for machine-contract consumer proof")
	}
	script := filepath.Join("testdata", "verify-machine-contract.mjs")
	artifact := filepath.Join("testdata", "moltnet.machine-contract.v1.json")
	run := func(path string) (string, error) {
		command := exec.Command(node, script, path)
		output, runErr := command.CombinedOutput()
		if runErr != nil {
			return string(output), fmt.Errorf("%w: %s", runErr, output)
		}
		return string(output), nil
	}
	if _, err := run(artifact); err != nil {
		t.Fatalf("node consumer: %v", err)
	}

	source, err := os.ReadFile(artifact)
	if err != nil {
		t.Fatal(err)
	}
	for name, mutation := range map[string]struct{ old, new string }{
		"unknown_relation": {`"kind":"mutually_exclusive"`, `"kind":"unknown_relation"`},
		"unknown_limit":    {`"limit":"max_correlation_bytes"`, `"limit":"unknown_limit"`},
		"vector_hash":      {`"sha256":"`, `"sha256":"0`},
	} {
		t.Run(name, func(t *testing.T) {
			changed := bytes.Replace(source, []byte(mutation.old), []byte(mutation.new), 1)
			if bytes.Equal(changed, source) {
				t.Fatalf("mutation %s did not alter artifact", name)
			}
			path := filepath.Join(t.TempDir(), "contract.json")
			if err := os.WriteFile(path, changed, 0o600); err != nil {
				t.Fatal(err)
			}
			if _, err := run(path); err == nil {
				t.Fatalf("node consumer accepted %s drift", name)
			}
		})
	}

	decode := func(t *testing.T) map[string]any {
		t.Helper()
		var value map[string]any
		if err := json.Unmarshal(source, &value); err != nil {
			t.Fatal(err)
		}
		return value
	}
	writeMutation := func(t *testing.T, value map[string]any) string {
		t.Helper()
		encoded, err := json.Marshal(value)
		if err != nil {
			t.Fatal(err)
		}
		path := filepath.Join(t.TempDir(), "contract.json")
		if err := os.WriteFile(path, encoded, 0o600); err != nil {
			t.Fatal(err)
		}
		return path
	}
	vector := func(t *testing.T, contract map[string]any, name string) map[string]any {
		t.Helper()
		for _, raw := range contract["vectors"].([]any) {
			item := raw.(map[string]any)
			if item["name"] == name {
				var line map[string]any
				if err := json.Unmarshal([]byte(item["line"].(string)), &line); err != nil {
					t.Fatal(err)
				}
				return line
			}
		}
		t.Fatalf("missing vector %q", name)
		return nil
	}
	storeVector := func(t *testing.T, contract map[string]any, name string, line map[string]any) {
		t.Helper()
		encoded, err := json.Marshal(line)
		if err != nil {
			t.Fatal(err)
		}
		digest := sha256.Sum256(encoded)
		for _, raw := range contract["vectors"].([]any) {
			item := raw.(map[string]any)
			if item["name"] == name {
				item["line"], item["sha256"] = string(encoded), hex.EncodeToString(digest[:])
				return
			}
		}
		t.Fatalf("missing vector %q", name)
	}
	mutations := []struct {
		name, vector, relation string
		mutate                 func(map[string]any)
	}{
		{"exactly_one", "send_nudge_request", "exactly_one", func(line map[string]any) {
			line["read"] = map[string]any{"target": map[string]any{"kind": "room", "id": "room_1"}, "limit": float64(1)}
		}},
		{"payload_key_equals_field", "send_nudge_request", "payload_key_equals_field", func(line map[string]any) { line["operation"] = "read" }},
		{"result_key_equals_field", "send_nudge_success", "result_key_equals_field", func(line map[string]any) { line["operation"] = "read" }},
		{"field_allowed_when", "subscribe_event", "field_allowed_when", func(line map[string]any) { line["operation"] = "read" }},
		{"present_iff_true", "send_nudge_success", "present_iff_true", func(line map[string]any) { delete(line["send_nudge"].(map[string]any), "thread_id") }},
		{"mutually_exclusive", "read_request", "mutually_exclusive", func(line map[string]any) { line["read"].(map[string]any)["before"] = "msg_0" }},
		{"exactly_one_when_true", "read_success_nonempty_with_after", "exactly_one_when_true", func(line map[string]any) {
			line["read"].(map[string]any)["page"].(map[string]any)["page"].(map[string]any)["next_before"] = "msg_0"
		}},
		{"absent_when_false", "read_success_empty", "absent_when_false", func(line map[string]any) {
			line["read"].(map[string]any)["page"].(map[string]any)["page"].(map[string]any)["next_after"] = "msg_0"
		}},
		{"kind_requires_only", "read_success_nonempty_with_after", "kind_requires_only", func(line map[string]any) {
			delete(line["read"].(map[string]any)["page"].(map[string]any)["messages"].([]any)[0].(map[string]any)["target"].(map[string]any), "room_id")
		}},
		{"at_least_one_nonempty", "export_request", "at_least_one_nonempty", func(line map[string]any) {
			request := line["export"].(map[string]any)
			request["room_ids"], request["dm_peer_ids"] = []any{}, []any{}
		}},
		{"sha256_utf8_matches", "export_success", "sha256_utf8_matches", func(line map[string]any) { line["export"].(map[string]any)["transcript"] = "changed" }},
	}
	for _, mutation := range mutations {
		t.Run("relation_"+mutation.name, func(t *testing.T) {
			contract := decode(t)
			line := vector(t, contract, mutation.vector)
			mutation.mutate(line)
			storeVector(t, contract, mutation.vector, line)
			output, err := run(writeMutation(t, contract))
			if err == nil || !strings.Contains(output, mutation.relation) {
				t.Fatalf("expected %s rejection, output=%q err=%v", mutation.relation, output, err)
			}
		})
	}
}

func TestMachineContractVectorsRoundTripAndShapeHashes(t *testing.T) {
	t.Parallel()

	contract, err := MachineContractV1()
	if err != nil {
		t.Fatal(err)
	}
	// The pristine corpus had 25 vectors.  The empty typed read-page is a
	// separately valid production wire state (messages:null), so it must stay
	// explicit rather than being silently conflated with a non-empty read page.
	if len(contract.Vectors) != 26 {
		t.Fatalf("vector count = %d, want 26", len(contract.Vectors))
	}
	for _, item := range contract.Vectors {
		var encoded string
		var codecErr error
		switch item.Direction {
		case "request":
			var decoded MachineRequest
			decoded, codecErr = DecodeMachineRequestLine(item.Line)
			if codecErr == nil {
				encoded, codecErr = EncodeMachineRequestLine(decoded)
			}
		case "response":
			var decoded MachineResponse
			decoded, codecErr = DecodeMachineResponseLine(item.Line)
			if codecErr == nil {
				encoded, codecErr = EncodeMachineResponseLine(decoded)
			}
		default:
			t.Fatalf("%s invalid direction %q", item.Name, item.Direction)
		}
		if codecErr != nil {
			t.Fatalf("%s codec: %v", item.Name, codecErr)
		}
		if encoded != item.Line {
			t.Fatalf("%s wire mismatch\n got  %s\nwant %s", item.Name, encoded, item.Line)
		}
		h := sha256.Sum256([]byte(item.Line))
		if got := hex.EncodeToString(h[:]); got != item.SHA256 {
			t.Fatalf("%s hash mismatch\n got  %s\nwant %s", item.Name, got, item.SHA256)
		}
	}
}

func TestMachineContractShapesTrackEveryWireField(t *testing.T) {
	t.Parallel()

	wireTypes := map[string]reflect.Type{
		"request": reflect.TypeOf(MachineRequest{}), "response": reflect.TypeOf(MachineResponse{}),
		"target": reflect.TypeOf(MachineTarget{}), "send_nudge_request": reflect.TypeOf(MachineSendNudgeRequest{}),
		"send_nudge_result": reflect.TypeOf(MachineSendNudgeResult{}), "read_request": reflect.TypeOf(MachineReadRequest{}),
		"read_result": reflect.TypeOf(MachineReadResult{}), "read_page": reflect.TypeOf(MachineReadPage{}),
		"read_page_info": reflect.TypeOf(MachineReadPageInfo{}), "read_message": reflect.TypeOf(MachineReadMessage{}),
		"message_origin": reflect.TypeOf(MessageOrigin{}), "message_target": reflect.TypeOf(Target{}),
		"message_actor": reflect.TypeOf(Actor{}), "message_part": reflect.TypeOf(Part{}),
		"subscribe_request": reflect.TypeOf(MachineSubscribeRequest{}), "subscribe_result": reflect.TypeOf(MachineSubscribeResult{}),
		"subscribe_event": reflect.TypeOf(MachineSubscribeEvent{}), "export_request": reflect.TypeOf(MachineExportRequest{}),
		"export_result": reflect.TypeOf(MachineExportResult{}), "cancel_request": reflect.TypeOf(MachineCancelRequest{}),
		"cancel_result": reflect.TypeOf(MachineCancelResult{}), "error": reflect.TypeOf(MachineError{}),
	}
	contract, err := MachineContractV1()
	if err != nil {
		t.Fatal(err)
	}
	shapes := make(map[string]MachineContractShape)
	for _, shape := range contract.Shapes {
		shapes[shape.Name] = shape
	}
	if len(shapes) != len(wireTypes) {
		t.Fatalf("shape count = %d, wire type count = %d", len(shapes), len(wireTypes))
	}
	for name, wireType := range wireTypes {
		shape, ok := shapes[name]
		if !ok {
			t.Fatalf("missing shape %q", name)
		}
		got := make([]string, 0, wireType.NumField())
		for index := 0; index < wireType.NumField(); index++ {
			got = append(got, strings.Split(wireType.Field(index).Tag.Get("json"), ",")[0])
		}
		want := make([]string, 0, len(shape.Fields))
		for _, field := range shape.Fields {
			want = append(want, field.Name)
		}
		if !reflect.DeepEqual(got, want) {
			t.Fatalf("%s fields = %v, contract = %v", name, got, want)
		}
	}
}
