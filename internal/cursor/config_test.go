package cursor

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

const testToken = "jwt-token-5678"

func TestConfigPath(t *testing.T) {
	if got := ConfigPath("/root"); got != filepath.Join("/root", "accounts", "cursor", "auth.json") {
		t.Fatalf("ConfigPath = %q", got)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	root := t.TempDir()
	if _, ok, err := LoadConfig(root); ok || err != nil {
		t.Fatalf("empty store = (%v, %v), want (false, nil)", ok, err)
	}
	if err := SaveConfig(root, "  "+testToken+"  "); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	token, ok, err := LoadConfig(root)
	if err != nil || !ok {
		t.Fatalf("LoadConfig = (%q, %v, %v)", token, ok, err)
	}
	if token != testToken {
		t.Fatalf("token = %q, want trimmed %q", token, testToken)
	}
}

func writeConfig(t *testing.T, root, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(ConfigPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath(root), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	token, ok, err := LoadConfig(t.TempDir())
	if ok || err != nil || token != "" {
		t.Fatalf("LoadConfig = (%q, %v, %v), want not configured", token, ok, err)
	}
}

func TestLoadConfigUndecodable(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{not json`)
	_, ok, err := LoadConfig(root)
	if ok || err == nil {
		t.Fatalf("ok=%v err=%v, want decode error", ok, err)
	}
	if !strings.HasPrefix(err.Error(), "cursor: ") {
		t.Fatalf("err = %v, want cursor prefix", err)
	}
}

func TestLoadConfigEmptyToken(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{"accessToken":"   "}`)
	token, ok, err := LoadConfig(root)
	if ok || err != nil || token != "" {
		t.Fatalf("LoadConfig = (%q, %v, %v), want not configured", token, ok, err)
	}
}

func TestSaveConfigEmptyToken(t *testing.T) {
	root := t.TempDir()
	err := SaveConfig(root, "   ")
	if err == nil || !strings.HasPrefix(err.Error(), "cursor: ") {
		t.Fatalf("err = %v, want cursor error", err)
	}
	if _, statErr := os.Stat(ConfigPath(root)); !os.IsNotExist(statErr) {
		t.Fatalf("config written for empty token: %v", statErr)
	}
}

func TestRemoveConfig(t *testing.T) {
	root := t.TempDir()
	if err := SaveConfig(root, testToken); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	dir := filepath.Join(root, "accounts", "cursor")
	if err := RemoveConfig(root); err != nil {
		t.Fatalf("RemoveConfig: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("dir %q still exists", dir)
	}
	if _, ok, err := LoadConfig(root); ok || err != nil {
		t.Fatalf("after remove = (%v, %v), want not configured", ok, err)
	}
	// Removing again is a no-op, not an error.
	if err := RemoveConfig(root); err != nil {
		t.Fatalf("second RemoveConfig: %v", err)
	}
}

func TestCredentialFromToken(t *testing.T) {
	cred, ok, err := CredentialFromToken(testJWT(testUser, 4102444800))
	if err != nil || !ok {
		t.Fatalf("CredentialFromToken = (%v, %v, %v)", cred, ok, err)
	}
	if cred.Token == "" || cred.User != "alice@example.com" {
		t.Fatalf("credential = %+v, want user alice@example.com", cred)
	}
	if !cred.HasExpiry || cred.ExpiresAt.Unix() != 4102444800 {
		t.Fatalf("expiry = %v (%v)", cred.ExpiresAt, cred.HasExpiry)
	}
}

func TestCredentialFromTokenInvalid(t *testing.T) {
	cred, ok, err := CredentialFromToken("not-a-jwt")
	if ok || !errors.Is(err, ErrInvalidToken) {
		t.Fatalf("invalid token = (%v, %v, %v), want (false, ErrInvalidToken)", cred, ok, err)
	}
	if cred.Token != "not-a-jwt" {
		t.Fatalf("token = %q, want the raw token kept for redacted use", cred.Token)
	}
}
