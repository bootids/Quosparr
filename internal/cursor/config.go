package cursor

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

// ConfigPath is the managed auth.json of the explicitly configured access
// token under root (~/.quosparr): <root>/accounts/cursor/auth.json.
func ConfigPath(root string) string {
	return filepath.Join(root, "accounts", "cursor", "auth.json")
}

// LoadConfig reads the explicitly configured access token. A missing file or
// an empty (whitespace-only) token means not configured: ("", false, nil);
// the token is returned trimmed. An unreadable or undecodable file is an
// error. Decoding the token is left to CredentialFromToken.
func LoadConfig(root string) (token string, ok bool, err error) {
	data, err := os.ReadFile(ConfigPath(root))
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("cursor: read config: %w", err)
	}
	var doc struct {
		AccessToken string `json:"accessToken"`
	}
	if err := json.Unmarshal(data, &doc); err != nil {
		return "", false, fmt.Errorf("cursor: decode config: %w", err)
	}
	token = strings.TrimSpace(doc.AccessToken)
	if token == "" {
		return "", false, nil
	}
	return token, true, nil
}

// SaveConfig stores token as the explicit configuration, written atomically
// with owner-only permissions. The token is trimmed and must not be empty.
func SaveConfig(root, token string) error {
	token = strings.TrimSpace(token)
	if token == "" {
		return errors.New("cursor: access token is required")
	}
	if err := platform.EnsurePrivateDirectory(filepath.Dir(ConfigPath(root))); err != nil {
		return err
	}
	body, err := json.Marshal(struct {
		AccessToken string `json:"accessToken"`
	}{AccessToken: token})
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(ConfigPath(root), body, storeFilePerm)
}

// RemoveConfig deletes the explicit configuration directory, leaving the
// provider to auto-discovery. A missing directory is not an error.
func RemoveConfig(root string) error {
	return os.RemoveAll(filepath.Join(root, "accounts", "cursor"))
}

// CredentialFromToken decodes an access token supplied by the caller (for
// example one loaded from the explicit configuration). A token that is not
// a usable JWT returns the token-bearing credential, false and
// ErrInvalidToken, mirroring LoadCredential.
func CredentialFromToken(token string) (Credential, bool, error) {
	return decodeJWT(token)
}
