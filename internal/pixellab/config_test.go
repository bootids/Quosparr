package pixellab

import (
	"os"
	"path/filepath"
	"testing"
)

func TestConfigPath(t *testing.T) {
	if got := ConfigPath("/root"); got != filepath.Join("/root", "accounts", "pixellab", "auth.json") {
		t.Fatalf("ConfigPath = %q", got)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	root := t.TempDir()
	if _, ok, err := LoadConfig(root); ok || err != nil {
		t.Fatalf("empty store = (%v, %v), want (false, nil)", ok, err)
	}
	if err := SaveConfig(root, "  "+testSecret+"  "); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	key, ok, err := LoadConfig(root)
	if err != nil || !ok {
		t.Fatalf("LoadConfig = (%q, %v, %v)", key, ok, err)
	}
	if key != testSecret {
		t.Fatalf("key = %q, want trimmed %q", key, testSecret)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	key, ok, err := LoadConfig(t.TempDir())
	if ok || err != nil || key != "" {
		t.Fatalf("LoadConfig = (%q, %v, %v), want not configured", key, ok, err)
	}
}

func writeConfig(t *testing.T, root, content string) {
	t.Helper()
	path := ConfigPath(root)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigUndecodable(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, "{not-json")
	if _, _, err := LoadConfig(root); err == nil {
		t.Fatal("LoadConfig on undecodable file = nil error")
	}
}

func TestLoadConfigEmptyKey(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{"apiKey":"   "}`)
	key, ok, err := LoadConfig(root)
	if ok || err != nil || key != "" {
		t.Fatalf("LoadConfig = (%q, %v, %v), want not configured", key, ok, err)
	}
}

func TestSaveConfigEmptyKey(t *testing.T) {
	if err := SaveConfig(t.TempDir(), "   "); err == nil {
		t.Fatal("SaveConfig on empty key = nil error")
	}
}

func TestSaveConfigFilePerms(t *testing.T) {
	root := t.TempDir()
	if err := SaveConfig(root, testSecret); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	info, err := os.Stat(ConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if perm := info.Mode().Perm(); perm != 0o600 {
		t.Fatalf("auth.json perm = %o, want 600", perm)
	}
}

func TestRemoveConfig(t *testing.T) {
	root := t.TempDir()
	if err := SaveConfig(root, testSecret); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	if err := RemoveConfig(root); err != nil {
		t.Fatalf("RemoveConfig: %v", err)
	}
	if _, err := os.Stat(filepath.Join(root, "accounts", "pixellab")); !os.IsNotExist(err) {
		t.Fatalf("accounts/pixellab still present: %v", err)
	}
	if err := RemoveConfig(root); err != nil {
		t.Fatalf("RemoveConfig on missing dir: %v", err)
	}
}
