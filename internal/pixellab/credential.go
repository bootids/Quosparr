package pixellab

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

// EnvKey returns the PIXELLAB_API_KEY environment value. Lookup stays with
// the caller.
func EnvKey() string {
	return os.Getenv("PIXELLAB_API_KEY")
}

// TokenFilePath returns the location of the PixelLab CLI token file:
func TokenFilePath(home string) string {
	return filepath.Join(home, ".config", "pixellab", "token")
}

// LoadCredential resolves the PixelLab API key. A non-empty apiKey wins.
// Otherwise the plain-text token file at path is read: the content trimmed
// of leading and trailing whitespace makes the credential when non-empty.
// A missing file or empty content means "not found" (ok false, nil error);
// an unreadable file is an error.
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
		return "", false, fmt.Errorf("pixellab: read token file: %w", err)
	}
	token := strings.TrimSpace(string(data))
	if token == "" {
		return "", false, nil
	}
	return token, true, nil
}
