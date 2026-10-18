package minimax

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestEnvKeyOrder(t *testing.T) {
	t.Setenv(envCodingKey, "")
	t.Setenv(envPlainKey, "")
	if k := EnvKey(); k != "" {
		t.Fatalf("EnvKey = %q, want empty", k)
	}
	t.Setenv(envPlainKey, " plain-key ")
	if k := EnvKey(); k != "plain-key" {
		t.Fatalf("EnvKey = %q, want plain fallback", k)
	}
	t.Setenv(envCodingKey, " coding-key ")
	if k := EnvKey(); k != "coding-key" {
		t.Fatalf("EnvKey = %q, want coding precedence", k)
	}
}

func TestConfigFilePath(t *testing.T) {
	if got := ConfigFilePath("/home/u"); got != filepath.Join("/home/u", ".config", "minimax", "config.json") {
		t.Fatalf("ConfigFilePath = %q", got)
	}
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

func TestLoadCredentialAPIKeyPriority(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"api_key":"file-key","base_url":"https://api.minimaxi.com"}`)
	key, region, ok, err := LoadCredential("explicit-key", "", path)
	if err != nil || !ok || key != "explicit-key" || region != "" {
		t.Fatalf("LoadCredential = (%q, %q, %v, %v)", key, region, ok, err)
	}
	// An explicit region survives alongside an explicit key.
	key, region, ok, err = LoadCredential("explicit-key", RegionIntl, path)
	if err != nil || !ok || key != "explicit-key" || region != RegionIntl {
		t.Fatalf("LoadCredential = (%q, %q, %v, %v)", key, region, ok, err)
	}
}

func TestLoadCredentialFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, fixture(t, "credentials-structure.json"))
	key, region, ok, err := LoadCredential("", "", path)
	if err != nil || !ok {
		t.Fatalf("LoadCredential = (%q, %q, %v, %v)", key, region, ok, err)
	}
	if !strings.HasPrefix(key, "sk-cp-") {
		t.Fatalf("key = %q, want discovered api_key", key)
	}
	if region != RegionCN {
		t.Fatalf("region = %q, want cn hint from minimaxi.com base_url", region)
	}
}

func TestLoadCredentialFileIntlHint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"api_key":"file-key","base_url":"https://api.minimax.io"}`)
	_, region, ok, err := LoadCredential("", "", path)
	if err != nil || !ok || region != RegionIntl {
		t.Fatalf("LoadCredential region = %q, want intl hint", region)
	}
}

func TestLoadCredentialExplicitRegionOverridesHint(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"api_key":"file-key","base_url":"https://api.minimaxi.com"}`)
	_, region, ok, err := LoadCredential("", RegionIntl, path)
	if err != nil || !ok || region != RegionIntl {
		t.Fatalf("LoadCredential region = %q, want explicit intl over cn hint", region)
	}
}

func TestLoadCredentialNoBaseURL(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"api_key":"file-key"}`)
	_, region, ok, err := LoadCredential("", "", path)
	if err != nil || !ok || region != "" {
		t.Fatalf("LoadCredential = region %q ok %v err %v, want empty region", region, ok, err)
	}
}

func TestLoadCredentialMissingFile(t *testing.T) {
	key, region, ok, err := LoadCredential("", "", filepath.Join(t.TempDir(), "absent.json"))
	if ok || err != nil || key != "" || region != "" {
		t.Fatalf("LoadCredential = (%q, %q, %v, %v), want not found", key, region, ok, err)
	}
}

func TestLoadCredentialEmptyPath(t *testing.T) {
	key, region, ok, err := LoadCredential("", "", "")
	if ok || err != nil || key != "" || region != "" {
		t.Fatalf("LoadCredential = (%q, %q, %v, %v), want not found", key, region, ok, err)
	}
}

func TestLoadCredentialEmptyAPIKey(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{"api_key":"  "}`)
	_, _, ok, err := LoadCredential("", "", path)
	if ok || err != nil {
		t.Fatalf("LoadCredential = (%v, %v), want not found", ok, err)
	}
}

func TestLoadCredentialUndecodable(t *testing.T) {
	path := filepath.Join(t.TempDir(), "config.json")
	writeFile(t, path, `{not json`)
	_, _, ok, err := LoadCredential("", "", path)
	if ok || err == nil || !strings.HasPrefix(err.Error(), "minimax: ") {
		t.Fatalf("LoadCredential = (%v, %v), want decode error", ok, err)
	}
}
