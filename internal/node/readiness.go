package node

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"sort"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

const readinessReceiptEnv = "MOLTNET_NODE_READINESS_RECEIPT"

type readinessAttachment struct {
	NetworkID string `json:"network_id"`
	AgentID   string `json:"agent_id"`
}
type readinessReceipt struct {
	Version     string                `json:"version"`
	Attachments []readinessAttachment `json:"attachments"`
}

func writeReadinessReceipt(configs []bridgeconfig.Config) error {
	path := os.Getenv(readinessReceiptEnv)
	if path == "" {
		return nil
	}
	attachments := make([]readinessAttachment, 0, len(configs))
	for _, config := range configs {
		attachments = append(attachments, readinessAttachment{NetworkID: config.Moltnet.NetworkID, AgentID: config.Agent.ID})
	}
	sort.Slice(attachments, func(i, j int) bool {
		if attachments[i].NetworkID == attachments[j].NetworkID {
			return attachments[i].AgentID < attachments[j].AgentID
		}
		return attachments[i].NetworkID < attachments[j].NetworkID
	})
	encoded, err := json.Marshal(readinessReceipt{Version: "moltnet.node-readiness.v1", Attachments: attachments})
	if err != nil {
		return err
	}
	encoded = append(encoded, '\n')
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return fmt.Errorf("create readiness directory: %w", err)
	}
	temporary := path + fmt.Sprintf(".tmp-%d", os.Getpid())
	file, err := os.OpenFile(temporary, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o600)
	if err != nil {
		return fmt.Errorf("create readiness receipt: %w", err)
	}
	ok := false
	defer func() {
		_ = file.Close()
		if !ok {
			_ = os.Remove(temporary)
		}
	}()
	if _, err = file.Write(encoded); err == nil {
		err = file.Sync()
	}
	if err == nil {
		err = file.Close()
	}
	if err == nil {
		err = os.Rename(temporary, path)
	}
	if err != nil {
		return fmt.Errorf("publish readiness receipt: %w", err)
	}
	directory, err := os.Open(filepath.Dir(path))
	if err != nil {
		return err
	}
	defer directory.Close()
	if err = directory.Sync(); err != nil {
		return err
	}
	ok = true
	return nil
}
