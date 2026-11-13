// Package automation owns local owner-configured wake policies and bounded discussions.
package automation

import (
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"time"

	"github.com/noopolis/moltnet/pkg/protocol"
)

type roomState struct {
	Settings   map[string]string    `json:"settings"`
	Discussion *protocol.Discussion `json:"discussion,omitempty"`
}

type state struct {
	Rooms map[string]*roomState `json:"rooms"`
	Hooks map[string]*hook      `json:"hooks"`
}

type Manager struct {
	mu        sync.Mutex
	path      string
	networkID string
	state     state
}

func New(path, networkID string) (*Manager, error) {
	m := &Manager{path: path, networkID: networkID, state: state{Rooms: map[string]*roomState{}, Hooks: map[string]*hook{}}}
	if path == "" {
		return m, nil
	}
	info, err := os.Lstat(path)
	if errors.Is(err, os.ErrNotExist) {
		return m, nil
	}
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || (runtime.GOOS != "windows" && info.Mode().Perm()&0077 != 0) {
		return nil, errors.New("automation store must be a private regular file (0600)")
	}
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, err
	}
	if err := json.Unmarshal(data, &m.state); err != nil {
		return nil, err
	}
	if m.state.Rooms == nil {
		m.state.Rooms = map[string]*roomState{}
	}
	if m.state.Hooks == nil {
		m.state.Hooks = map[string]*hook{}
	}
	// Never silently resume billable discussions after a process restart.
	for _, room := range m.state.Rooms {
		if room.Discussion != nil && room.Discussion.Active {
			room.Discussion.Active = false
			room.Discussion.Reason = "服务已重启，请重新开始讨论"
		}
	}
	return m, m.save()
}

func (m *Manager) save() error {
	if m.path == "" {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(m.path), 0700); err != nil {
		return err
	}
	data, err := json.MarshalIndent(m.state, "", "  ")
	if err != nil {
		return err
	}
	f, err := os.CreateTemp(filepath.Dir(m.path), ".automation-*")
	if err != nil {
		return err
	}
	defer os.Remove(f.Name())
	if err := f.Chmod(0600); err != nil {
		f.Close()
		return err
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return err
	}
	if err := f.Sync(); err != nil {
		f.Close()
		return err
	}
	if err := f.Close(); err != nil {
		return err
	}
	return os.Rename(f.Name(), m.path)
}

func (m *Manager) room(id string) *roomState {
	if m.state.Rooms[id] == nil {
		m.state.Rooms[id] = &roomState{Settings: map[string]string{}}
	}
	if m.state.Rooms[id].Settings == nil {
		m.state.Rooms[id].Settings = map[string]string{}
	}
	return m.state.Rooms[id]
}

func (m *Manager) View(roomID string, members []string) protocol.RoomAutomation {
	m.mu.Lock()
	defer m.mu.Unlock()
	room := m.room(roomID)
	if expired(room.Discussion) {
		room.Discussion.Active = false
		room.Discussion.Reason = "超过 10 分钟未收到回复，讨论已停止"
		_ = m.save()
	}
	view := protocol.RoomAutomation{Settings: []protocol.AgentWakeSetting{}, Connectors: []protocol.ConnectorStatus{}}
	for _, id := range members {
		mode := room.Settings[id]
		if mode == "" {
			mode = "owner"
		}
		view.Settings = append(view.Settings, protocol.AgentWakeSetting{AgentID: id, Mode: mode})
		view.Connectors = append(view.Connectors, m.connectorStatus(id))
	}
	if room.Discussion != nil {
		copy := *room.Discussion
		copy.Participants = append([]string(nil), copy.Participants...)
		view.Discussion = &copy
	}
	return view
}

func (m *Manager) SetMode(roomID, agentID, mode string) error {
	if mode != "owner" && mode != "mentions" && mode != "never" {
		return errors.New("唤醒方式必须是 owner、mentions 或 never")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	room := m.room(roomID)
	old, existed := room.Settings[agentID]
	room.Settings[agentID] = mode
	if err := m.save(); err != nil {
		if existed {
			room.Settings[agentID] = old
		} else {
			delete(room.Settings, agentID)
		}
		return err
	}
	return nil
}

func (m *Manager) Stop(roomID, reason string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	d := m.room(roomID).Discussion
	if d == nil {
		return nil
	}
	old := *d
	d.Active, d.Reason, d.UpdatedAt = false, reason, time.Now().UTC()
	if err := m.save(); err != nil {
		*d = old
		return err
	}
	return nil
}

func contains(ids []string, id string) bool {
	for _, candidate := range ids {
		if candidate == id {
			return true
		}
	}
	return false
}
