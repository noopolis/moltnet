package webhook

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
)

type receipts struct {
	Day      string          `json:"day"`
	Attempts int             `json:"attempts"`
	Accepted map[string]bool `json:"accepted"`
	Order    []string        `json:"order"`
}

func (c *codec) load() error {
	c.state.Accepted = make(map[string]bool)
	info, err := os.Lstat(c.path)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return fmt.Errorf("stat webhook receipts: %w", err)
	}
	if !info.Mode().IsRegular() || info.Mode().Perm()&0o077 != 0 {
		return fmt.Errorf("webhook receipts must be a private regular file (0600)")
	}
	b, err := os.ReadFile(c.path)
	if err != nil {
		return err
	}
	if err = json.Unmarshal(b, &c.state); err != nil {
		return fmt.Errorf("read webhook receipts: %w", err)
	}
	if c.state.Accepted == nil {
		c.state.Accepted = make(map[string]bool)
	}
	if c.state.Attempts < 0 {
		return fmt.Errorf("invalid webhook budget")
	}
	return nil
}

func (c *codec) save() error {
	if err := os.MkdirAll(filepath.Dir(c.path), 0o700); err != nil {
		return err
	}
	b, err := json.Marshal(c.state)
	if err != nil {
		return err
	}
	tmp, err := os.CreateTemp(filepath.Dir(c.path), ".webhook-receipts-*")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err = tmp.Write(b); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Sync(); err != nil {
		tmp.Close()
		return err
	}
	if err = tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), c.path)
}
