package agy

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestConfigPath(t *testing.T) {
	if got := ConfigPath("/root"); got != filepath.Join("/root", "accounts", "agy", "config.json") {
		t.Fatalf("ConfigPath = %q", got)
	}
}

func TestConfigRoundTrip(t *testing.T) {
	root := t.TempDir()
	if _, ok, err := LoadConfig(root); ok || err != nil {
		t.Fatalf("empty store = (%v, %v), want (false, nil)", ok, err)
	}
	in := Config{Bin: "/opt/agy/bin/agy", ProfileDir: "/opt/agy/profile"}
	if err := SaveConfig(root, in); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	got, ok, err := LoadConfig(root)
	if err != nil || !ok {
		t.Fatalf("LoadConfig = (%+v, %v, %v)", got, ok, err)
	}
	if got != in {
		t.Fatalf("config = %+v, want %+v", got, in)
	}
}

func TestLoadConfigMissingFile(t *testing.T) {
	cfg, ok, err := LoadConfig(t.TempDir())
	if ok || err != nil || cfg != (Config{}) {
		t.Fatalf("LoadConfig = (%+v, %v, %v), want not configured", cfg, ok, err)
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
	if !strings.HasPrefix(err.Error(), "agy: ") {
		t.Fatalf("err = %v, want agy prefix", err)
	}
}

func TestConfigEmptyFields(t *testing.T) {
	root := t.TempDir()
	// Saving an all-empty config is allowed: explicit reset.
	if err := SaveConfig(root, Config{}); err != nil {
		t.Fatalf("SaveConfig empty: %v", err)
	}
	cfg, ok, err := LoadConfig(root)
	if ok || err != nil || cfg != (Config{}) {
		t.Fatalf("LoadConfig = (%+v, %v, %v), want not configured", cfg, ok, err)
	}
	// Whitespace-only fields count as empty too.
	writeConfig(t, root, `{"bin":"   ","profileDir":"  "}`)
	if _, ok, err := LoadConfig(root); ok || err != nil {
		t.Fatalf("whitespace config = (%v, %v), want not configured", ok, err)
	}
	// One field set is enough to count as configured.
	writeConfig(t, root, `{"bin":"","profileDir":"/opt/agy/profile"}`)
	cfg, ok, err = LoadConfig(root)
	if err != nil || !ok || cfg.ProfileDir != "/opt/agy/profile" {
		t.Fatalf("profile-only config = (%+v, %v, %v)", cfg, ok, err)
	}
}

func TestRemoveConfig(t *testing.T) {
	root := t.TempDir()
	if err := SaveConfig(root, Config{Bin: "/opt/agy/bin/agy"}); err != nil {
		t.Fatalf("SaveConfig: %v", err)
	}
	dir := filepath.Join(root, "accounts", "agy")
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

func TestDetectInProfileDirOverride(t *testing.T) {
	dir := t.TempDir()
	bin := writeFake(t, dir, fixture, 0)
	custom := t.TempDir()

	// Override wins over the default and replaces its presence requirement:
	// this home has no .gemini/antigravity-cli at all.
	home := t.TempDir()
	d, ok := DetectIn(bin, custom, home)
	if !ok || d.Bin != bin || d.ProfileDir != custom {
		t.Fatalf("override = (%+v, %v)", d, ok)
	}

	// A nonexistent override fails even though the default exists.
	home = fakeHome(t, false)
	if _, ok := DetectIn(bin, filepath.Join(home, "absent"), home); ok {
		t.Fatal("DetectIn ok with nonexistent profile override")
	}
	// A file (not a directory) as override fails too.
	filePath := filepath.Join(home, "file-profile")
	if err := os.WriteFile(filePath, []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, ok := DetectIn(bin, filePath, home); ok {
		t.Fatal("DetectIn ok with file profile override")
	}

	// Empty override keeps the default directory.
	d, ok = DetectIn(bin, "", home)
	if !ok || d.ProfileDir != filepath.Join(home, ".gemini", "antigravity-cli") {
		t.Fatalf("default = (%+v, %v)", d, ok)
	}
}

func TestDetectWrapsDetectIn(t *testing.T) {
	dir := t.TempDir()
	bin := writeFake(t, dir, fixture, 0)
	home := fakeHome(t, false)
	d, ok := Detect(bin, home)
	if !ok {
		t.Fatal("Detect failed")
	}
	if dIn, okIn := DetectIn(bin, "", home); okIn != ok || dIn != d {
		t.Fatalf("Detect = (%+v, %v), DetectIn = (%+v, %v)", d, ok, dIn, okIn)
	}
}
