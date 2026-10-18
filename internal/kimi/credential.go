package kimi

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// Credential is a Kimi Code access token: either an explicit API key (no
// expiry) or the access_token read from the CLI credentials file.
type Credential struct {
	Secret    string
	ExpiresAt time.Time
	HasExpiry bool
}

// credentialsFile is the on-disk shape of the kimi-code credentials file.
// Only access_token and expires_at are read; refresh_token, expires_in,
// scope and token_type are ignored and the file is never written.
type credentialsFile struct {
	AccessToken string   `json:"access_token"`
	ExpiresAt   *float64 `json:"expires_at"`
}

// CredentialsPath returns the location of the kimi-code credentials file:
// shareDir/credentials/kimi-code.json when shareDir is set (KIMI_SHARE_DIR),
// otherwise home/.kimi/credentials/kimi-code.json. Env lookup stays with the
// caller.
func CredentialsPath(shareDir, home string) string {
	if shareDir != "" {
		return filepath.Join(shareDir, "credentials", "kimi-code.json")
	}
	return filepath.Join(home, ".kimi", "credentials", "kimi-code.json")
}

// LoadCredential resolves the Kimi Code credential. A non-empty apiKey wins
// and carries no expiry. Otherwise the JSON file at credentialsPath is read:
// a non-empty root access_token makes the credential usable and a numeric
// expires_at (epoch seconds) sets the expiry. A missing file or an empty
// token means "not found" (ok false, nil error); an unreadable or
// undecodable file is an error.
func LoadCredential(apiKey, credentialsPath string) (Credential, bool, error) {
	if apiKey != "" {
		return Credential{Secret: apiKey}, true, nil
	}
	if credentialsPath == "" {
		return Credential{}, false, nil
	}
	data, err := os.ReadFile(credentialsPath)
	if err != nil {
		if os.IsNotExist(err) {
			return Credential{}, false, nil
		}
		return Credential{}, false, fmt.Errorf("kimi: read credentials: %w", err)
	}
	var file credentialsFile
	if err := json.Unmarshal(data, &file); err != nil {
		return Credential{}, false, fmt.Errorf("kimi: decode credentials: %w", err)
	}
	if file.AccessToken == "" {
		return Credential{}, false, nil
	}
	cred := Credential{Secret: file.AccessToken}
	if file.ExpiresAt != nil {
		cred.ExpiresAt = time.Unix(int64(*file.ExpiresAt), 0).UTC()
		cred.HasExpiry = true
	}
	return cred, true, nil
}
