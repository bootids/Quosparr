package meter

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"slices"

	"github.com/licoy/quosparr/internal/platform"
)

// DisabledPath is the non-secret list of quota providers the user turned
// off: <root>/meter-disabled.json, {"disabled": ["minimax", ...]}.
func DisabledPath(root string) string {
	return filepath.Join(root, "meter-disabled.json")
}

// LoadDisabled reads the disabled provider IDs. A missing file means none.
func LoadDisabled(root string) ([]string, error) {
	data, err := os.ReadFile(DisabledPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return nil, nil
		}
		return nil, fmt.Errorf("meter: read disabled providers: %w", err)
	}
	var doc struct {
		Disabled []string `json:"disabled"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return nil, fmt.Errorf("meter: decode disabled providers: %w", err)
	}
	return doc.Disabled, nil
}

// IsDisabled reports whether provider is turned off.
func IsDisabled(root, provider string) (bool, error) {
	ids, err := LoadDisabled(root)
	return slices.Contains(ids, provider), err
}

// SetDisabled turns provider off or back on, rewriting the list atomically.
func SetDisabled(root, provider string, disabled bool) error {
	ids, err := LoadDisabled(root)
	if err != nil {
		return err
	}
	ids = slices.DeleteFunc(ids, func(id string) bool { return id == provider })
	if disabled {
		ids = append(ids, provider)
	}
	slices.Sort(ids)
	body, err := json.Marshal(struct {
		Disabled []string `json:"disabled"`
	}{Disabled: ids})
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(DisabledPath(root), body, 0o600)
}
