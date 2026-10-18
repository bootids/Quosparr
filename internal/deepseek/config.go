package deepseek

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/licoy/quosparr/internal/platform"
)

// storeFilePerm mirrors the zai account store: owner-only credential files.
const storeFilePerm = 0o600

// ConfigPath is the managed auth.json of the explicitly configured API key
// under root (~/.quosparr): <root>/accounts/deepseek/auth.json.
func ConfigPath(root string) string {
	return filepath.Join(root, "accounts", "deepseek", "auth.json")
}

// LoadConfig reads the explicitly configured API key. A missing file or an
// empty (whitespace-only) key means not configured: ("", false, nil); the
// key is returned trimmed. An unreadable or undecodable file is an error.
func LoadConfig(root string) (apiKey string, ok bool, err error) {
	data, err := os.ReadFile(ConfigPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("deepseek: read config: %w", err)
	}
	var doc struct {
		APIKey string `json:"apiKey"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", false, fmt.Errorf("deepseek: decode config: %w", err)
	}
	key := strings.TrimSpace(doc.APIKey)
	if key == "" {
		return "", false, nil
	}
	return key, true, nil
}

// SaveConfig stores apiKey as the explicit configuration, written atomically
// with owner-only permissions. The key is trimmed and must not be empty.
func SaveConfig(root, apiKey string) error {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return errors.New("deepseek: API key is required")
	}
	if err := platform.EnsurePrivateDirectory(filepath.Dir(ConfigPath(root))); err != nil {
		return err
	}
	body, err := json.Marshal(struct {
		APIKey string `json:"apiKey"`
	}{APIKey: key})
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(ConfigPath(root), body, storeFilePerm)
}

// RemoveConfig deletes the explicit configuration directory, leaving the
// provider to the environment. A missing directory is not an error.
func RemoveConfig(root string) error {
	return os.RemoveAll(filepath.Join(root, "accounts", "deepseek"))
}
