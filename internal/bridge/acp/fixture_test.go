package acp

import (
	"bufio"
	"bytes"
	"encoding/json"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

const literalArgument = "argument with spaces; $(false)"
const privateOutput = "fixture model output must stay private"

type fixtureEvent struct {
	Method    string `json:"method"`
	SessionID string `json:"sessionId,omitempty"`
	Text      string `json:"text,omitempty"`
}

type fixture struct {
	t        *testing.T
	input    *bufio.Scanner
	output   *json.Encoder
	log      *json.Encoder
	mode     string
	signal   string
	session  int
	sessions map[string]bool
	held     rpcMessage
	queued   []rpcMessage
}

func TestACPFixture(t *testing.T) {
	index := slices.Index(os.Args, "--")
	if index < 0 {
		return
	}
	args := os.Args[index+1:]
	if len(args) != 4 || args[3] != literalArgument {
		t.Fatalf("unexpected fixture arguments: %q", args)
	}
	log, err := os.OpenFile(args[0], os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0o600)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	f := fixture{
		t: t, input: bufio.NewScanner(os.Stdin), output: json.NewEncoder(os.Stdout),
		log: json.NewEncoder(log), mode: args[1], signal: args[2], sessions: make(map[string]bool),
	}
	f.run()
	// The Go test harness would append PASS to protocol stdout.
	os.Exit(0)
}

func (f *fixture) run() {
	init := f.read()
	var params struct {
		Version      int            `json:"protocolVersion"`
		Capabilities map[string]any `json:"clientCapabilities"`
	}
	f.decode(init.Params, &params)
	if init.Method != "initialize" || params.Version != 1 || len(params.Capabilities) != 0 {
		f.t.Fatalf("invalid initialization: %+v %+v", init, params)
	}
	f.record(fixtureEvent{Method: init.Method})
	// A string ID is distinct from the numeric ID of our in-flight request.
	f.send(rpcResponse{Version: rpcVersion, ID: json.RawMessage(strconv.Quote(string(init.ID))), Result: map[string]int{"protocolVersion": 999}})
	version := 1
	if f.mode == "bad-version" {
		version = 2
	}
	f.reply(init, map[string]any{"protocolVersion": version, "agentCapabilities": map[string]bool{"loadSession": f.mode == "load"}})

	for {
		var request rpcMessage
		if len(f.queued) > 0 {
			request = f.queued[0]
			f.queued = f.queued[1:]
		} else {
			if !f.input.Scan() {
				break
			}
			f.decode(f.input.Bytes(), &request)
		}
		switch request.Method {
		case "session/new", "session/load":
			f.setup(request)
		case "session/prompt":
			f.prompt(request)
		case "session/cancel":
			var session sessionReference
			f.decode(request.Params, &session)
			var held sessionPrompt
			f.decode(f.held.Params, &held)
			if len(request.ID) != 0 || session.SessionID != held.SessionID {
				f.t.Fatalf("invalid cancellation: %+v", request)
			}
			f.record(fixtureEvent{Method: request.Method, SessionID: session.SessionID})
			f.checkPermission(f.held.ID, session.SessionID, "cancelled", "")
			f.reply(f.held, map[string]string{"stopReason": "cancelled"})
		default:
			f.t.Fatalf("unexpected request: %+v", request)
		}
	}
	if err := f.input.Err(); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) setup(request rpcMessage) {
	var setup sessionSetup
	f.decode(request.Params, &setup)
	dir, err := os.Getwd()
	if err != nil {
		f.t.Fatal(err)
	}
	if dir != setup.Directory || setup.MCPServers == nil || len(setup.MCPServers) != 0 {
		f.t.Fatalf("invalid session setup: %+v (cwd %q)", setup, dir)
	}
	if request.Method == "session/load" {
		if f.mode != "load" || setup.SessionID == "" {
			f.t.Fatalf("unexpected session load: %+v", setup)
		}
		f.sessions[setup.SessionID] = true
		f.record(fixtureEvent{Method: request.Method, SessionID: setup.SessionID})
		f.reply(request, struct{}{})
		return
	}
	if setup.SessionID != "" {
		f.t.Fatalf("unexpected initial session ID in %+v", setup)
	}
	f.session++
	id := fmt.Sprintf("fixture-session-%d", f.session)
	f.sessions[id] = true
	f.record(fixtureEvent{Method: request.Method, SessionID: id})
	f.reply(request, sessionReference{SessionID: id})
}

func (f *fixture) prompt(request rpcMessage) {
	var prompt sessionPrompt
	f.decode(request.Params, &prompt)
	if !f.sessions[prompt.SessionID] || len(prompt.Prompt) != 1 || prompt.Prompt[0].Type != "text" {
		f.t.Fatalf("invalid session prompt: %+v", prompt)
	}
	if f.mode == "attachment" {
		config := os.Getenv("MOLTNET_CLIENT_CONFIG")
		expected := filepath.Join(mustGetwd(f.t), ".moltnet", "config.json")
		if config != expected {
			f.t.Fatalf("MOLTNET_CLIENT_CONFIG = %q, want %q", config, expected)
		}
		if _, err := os.Stat(config); err != nil {
			f.t.Fatal(err)
		}
	}
	text := prompt.Prompt[0].Text
	f.record(fixtureEvent{Method: request.Method, SessionID: prompt.SessionID, Text: text})
	switch text {
	case "hold":
		f.held = request
		client, err := net.DialTimeout("tcp", f.signal, 10*time.Second)
		if err != nil {
			f.t.Fatal(err)
		}
		client.Close()
		return
	case "release":
		f.reply(request, map[string]string{"stopReason": "end_turn"})
		f.send(rpcResponse{Version: rpcVersion, ID: f.held.ID, Error: &rpcError{Code: -32000, Message: "fixture prompt failure"}})
		return
	case "exit":
		os.Exit(3)
	}

	f.send(rpcRequest{Version: rpcVersion, Method: "session/update", Params: map[string]any{
		"sessionId": prompt.SessionID,
		"update": map[string]any{
			"sessionUpdate": "agent_message_chunk",
			"content":       textBlock{Type: "text", Text: privateOutput},
		},
	}})
	if f.mode == "attachment" {
		f.checkClientRequests(request.ID, prompt.SessionID)
		f.record(fixtureEvent{Method: "prompt/complete", SessionID: prompt.SessionID})
	}
	f.reply(request, map[string]string{"stopReason": "end_turn"})
}

func (f *fixture) checkPermission(id json.RawMessage, sessionID, outcome, optionID string) {
	// Agents can use the same ID as an in-flight client request.
	f.send(rpcRequest{Version: rpcVersion, ID: id, Method: "session/request_permission", Params: map[string]any{
		"sessionId": sessionID,
		"options": []map[string]string{
			{"kind": "allow_once", "optionId": "yes", "name": "Allow"},
			{"kind": "reject_once", "optionId": "no", "name": "Reject"},
		},
	}})
	reply := f.readClientResponse()
	var permission struct {
		Outcome map[string]string `json:"outcome"`
	}
	f.decode(reply.Result, &permission)
	if string(reply.ID) != string(id) || permission.Outcome["outcome"] != outcome || permission.Outcome["optionId"] != optionID {
		f.t.Fatalf("unexpected permission response: %+v %+v", reply, permission)
	}
}

func (f *fixture) checkClientRequests(id json.RawMessage, sessionID string) {
	f.checkPermission(id, sessionID, "selected", "no")

	const fileRequestID = `"host-file-request"`
	f.send(rpcRequest{Version: rpcVersion, ID: json.RawMessage(fileRequestID), Method: "fs/read_text_file", Params: map[string]string{"path": "/unavailable"}})
	reply := f.readClientResponse()
	if string(reply.ID) != fileRequestID || reply.Error == nil || reply.Error.Code != -32601 {
		f.t.Fatalf("unexpected file response: %+v", reply)
	}
}

func (f *fixture) readClientResponse() rpcMessage {
	for {
		message := f.read()
		if message.Method == "" {
			return message
		}
		f.queued = append(f.queued, message)
	}
}

func (f *fixture) read() rpcMessage {
	f.t.Helper()
	var message rpcMessage
	if !f.input.Scan() {
		f.t.Fatalf("no ACP request or response: %v", f.input.Err())
	}
	f.decode(f.input.Bytes(), &message)
	return message
}

func (f *fixture) reply(request rpcMessage, result any) {
	f.t.Helper()
	f.send(rpcResponse{Version: rpcVersion, ID: request.ID, Result: result})
}

func (f *fixture) decode(raw []byte, dst any) {
	f.t.Helper()
	if err := json.Unmarshal(raw, dst); err != nil {
		f.t.Fatalf("decode %s: %v", raw, err)
	}
}

func (f *fixture) send(value any) {
	f.t.Helper()
	if err := f.output.Encode(value); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fixture) record(value fixtureEvent) {
	f.t.Helper()
	if err := f.log.Encode(value); err != nil {
		f.t.Fatal(err)
	}
}

func fixtureConfig(t *testing.T, mode, signal string) (bridgeconfig.Config, string) {
	t.Helper()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	workspace, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	trace := filepath.Join(workspace, "fixture.jsonl")
	return bridgeconfig.Config{
		Version: bridgeconfig.VersionV1,
		Agent:   bridgeconfig.AgentConfig{ID: "acp-bot"},
		Moltnet: bridgeconfig.MoltnetConfig{BaseURL: "http://127.0.0.1", NetworkID: "local"},
		Runtime: bridgeconfig.RuntimeConfig{
			Kind: bridgeconfig.RuntimeACP, Command: executable, WorkspacePath: workspace,
			Args: []string{"-test.run=^TestACPFixture$", "--", trace, mode, signal, literalArgument},
		},
	}, trace
}

func readFixtureTrace(t *testing.T, path string) []fixtureEvent {
	t.Helper()
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var events []fixtureEvent
	for _, line := range bytes.Split(bytes.TrimSpace(data), []byte("\n")) {
		var event fixtureEvent
		if err := json.Unmarshal(line, &event); err != nil {
			t.Fatal(err)
		}
		events = append(events, event)
	}
	return events
}

func mustGetwd(t *testing.T) string {
	t.Helper()
	dir, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	return dir
}
