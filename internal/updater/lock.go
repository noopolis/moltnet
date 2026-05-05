package updater

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

const defaultUpdateLockStaleAfter = 30 * time.Minute

type updateLockOptions struct {
	BinaryPath    string
	Path          string
	StaleAfter    time.Duration
	TargetVersion string
}

type updateLock struct {
	file  *os.File
	path  string
	token string
}

type updateLockRecord struct {
	BinaryPath    string    `json:"binary_path,omitempty"`
	CreatedAt     time.Time `json:"created_at"`
	PID           int       `json:"pid"`
	TargetVersion string    `json:"target_version,omitempty"`
	Token         string    `json:"token"`
}

func acquireUpdateLock(options updateLockOptions) (*updateLock, error) {
	path := strings.TrimSpace(options.Path)
	if path == "" {
		return nil, fmt.Errorf("update lock path is empty")
	}
	staleAfter := options.StaleAfter
	if staleAfter <= 0 {
		staleAfter = defaultUpdateLockStaleAfter
	}

	for {
		file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
		if err == nil {
			record := updateLockRecord{
				BinaryPath:    strings.TrimSpace(options.BinaryPath),
				CreatedAt:     time.Now().UTC(),
				PID:           os.Getpid(),
				TargetVersion: strings.TrimSpace(options.TargetVersion),
				Token:         randomUpdateLockToken(),
			}
			contents, err := json.Marshal(record)
			if err != nil {
				_ = file.Close()
				_ = os.Remove(path)
				return nil, err
			}
			contents = append(contents, '\n')
			if err := file.Chmod(0o600); err != nil {
				_ = file.Close()
				_ = os.Remove(path)
				return nil, err
			}
			if _, err := file.Write(contents); err != nil {
				_ = file.Close()
				_ = os.Remove(path)
				return nil, err
			}
			return &updateLock{file: file, path: path, token: record.Token}, nil
		}
		if !errors.Is(err, os.ErrExist) {
			return nil, fmt.Errorf("create update lock %s: %w", path, err)
		}
		stale, err := existingLockIsStale(path, staleAfter)
		if err != nil {
			return nil, err
		}
		if !stale {
			return nil, fmt.Errorf("another moltnet update is already running (lock: %s); remove the lock only if no update process is active", path)
		}
		if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
			return nil, fmt.Errorf("remove stale update lock %s: %w", path, err)
		}
	}
}

func (lock *updateLock) Release() error {
	if lock == nil {
		return nil
	}
	var closeErr error
	if lock.file != nil {
		closeErr = lock.file.Close()
		lock.file = nil
	}
	if !updateLockTokenMatches(lock.path, lock.token) {
		return closeErr
	}
	removeErr := os.Remove(lock.path)
	if errors.Is(removeErr, os.ErrNotExist) {
		removeErr = nil
	}
	if closeErr != nil {
		return closeErr
	}
	return removeErr
}

func existingLockIsStale(path string, staleAfter time.Duration) (bool, error) {
	info, err := os.Lstat(path)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			return true, nil
		}
		return false, fmt.Errorf("inspect update lock %s: %w", path, err)
	}
	if info.Mode()&os.ModeSymlink != 0 {
		return false, fmt.Errorf("refusing update lock symlink %s", path)
	}
	if !info.Mode().IsRegular() {
		return false, fmt.Errorf("refusing non-regular update lock %s", path)
	}
	if record, ok := readUpdateLockRecord(path); ok && record.PID > 0 {
		if processAlive(record.PID) {
			return false, nil
		}
		return true, nil
	}
	return time.Since(info.ModTime()) > staleAfter, nil
}

func defaultUpdateLockPath(installPath string) string {
	if strings.TrimSpace(installPath) == "" {
		return ""
	}
	return filepath.Clean(installPath) + ".update.lock"
}

func readUpdateLockRecord(path string) (updateLockRecord, bool) {
	contents, err := os.ReadFile(path)
	if err != nil {
		return updateLockRecord{}, false
	}
	var record updateLockRecord
	if err := json.Unmarshal(contents, &record); err != nil {
		return updateLockRecord{}, false
	}
	return record, true
}

func updateLockTokenMatches(path string, token string) bool {
	if strings.TrimSpace(token) == "" {
		return false
	}
	record, ok := readUpdateLockRecord(path)
	return ok && record.Token == token
}

func randomUpdateLockToken() string {
	var buffer [16]byte
	if _, err := rand.Read(buffer[:]); err == nil {
		return hex.EncodeToString(buffer[:])
	}
	return fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
}

func processAlive(pid int) bool {
	if pid <= 0 {
		return false
	}
	err := syscall.Kill(pid, 0)
	if err == nil {
		return true
	}
	return errors.Is(err, syscall.EPERM)
}
