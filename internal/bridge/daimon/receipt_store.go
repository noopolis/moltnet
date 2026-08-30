package daimon

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"reflect"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

const (
	receiptStoreVersion = "moltnet.daimon-receipts.v1"
	maxReceiptJobs      = 4096
	maxReceiptStoreSize = 64 << 20

	receiptJobPending        = "pending"
	receiptJobPublished      = "published"
	receiptJobNoReply        = "completed_without_reply"
	receiptJobRuntimeFailed  = "runtime_failed"
	receiptJobRuntimeStopped = "runtime_stopped"
)

type receiptJob struct {
	AcceptanceID   string         `json:"acceptance_id"`
	RuntimeAgentID string         `json:"runtime_agent_id"`
	DeliveryID     string         `json:"delivery_id"`
	RequestDigest  string         `json:"request_digest"`
	MoltnetAgent   protocol.Actor `json:"moltnet_agent"`
	Event          protocol.Event `json:"event"`
	State          string         `json:"state"`
	AcceptedAt     time.Time      `json:"accepted_at"`
	UpdatedAt      time.Time      `json:"updated_at"`
	Code           string         `json:"code,omitempty"`
}

type receiptStoreFile struct {
	Version string                `json:"version"`
	Jobs    map[string]receiptJob `json:"jobs"`
}

type receiptStore struct {
	path string
	mu   sync.Mutex
	data receiptStoreFile
}

func openReceiptStore(path string) (*receiptStore, error) {
	if !filepath.IsAbs(path) {
		return nil, fmt.Errorf("Daimon receipt store path must be absolute")
	}
	directory := filepath.Dir(path)
	if err := os.MkdirAll(directory, 0o700); err != nil {
		return nil, fmt.Errorf("create Daimon receipt store directory: %w", err)
	}
	info, err := os.Lstat(directory)
	if err != nil || !info.IsDir() || info.Mode()&os.ModeSymlink != 0 || info.Mode().Perm()&0o077 != 0 {
		return nil, fmt.Errorf("Daimon receipt store directory must be a private directory")
	}

	store := &receiptStore{path: path, data: receiptStoreFile{Version: receiptStoreVersion, Jobs: map[string]receiptJob{}}}
	fileInfo, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return store, nil
	}
	if err != nil {
		return nil, fmt.Errorf("inspect Daimon receipt store: %w", err)
	}
	if !fileInfo.Mode().IsRegular() || fileInfo.Mode().Perm()&0o077 != 0 || fileInfo.Size() > maxReceiptStoreSize {
		return nil, fmt.Errorf("Daimon receipt store must be a private regular file")
	}
	file, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open Daimon receipt store: %w", err)
	}
	openedInfo, statErr := file.Stat()
	if statErr != nil || !os.SameFile(fileInfo, openedInfo) {
		file.Close()
		return nil, fmt.Errorf("Daimon receipt store changed during validation")
	}
	contents, err := io.ReadAll(io.LimitReader(file, maxReceiptStoreSize+1))
	closeErr := file.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil || len(contents) > maxReceiptStoreSize {
		return nil, fmt.Errorf("read Daimon receipt store: invalid bounded file")
	}
	decoder := json.NewDecoder(strings.NewReader(string(contents)))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&store.data); err != nil {
		return nil, fmt.Errorf("decode Daimon receipt store: %w", err)
	}
	var trailing json.RawMessage
	if err := decoder.Decode(&trailing); err != io.EOF {
		return nil, fmt.Errorf("decode Daimon receipt store: trailing data")
	}
	if store.data.Version != receiptStoreVersion || store.data.Jobs == nil {
		return nil, fmt.Errorf("unsupported Daimon receipt store")
	}
	for key, job := range store.data.Jobs {
		if key != job.AcceptanceID || validateReceiptJob(job) != nil {
			return nil, fmt.Errorf("Daimon receipt store contains an invalid job")
		}
	}
	return store, nil
}

func (s *receiptStore) Put(job receiptJob) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := validateReceiptJob(job); err != nil {
		return err
	}
	if prior, ok := s.data.Jobs[job.AcceptanceID]; ok {
		if !sameReceiptJob(prior, job) {
			return fmt.Errorf("Daimon receipt acceptance conflicts with durable state")
		}
		return nil
	}
	priorJobs := cloneReceiptJobs(s.data.Jobs)
	if len(s.data.Jobs) >= maxReceiptJobs {
		s.pruneTerminal()
	}
	if len(s.data.Jobs) >= maxReceiptJobs {
		return fmt.Errorf("Daimon receipt store has too many pending jobs")
	}
	s.data.Jobs[job.AcceptanceID] = job
	if err := s.save(); err != nil {
		s.data.Jobs = priorJobs
		return err
	}
	return nil
}

func (s *receiptStore) Pending() []receiptJob {
	s.mu.Lock()
	defer s.mu.Unlock()
	jobs := make([]receiptJob, 0, len(s.data.Jobs))
	for _, job := range s.data.Jobs {
		if job.State == receiptJobPending {
			jobs = append(jobs, job)
		}
	}
	sort.Slice(jobs, func(i, j int) bool {
		return jobs[i].AcceptedAt.Before(jobs[j].AcceptedAt) || jobs[i].AcceptedAt.Equal(jobs[j].AcceptedAt) && jobs[i].AcceptanceID < jobs[j].AcceptanceID
	})
	return jobs
}

func (s *receiptStore) ValidateAuthority(moltnetAgentID, runtimeAgentID string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, job := range s.data.Jobs {
		if job.MoltnetAgent.ID != moltnetAgentID || job.RuntimeAgentID != runtimeAgentID {
			return fmt.Errorf("Daimon receipt store belongs to a different attachment")
		}
	}
	return nil
}

func (s *receiptStore) MarkTerminal(acceptanceID, state, code string, now time.Time) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	job, ok := s.data.Jobs[acceptanceID]
	if !ok {
		return fmt.Errorf("Daimon receipt job is unavailable")
	}
	if job.State != receiptJobPending {
		return nil
	}
	prior := job
	job.State, job.Code, job.UpdatedAt = state, code, now.UTC()
	if err := validateReceiptJob(job); err != nil {
		return err
	}
	s.data.Jobs[acceptanceID] = job
	if err := s.save(); err != nil {
		s.data.Jobs[acceptanceID] = prior
		return err
	}
	return nil
}

func (s *receiptStore) pruneTerminal() {
	terminal := make([]receiptJob, 0, len(s.data.Jobs))
	for _, job := range s.data.Jobs {
		if job.State != receiptJobPending {
			terminal = append(terminal, job)
		}
	}
	sort.Slice(terminal, func(i, j int) bool { return terminal[i].UpdatedAt.Before(terminal[j].UpdatedAt) })
	for len(s.data.Jobs) >= maxReceiptJobs && len(terminal) > 0 {
		delete(s.data.Jobs, terminal[0].AcceptanceID)
		terminal = terminal[1:]
	}
}

// Seams for the durability tests in receipt_store_test.go, which must be able
// to fail the chmod and the write of the temporary file. Both steps write to a
// freshly created temp file, so no filesystem-level setup can make either fail
// on its own: every way to make a directory hostile fails at CreateTemp
// instead, which is a different branch. Production always runs the real
// methods; only the test replaces them, and it restores them immediately.
var (
	receiptStoreChmod = func(file *os.File, mode os.FileMode) error { return file.Chmod(mode) }
	receiptStoreWrite = func(file *os.File, contents []byte) (int, error) { return file.Write(contents) }
)

func (s *receiptStore) save() error {
	contents, err := json.MarshalIndent(s.data, "", "  ")
	if err != nil {
		return fmt.Errorf("encode Daimon receipt store: %w", err)
	}
	contents = append(contents, '\n')
	directory := filepath.Dir(s.path)
	temporary, err := os.CreateTemp(directory, ".daimon-receipts-*")
	if err != nil {
		return fmt.Errorf("create Daimon receipt store temporary file: %w", err)
	}
	temporaryPath := temporary.Name()
	defer os.Remove(temporaryPath)
	if err = receiptStoreChmod(temporary, 0o600); err == nil {
		_, err = receiptStoreWrite(temporary, contents)
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write Daimon receipt store: %w", err)
	}
	if err := os.Rename(temporaryPath, s.path); err != nil {
		return fmt.Errorf("replace Daimon receipt store: %w", err)
	}
	dir, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open Daimon receipt store directory: %w", err)
	}
	err = dir.Sync()
	closeErr = dir.Close()
	if err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("sync Daimon receipt store directory: %w", err)
	}
	return nil
}

func validateReceiptJob(job receiptJob) error {
	if !acceptanceIDPattern.MatchString(job.AcceptanceID) || protocol.ValidateMessageID(job.RuntimeAgentID) != nil || !digestPattern.MatchString(job.RequestDigest) {
		return fmt.Errorf("Daimon receipt job identity is invalid")
	}
	if job.MoltnetAgent.Type != "agent" || protocol.ValidateMessageID(job.MoltnetAgent.ID) != nil || job.Event.Type != protocol.EventTypeMessageCreated || job.Event.Message == nil || protocol.ValidateMessageID(job.Event.Message.ID) != nil || protocol.MessageEventID(job.Event.Message.ID) != job.DeliveryID {
		return fmt.Errorf("Daimon receipt job source event is invalid")
	}
	if err := protocol.ValidateTarget(job.Event.Message.Target); err != nil {
		return fmt.Errorf("Daimon receipt job target is invalid")
	}
	switch job.State {
	case receiptJobPending, receiptJobPublished, receiptJobNoReply, receiptJobRuntimeFailed, receiptJobRuntimeStopped:
	default:
		return fmt.Errorf("Daimon receipt job state is invalid")
	}
	if job.AcceptedAt.IsZero() || job.UpdatedAt.Before(job.AcceptedAt) || len(job.Code) > 128 {
		return fmt.Errorf("Daimon receipt job lifecycle is invalid")
	}
	return nil
}

func sameReceiptJob(left, right receiptJob) bool {
	return left.AcceptanceID == right.AcceptanceID && left.RuntimeAgentID == right.RuntimeAgentID && left.DeliveryID == right.DeliveryID && left.RequestDigest == right.RequestDigest && left.AcceptedAt.Equal(right.AcceptedAt) && reflect.DeepEqual(left.MoltnetAgent, right.MoltnetAgent) && reflect.DeepEqual(left.Event, right.Event)
}

func cloneReceiptJobs(source map[string]receiptJob) map[string]receiptJob {
	result := make(map[string]receiptJob, len(source))
	for key, job := range source {
		result[key] = job
	}
	return result
}
