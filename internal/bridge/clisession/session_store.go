package clisession

import (
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const sessionStoreVersion = "moltnet.cli-sessions.v1"

type SessionStore struct {
	path string
	mu   sync.Mutex
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
	s.mu.Lock()
	defer s.mu.Unlock()

	store, err := s.load()
	if err != nil {
		return SessionRecord{}, false, err
	}

	record, ok := store.Sessions[strings.TrimSpace(key)]
	return record, ok, nil
}

func (s *SessionStore) Put(key string, runtimeSessionID string) (SessionRecord, error) {
	s.mu.Lock()
	defer s.mu.Unlock()

	store, err := s.load()
	if err != nil {
		return SessionRecord{}, err
	}

	trimmedKey := strings.TrimSpace(key)
	trimmedSessionID := strings.TrimSpace(runtimeSessionID)
	if trimmedKey == "" {
		return SessionRecord{}, fmt.Errorf("session key is required")
	}
	if trimmedSessionID == "" {
		return SessionRecord{}, fmt.Errorf("runtime session id is required")
	}

	now := time.Now().UTC()
	record := store.Sessions[trimmedKey]
	if record.CreatedAt.IsZero() {
		record.CreatedAt = now
	}
	record.UpdatedAt = now
	record.RuntimeSessionID = trimmedSessionID
	store.Sessions[trimmedKey] = record

	if err := s.save(store); err != nil {
		return SessionRecord{}, err
	}
	return record, nil
}

func (s *SessionStore) GetOrCreate(key string) (SessionRecord, bool, error) {
	record, ok, err := s.Get(key)
	if err != nil || ok {
		return record, ok, err
	}

	sessionID, err := GenerateUUID()
	if err != nil {
		return SessionRecord{}, false, err
	}
	record, err = s.Put(key, sessionID)
	return record, false, err
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
	if err := os.MkdirAll(filepath.Dir(s.path), 0o700); err != nil {
		return fmt.Errorf("create CLI session store directory: %w", err)
	}

	bytes, err := json.MarshalIndent(store, "", "  ")
	if err != nil {
		return fmt.Errorf("encode CLI session store: %w", err)
	}
	bytes = append(bytes, '\n')

	tmpPath := s.path + ".tmp"
	if err := os.WriteFile(tmpPath, bytes, 0o600); err != nil {
		return fmt.Errorf("write CLI session store: %w", err)
	}
	if err := os.Rename(tmpPath, s.path); err != nil {
		return fmt.Errorf("replace CLI session store: %w", err)
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
