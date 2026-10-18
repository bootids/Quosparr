package kimi

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigPath(t *testing.T) {
	if got := ConfigPath("/root"); got != filepath.Join("/root", "accounts", "kimi", "auth.json") {
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
	if err := os.MkdirAll(filepath.Dir(ConfigPath(root)), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(ConfigPath(root), []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadConfigUndecodable(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{not json`)
	_, ok, err := LoadConfig(root)
	if ok || err == nil {
		t.Fatalf("ok=%v err=%v, want decode error", ok, err)
	}
	if !strings.HasPrefix(err.Error(), "kimi: ") {
		t.Fatalf("err = %v, want kimi prefix", err)
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
	root := t.TempDir()
	err := SaveConfig(root, "   ")
	if err == nil || !strings.HasPrefix(err.Error(), "kimi: ") {
		t.Fatalf("err = %v, want kimi error", err)
	}
	if _, statErr := os.Stat(ConfigPath(root)); !os.IsNotExist(statErr) {
		t.Fatalf("config written for empty key: %v", statErr)
	}
}

func TestRemoveConfig(t *testing.T) {
	root := t.TempDir()
	if err := SaveConfig(root, testSecret); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	dir := filepath.Join(root, "accounts", "kimi")
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
