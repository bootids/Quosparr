package minimax

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// Env key precedence for the MiniMax coding plan API key.
const (
	envCodingKey = "MINIMAX_CODING_API_KEY"
	envPlainKey  = "MINIMAX_API_KEY"
)

// EnvKey returns the MiniMax API key from MINIMAX_CODING_API_KEY, falling
// back to MINIMAX_API_KEY. It is empty when neither variable holds a key.
func EnvKey() string {
	if k := strings.TrimSpace(os.Getenv(envCodingKey)); k != "" {
		return k
	}
	return strings.TrimSpace(os.Getenv(envPlainKey))
}

// ConfigFilePath returns the location of the read-only MiniMax CLI config:
// home/.config/minimax/config.json. The path is fixed to $HOME/.config,
// mirroring the reference implementation (XDG_CONFIG_HOME is not honored).
func ConfigFilePath(home string) string {
	return filepath.Join(home, ".config", "minimax", "config.json")
}

// discoveredConfig is the on-disk shape of ~/.config/minimax/config.json.
// Only api_key and base_url are read; the file is never written.
type discoveredConfig struct {
	APIKey  string `json:"api_key"`
	BaseURL string `json:"base_url"`
}

// LoadCredential resolves the MiniMax API key and region. A non-empty apiKey
// wins and keeps the region passed by the caller (which may be ""). Otherwise
// the JSON file at configPath is read: a non-empty root api_key makes the
// credential usable, and base_url containing minimaxi.com hints region "cn"
// while minimax.io hints "intl". A missing file or an empty api_key means
// "not found" (ok false, nil error); an unreadable or undecodable file is an
// error. The returned region is the explicit region when set, else the file
// hint, else "" (the caller defaults to "cn").
func LoadCredential(apiKey, region, configPath string) (key, resolvedRegion string, ok bool, err error) {
	if apiKey != "" {
		return apiKey, region, true, nil
	}
	if configPath == "" {
		return "", "", false, nil
	}
	data, err := os.ReadFile(configPath)
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("minimax: read config file: %w", err)
	}
	var file discoveredConfig
	if err := json.Unmarshal(data, &file); err != nil {
		return "", "", false, fmt.Errorf("minimax: decode config file: %w", err)
	}
	key = strings.TrimSpace(file.APIKey)
	if key == "" {
		return "", "", false, nil
	}
	if region != "" {
		return key, region, true, nil
	}
	return key, hintRegion(file.BaseURL), true, nil
}

// hintRegion derives a region hint from the discovered base_url.
func hintRegion(baseURL string) string {
	switch {
	case strings.Contains(baseURL, "minimaxi.com"):
		return RegionCN
	case strings.Contains(baseURL, "minimax.io"):
		return RegionIntl
	}
	return ""
}
