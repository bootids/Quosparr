package elevenlabs

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvKey returns the ELEVENLABS_API_KEY environment value; env lookup stays
// with the caller.
func EnvKey() string {
	return os.Getenv("ELEVENLABS_API_KEY")
}

// APIKeyFilePath returns the location of the elevenlabs CLI api-key file:
// home/.config/elevenlabs/api-key. The path is fixed (no XDG override) to
// match the reference implementation.
func APIKeyFilePath(home string) string {
	return filepath.Join(home, ".config", "elevenlabs", "api-key")
}

// LoadCredential resolves the ElevenLabs API key. A non-empty apiKey wins.
// Otherwise the plain-text file at path is read and its content, trimmed of
// surrounding whitespace, is the key when non-empty. A missing file means
// "not found" (ok false, nil error); an unreadable file is an error.
func LoadCredential(apiKey, path string) (string, bool, error) {
	if apiKey != "" {
		return apiKey, true, nil
	}
	if path == "" {
		return "", false, nil
	}
	data, err := os.ReadFile(path)
	if err != nil {
		if os.IsNotExist(err) {
			return "", false, nil
		}
		return "", false, fmt.Errorf("elevenlabs: read api-key file: %w", err)
	}
	key := strings.TrimSpace(string(data))
	if key == "" {
		return "", false, nil
	}
	return key, true, nil
}
