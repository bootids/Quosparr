package minimax

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
// under root (~/.quosparr): <root>/accounts/minimax/auth.json.
func ConfigPath(root string) string {
	return filepath.Join(root, "accounts", "minimax", "auth.json")
}

// LoadConfig reads the explicitly configured API key and region. A missing
// file or an empty (whitespace-only) key means not configured:
// ("", "", false, nil); the key is returned trimmed. The region is only
// reported when it is exactly "cn" or "intl"; any other stored value is
// treated as unset. An unreadable or undecodable file is an error.
func LoadConfig(root string) (apiKey, region string, ok bool, err error) {
	data, err := os.ReadFile(ConfigPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return "", "", false, nil
		}
		return "", "", false, fmt.Errorf("minimax: read config: %w", err)
	}
	var doc struct {
		APIKey string `json:"apiKey"`
		Region string `json:"region"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", "", false, fmt.Errorf("minimax: decode config: %w", err)
	}
	key := strings.TrimSpace(doc.APIKey)
	if key == "" {
		return "", "", false, nil
	}
	return key, normalizeRegion(doc.Region), true, nil
}

// SaveConfig stores apiKey and region as the explicit configuration, written
// atomically with owner-only permissions. The key is trimmed and must not be
// empty; a region that is not "cn" or "intl" is stored as unset ("").
func SaveConfig(root, apiKey, region string) error {
	key := strings.TrimSpace(apiKey)
	if key == "" {
		return errors.New("minimax: API key is required")
	}
	if err := platform.EnsurePrivateDirectory(filepath.Dir(ConfigPath(root))); err != nil {
		return err
	}
	body, err := json.Marshal(struct {
		APIKey string `json:"apiKey"`
		Region string `json:"region"`
	}{APIKey: key, Region: normalizeRegion(region)})
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(ConfigPath(root), body, storeFilePerm)
}

// RemoveConfig deletes the explicit configuration directory, leaving the
// provider to auto-discovery. A missing directory is not an error.
func RemoveConfig(root string) error {
	return os.RemoveAll(filepath.Join(root, "accounts", "minimax"))
}

// normalizeRegion keeps only the two known regions; anything else is unset.
func normalizeRegion(region string) string {
	switch strings.TrimSpace(region) {
	case RegionCN:
		return RegionCN
	case RegionIntl:
		return RegionIntl
	}
	return ""
}
