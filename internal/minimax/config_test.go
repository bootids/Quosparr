package minimax

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigPath(t *testing.T) {
	if got := ConfigPath("/root"); got != filepath.Join("/root", "accounts", "minimax", "auth.json") {
		t.Fatalf("ConfigPath = %q", got)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	root := t.TempDir()
	if _, _, ok, err := LoadConfig(root); ok || err != nil {
		t.Fatalf("empty store = (%v, %v), want (false, nil)", ok, err)
	}
	if err := SaveConfig(root, "  "+testSecret+"  ", " intl "); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	key, region, ok, err := LoadConfig(root)
	if err != nil || !ok {
		t.Fatalf("LoadConfig = (%q, %q, %v, %v)", key, region, ok, err)
	}
	if key != testSecret || region != RegionIntl {
		t.Fatalf("config = (%q, %q), want (%q, intl)", key, region, testSecret)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	key, region, ok, err := LoadConfig(t.TempDir())
	if ok || err != nil || key != "" || region != "" {
		t.Fatalf("LoadConfig = (%q, %q, %v, %v), want not configured", key, region, ok, err)
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
	_, _, ok, err := LoadConfig(root)
	if ok || err == nil {
		t.Fatalf("ok=%v err=%v, want decode error", ok, err)
	}
	if !strings.HasPrefix(err.Error(), "minimax: ") {
		t.Fatalf("err = %v, want minimax prefix", err)
	}
}

func TestLoadConfigEmptyKey(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{"apiKey":"   ","region":"intl"}`)
	key, region, ok, err := LoadConfig(root)
	if ok || err != nil || key != "" || region != "" {
		t.Fatalf("LoadConfig = (%q, %q, %v, %v), want not configured", key, region, ok, err)
	}
}

func TestLoadConfigInvalidRegion(t *testing.T) {
	root := t.TempDir()
	writeConfig(t, root, `{"apiKey":"`+testSecret+`","region":"japan"}`)
	key, region, ok, err := LoadConfig(root)
	if err != nil || !ok || key != testSecret {
		t.Fatalf("LoadConfig = (%q, %q, %v, %v)", key, region, ok, err)
	}
	if region != "" {
		t.Fatalf("region = %q, want unset for unknown value", region)
	}
}

func TestSaveConfigNormalizesRegion(t *testing.T) {
	root := t.TempDir()
	if err := SaveConfig(root, testSecret, "japan"); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	data, err := os.ReadFile(ConfigPath(root))
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"region":""`) {
		t.Fatalf("stored config = %s, want empty region", data)
	}
}

func TestSaveConfigEmptyKey(t *testing.T) {
	root := t.TempDir()
	err := SaveConfig(root, "   ", RegionCN)
	if err == nil || !strings.HasPrefix(err.Error(), "minimax: ") {
		t.Fatalf("err = %v, want minimax error", err)
	}
	if _, statErr := os.Stat(ConfigPath(root)); !os.IsNotExist(statErr) {
		t.Fatalf("config written for empty key: %v", statErr)
	}
}

func TestRemoveConfig(t *testing.T) {
	root := t.TempDir()
	if err := SaveConfig(root, testSecret, RegionCN); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	dir := filepath.Join(root, "accounts", "minimax")
	if err := RemoveConfig(root); err != nil {
		t.Fatalf("RemoveConfig: %v", err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Fatalf("dir %q still exists", dir)
	}
	if _, _, ok, err := LoadConfig(root); ok || err != nil {
		t.Fatalf("after remove = (%v, %v), want not configured", ok, err)
	}
	// Removing again is a no-op, not an error.
	if err := RemoveConfig(root); err != nil {
		t.Fatalf("second RemoveConfig: %v", err)
	}
}
