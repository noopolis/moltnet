package loop

import (
	"fmt"
	"strings"

	"github.com/noopolis/moltnet/pkg/bridgeconfig"
)

func (c *MoltnetClient) applyReadyToken(config bridgeconfig.Config, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return nil
	}
	if strings.TrimSpace(config.Moltnet.TokenPath) == "" {
		return fmt.Errorf("attachment returned an agent token but moltnet.token_path is not configured")
	}
	if err := bridgeconfig.WriteTokenFile(config.Moltnet.TokenPath, token); err != nil {
		return err
	}
	c.setToken(token)
	return nil
}

func (c *MoltnetClient) prepareOpenClaim(config bridgeconfig.Config) error {
	if config.Moltnet.EffectiveAuthMode() != bridgeconfig.AuthModeOpen {
		return nil
	}
	if err := c.resolveTokenErr(); err != nil {
		return err
	}
	if c.currentToken() != "" {
		return nil
	}
	if strings.TrimSpace(config.Moltnet.TokenPath) == "" {
		return fmt.Errorf("open auth first claim requires moltnet.token_path")
	}
	return bridgeconfig.PrepareTokenFileWrite(config.Moltnet.TokenPath)
}

func (c *MoltnetClient) currentToken() string {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.token
}

func (c *MoltnetClient) setToken(token string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.token = strings.TrimSpace(token)
	c.tokenErr = nil
}

func (c *MoltnetClient) resolveTokenErr() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.tokenErr
}
