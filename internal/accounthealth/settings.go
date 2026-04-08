package accounthealth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
)

type sidecarSettings struct {
	WebToken string `json:"web_token,omitempty"`
}

func (a *App) settingsPath() string {
	if a == nil || strings.TrimSpace(a.authDir) == "" {
		return ""
	}
	return filepath.Join(a.authDir, ".account-health-settings.json")
}

func (a *App) loadSettings() error {
	path := a.settingsPath()
	if path == "" {
		return nil
	}
	raw, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return nil
		}
		return err
	}
	var settings sidecarSettings
	if err := json.Unmarshal(raw, &settings); err != nil {
		return err
	}
	if strings.TrimSpace(settings.WebToken) != "" {
		a.mu.Lock()
		a.webToken = strings.TrimSpace(settings.WebToken)
		a.mu.Unlock()
	}
	return nil
}

func (a *App) updateWebToken(token string) error {
	path := a.settingsPath()
	if path == "" {
		return nil
	}
	settings := sidecarSettings{WebToken: strings.TrimSpace(token)}
	payload, err := json.MarshalIndent(settings, "", "  ")
	if err != nil {
		return err
	}
	if err := os.WriteFile(path, append(payload, '\n'), 0o600); err != nil {
		return err
	}
	a.mu.Lock()
	a.webToken = settings.WebToken
	a.mu.Unlock()
	return nil
}

func (a *App) hasWebToken() bool {
	a.mu.RLock()
	defer a.mu.RUnlock()
	return strings.TrimSpace(a.webToken) != ""
}
