package pixellab

import (
	"os"
	"path/filepath"
	"testing"
)

func TestTokenFilePath(t *testing.T) {
	if got := TokenFilePath("/home/u"); got != filepath.Join("/home/u", ".config", "pixellab", "token") {
		t.Fatalf("TokenFilePath = %q", got)
	}
}

func TestLoadCredentialExplicitKeyWins(t *testing.T) {
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("file-token"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, ok, err := LoadCredential("explicit-key", token)
	if err != nil || !ok || key != "explicit-key" {
		t.Fatalf("LoadCredential = (%q, %v, %v), want explicit key", key, ok, err)
	}
}

func TestLoadCredentialTrimsToken(t *testing.T) {
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("  file-token\n\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, ok, err := LoadCredential("", token)
	if err != nil || !ok || key != "file-token" {
		t.Fatalf("LoadCredential = (%q, %v, %v), want trimmed token", key, ok, err)
	}
}

func TestLoadCredentialMissingFile(t *testing.T) {
	key, ok, err := LoadCredential("", filepath.Join(t.TempDir(), "token"))
	if ok || err != nil || key != "" {
		t.Fatalf("LoadCredential = (%q, %v, %v), want not found", key, ok, err)
	}
}

func TestLoadCredentialEmptyToken(t *testing.T) {
	dir := t.TempDir()
	token := filepath.Join(dir, "token")
	if err := os.WriteFile(token, []byte("   \n"), 0o600); err != nil {
		t.Fatal(err)
	}
	key, ok, err := LoadCredential("", token)
	if ok || err != nil || key != "" {
		t.Fatalf("LoadCredential = (%q, %v, %v), want not found", key, ok, err)
	}
}
