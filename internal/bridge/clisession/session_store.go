package clisession

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"
)

const (
	sessionStoreVersion  = "moltnet.cli-sessions.v1"
	sessionStoreLockWait = 5 * time.Second
	sessionStoreLockPoll = 10 * time.Millisecond
)

type SessionStore struct {
	path string
}

type sessionStoreFile struct {
	Version  string                   `json:"version"`
	Sessions map[string]SessionRecord `json:"sessions"`
}

type SessionRecord struct {
	RuntimeSessionID string    `json:"runtime_session_id"`
	CreatedAt        time.Time `json:"created_at"`
	UpdatedAt        time.Time `json:"updated_at"`
}

func NewSessionStore(path string) *SessionStore {
	return &SessionStore{path: path}
}

func DefaultSessionStorePath(workspacePath string) string {
	root := strings.TrimSpace(workspacePath)
	if root == "" {
		root = "."
	}
	return filepath.Join(root, ".moltnet", "sessions.json")
}

func (s *SessionStore) Get(key string) (SessionRecord, bool, error) {
	store, err := s.load()
	if err != nil {
		return SessionRecord{}, false, err
	}

	record, ok := store.Sessions[strings.TrimSpace(key)]
	return record, ok, nil
}

func (s *SessionStore) Put(key string, runtimeSessionID string) (SessionRecord, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return SessionRecord{}, fmt.Errorf("session key is required")
	}
	runtimeSessionID = strings.TrimSpace(runtimeSessionID)
	if runtimeSessionID == "" {
		return SessionRecord{}, fmt.Errorf("runtime session id is required")
	}

	unlock, err := s.lock()
	if err != nil {
		return SessionRecord{}, err
	}
	defer unlock()
	store, err := s.load()
	if err != nil {
		return SessionRecord{}, err
	}
	return s.put(store, key, runtimeSessionID)
}

func (s *SessionStore) put(store sessionStoreFile, key, runtimeSessionID string) (SessionRecord, error) {
	now := time.Now().UTC()
	record := store.Sessions[key]
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	record.RuntimeSessionID = runtimeSessionID
	store.Sessions[key] = record

	if err := s.save(store); err != nil {
		return SessionRecord{}, err
	}
	return record, nil
}

func (s *SessionStore) Delete(key string) error {
	key = strings.TrimSpace(key)
	if key == "" {
		return fmt.Errorf("session key is required")
	}
	if _, exists, err := s.Get(key); err != nil || !exists {
		return err
	}

	unlock, err := s.lock()
	if err != nil {
		return err
	}
	defer unlock()
	store, err := s.load()
	if err != nil {
		return err
	}

	if _, ok := store.Sessions[key]; !ok {
		return nil
	}
	delete(store.Sessions, key)
	return s.save(store)
}

func (s *SessionStore) GetOrCreate(key string) (SessionRecord, bool, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return SessionRecord{}, false, fmt.Errorf("session key is required")
	}
	if record, exists, err := s.Get(key); err != nil || exists {
		return record, exists, err
	}

	unlock, err := s.lock()
	if err != nil {
		return SessionRecord{}, false, err
	}
	defer unlock()
	store, err := s.load()
	if err != nil {
		return SessionRecord{}, false, err
	}
	if record, exists := store.Sessions[key]; exists {
		return record, true, nil
	}

	sessionID, err := GenerateUUID()
	if err != nil {
		return SessionRecord{}, false, err
	}
	record, err := s.put(store, key, sessionID)
	return record, false, err
}

// lock serializes load-modify-save across runners and processes. Keep the
// sibling lock file in place so every writer always locks the same inode.
func (s *SessionStore) lock() (func(), error) {
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return nil, fmt.Errorf("create CLI session store directory: %w", err)
	}
	path := s.path + ".lock"
	file, err := os.OpenFile(path, os.O_RDWR|os.O_CREATE, 0o600)
	if err != nil {
		return nil, fmt.Errorf("lock CLI session store: %w", err)
	}

	deadline := time.Now().Add(sessionStoreLockWait)
	for {
		err := syscall.Flock(int(file.Fd()), syscall.LOCK_EX|syscall.LOCK_NB)
		if err == nil {
			return func() { _ = file.Close() }, nil
		}
		if !errors.Is(err, syscall.EWOULDBLOCK) && !errors.Is(err, syscall.EINTR) {
			_ = file.Close()
			return nil, fmt.Errorf("lock CLI session store: %w", err)
		}
		if time.Now().After(deadline) {
			_ = file.Close()
			return nil, fmt.Errorf("lock CLI session store: %s is held by another writer", path)
		}
		time.Sleep(sessionStoreLockPoll)
	}
}

func (s *SessionStore) load() (sessionStoreFile, error) {
	store := sessionStoreFile{
		Version:  sessionStoreVersion,
		Sessions: map[string]SessionRecord{},
	}

	bytes, err := os.ReadFile(s.path)
	if err != nil {
		if os.IsNotExist(err) {
			return store, nil
		}
		return sessionStoreFile{}, fmt.Errorf("read CLI session store: %w", err)
	}

	if err := json.Unmarshal(bytes, &store); err != nil {
		return sessionStoreFile{}, fmt.Errorf("decode CLI session store: %w", err)
	}
	if strings.TrimSpace(store.Version) != "" && store.Version != sessionStoreVersion {
		return sessionStoreFile{}, fmt.Errorf("unsupported CLI session store version %q", store.Version)
	}
	if store.Sessions == nil {
		store.Sessions = map[string]SessionRecord{}
	}
	store.Version = sessionStoreVersion
	return store, nil
}

func (s *SessionStore) save(store sessionStoreFile) error {
	contents, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("encode CLI session store: %w", err)
	}

	directory := filepath.Dir(s.path)
	temporary, err := os.CreateTemp(directory, ".cli-sessions-*")
	if err != nil {
		return fmt.Errorf("create CLI session store temporary file: %w", err)
	}
	tempPath := temporary.Name()
	defer os.Remove(tempPath)
	if _, err = temporary.Write(append(contents, '\n')); err == nil {
		err = temporary.Sync()
	}
	if closeErr := temporary.Close(); err == nil {
		err = closeErr
	}
	if err != nil {
		return fmt.Errorf("write CLI session store: %w", err)
	}

	// Get can read without locking because readers only see complete files.
	if err := os.Rename(tempPath, s.path); err != nil {
		return fmt.Errorf("replace CLI session store: %w", err)
	}
	dir, err := os.Open(directory)
	if err != nil {
		return fmt.Errorf("open CLI session store directory: %w", err)
	}
	defer dir.Close()
	if err := dir.Sync(); err != nil {
		return fmt.Errorf("sync CLI session store directory: %w", err)
	}
	return nil
}

func GenerateUUID() (string, error) {
	var bytes [16]byte
	if _, err := rand.Read(bytes[:]); err != nil {
		return "", fmt.Errorf("generate uuid: %w", err)
	}
	bytes[6] = (bytes[6] & 0x0f) | 0x40
	bytes[8] = (bytes[8] & 0x3f) | 0x80

	encoded := hex.EncodeToString(bytes[:])
	return fmt.Sprintf(
		"%s-%s-%s-%s-%s",
		encoded[0:8],
		encoded[8:12],
		encoded[12:16],
		encoded[16:20],
		encoded[20:32],
	), nil
}
