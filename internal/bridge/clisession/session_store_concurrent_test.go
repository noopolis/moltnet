package clisession

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strconv"
	"testing"
	"time"
)

const (
	concurrentSessionWriters = 4
	sessionsPerWriter        = 20
)

type sharedSessionResult struct {
	Record  SessionRecord
	Existed bool
}

func TestSessionStoreConcurrentProcesses(t *testing.T) {
	directory := t.TempDir()
	path := filepath.Join(directory, "sessions.json")
	start, release, err := os.Pipe()
	if err != nil {
		t.Fatal(err)
	}
	defer start.Close()
	defer release.Close()

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}

	type writer struct {
		command *exec.Cmd
		output  bytes.Buffer
	}
	writers := make([]writer, concurrentSessionWriters)
	for index := range writers {
		child := &writers[index]
		child.command = exec.CommandContext(ctx, executable, "-test.run=^TestSessionStoreWriterHelper$", "--", path, strconv.Itoa(index))
		child.command.Stdin = start
		child.command.Stdout = &child.output
		child.command.Stderr = &child.output
		if err := child.command.Start(); err != nil {
			t.Fatal(err)
		}
		t.Cleanup(func() {
			if child.command.ProcessState == nil {
				_ = child.command.Process.Kill()
				_ = child.command.Wait()
			}
		})
	}
	// Each child blocks reading stdin until every writer has launched.
	if err := release.Close(); err != nil {
		t.Fatal(err)
	}

	shared := make([]sharedSessionResult, len(writers))
	for index := range writers {
		child := &writers[index]
		if err := child.command.Wait(); err != nil {
			t.Errorf("writer %d: %v\n%s", index, err, child.output.String())
			continue
		}
		result, err := os.ReadFile(writerResultPath(path, index))
		if err != nil {
			t.Fatal(err)
		}
		if err := json.Unmarshal(result, &shared[index]); err != nil {
			t.Fatal(err)
		}
	}

	// Use a fresh store to confirm the file retains every accepted mapping.
	store := NewSessionStore(path)
	for writer := range writers {
		for index := range sessionsPerWriter {
			key := writerSessionKey(writer, index)
			record, found, err := store.Get(key)
			if err != nil || !found || record.RuntimeSessionID != key {
				t.Errorf("Get(%q) = %+v, found=%t, err=%v", key, record, found, err)
			}
		}
	}
	record, found, err := store.Get("shared")
	if err != nil || !found || record.RuntimeSessionID == "" {
		t.Fatalf("shared session = %+v, found=%t, err=%v", record, found, err)
	}
	created := 0
	for _, result := range shared {
		if !result.Existed {
			created++
		}
		if result.Record != record {
			t.Errorf("GetOrCreate returned %+v, durable record is %+v", result.Record, record)
		}
	}
	if created != 1 {
		t.Errorf("%d writers created the shared session, want one", created)
	}

	for _, privateFile := range []string{path, path + ".lock"} {
		info, err := os.Stat(privateFile)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != 0o600 {
			t.Errorf("%s mode = %o, want 600", privateFile, info.Mode().Perm())
		}
	}
	entries, err := filepath.Glob(filepath.Join(directory, ".cli-sessions-*"))
	if err != nil || len(entries) != 0 {
		t.Errorf("leftover temporary session files: %v, err=%v", entries, err)
	}
}

func TestSessionStoreWriterHelper(t *testing.T) {
	if len(os.Args) < 4 || os.Args[len(os.Args)-3] != "--" {
		return
	}
	path := os.Args[len(os.Args)-2]
	writer, err := strconv.Atoi(os.Args[len(os.Args)-1])
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.Copy(io.Discard, os.Stdin); err != nil {
		t.Fatal(err)
	}

	store := NewSessionStore(path)
	record, existed, err := store.GetOrCreate("shared")
	if err != nil {
		t.Fatal(err)
	}
	result, err := json.Marshal(sharedSessionResult{Record: record, Existed: existed})
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(writerResultPath(path, writer), result, 0o600); err != nil {
		t.Fatal(err)
	}
	for index := range sessionsPerWriter {
		key := writerSessionKey(writer, index)
		if _, err := store.Put(key, key); err != nil {
			t.Fatal(err)
		}
	}
}

func writerResultPath(path string, writer int) string {
	return filepath.Join(filepath.Dir(path), fmt.Sprintf("writer-%d.json", writer))
}

func writerSessionKey(writer, index int) string {
	return fmt.Sprintf("writer-%d-session-%d", writer, index)
}
