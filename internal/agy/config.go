package agy

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/licoy/quosparr/internal/platform"
)

// storeFilePerm mirrors the zai account store: owner-only config files.
const storeFilePerm = 0o600

// Config is the explicit Antigravity CLI configuration: non-secret path
// overrides for the binary and the profile directory. Both fields are
// optional; an all-empty configuration means "not configured".
type Config struct {
	Bin        string `json:"bin"`
	ProfileDir string `json:"profileDir"`
}

// ConfigPath is the managed config.json of the explicit configuration under
// root (~/.quosparr): <root>/accounts/agy/config.json.
func ConfigPath(root string) string {
	return filepath.Join(root, "accounts", "agy", "config.json")
}

// LoadConfig reads the explicit configuration. A missing file or a file
// whose fields are both empty (whitespace-only) means not configured:
// (zero, false, nil); fields are returned trimmed. An unreadable or
// undecodable file is an error.
func LoadConfig(root string) (Config, bool, error) {
	data, err := os.ReadFile(ConfigPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return Config{}, false, nil
		}
		return Config{}, false, fmt.Errorf("agy: read config: %w", err)
	}
	var cfg Config
	if err := json.Unmarshal(data, &cfg); err != nil {
		return Config{}, false, fmt.Errorf("agy: decode config: %w", err)
	}
	cfg.Bin = strings.TrimSpace(cfg.Bin)
	cfg.ProfileDir = strings.TrimSpace(cfg.ProfileDir)
	if cfg.Bin == "" && cfg.ProfileDir == "" {
		return Config{}, false, nil
	}
	return cfg, true, nil
}

// SaveConfig stores cfg as the explicit configuration, written atomically
// with owner-only permissions. Fields are trimmed; empty fields are allowed
// and express an explicit reset to auto-discovery for that side.
func SaveConfig(root string, cfg Config) error {
	cfg.Bin = strings.TrimSpace(cfg.Bin)
	cfg.ProfileDir = strings.TrimSpace(cfg.ProfileDir)
	if err := platform.EnsurePrivateDirectory(filepath.Dir(ConfigPath(root))); err != nil {
		return err
	}
	body, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(ConfigPath(root), body, storeFilePerm)
}

// RemoveConfig deletes the explicit configuration directory, leaving the
// provider to auto-discovery. A missing directory is not an error.
func RemoveConfig(root string) error {
	return os.RemoveAll(filepath.Join(root, "accounts", "agy"))
}
