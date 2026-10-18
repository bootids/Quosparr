package zai

import (
	"bytes"
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func newStore(t *testing.T) *Store {
	t.Helper()
	root := t.TempDir()
	s, err := LoadStore(root)
	if err != nil {
		t.Fatalf("LoadStore: %v", err)
	}
	return s
}

func TestLoadStoreMissingIndexIsEmpty(t *testing.T) {
	s := newStore(t)
	rows, err := s.Rows()
	if err != nil || len(rows) != 0 {
		t.Fatalf("rows = %v, err = %v", rows, err)
	}
	cur, err := s.Current()
	if err != nil || cur != "" {
		t.Fatalf("current = %q, err = %v", cur, err)
	}
}

func TestAddWritesCredentialAndIndex(t *testing.T) {
	s := newStore(t)
	const key = "sk-store-key-9999"
	id, err := s.Add("Work", key, "cn", "bearer")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if id == "" {
		t.Fatal("id is empty")
	}
	// auth.json content
	data, err := os.ReadFile(s.CredentialPath(id))
	if err != nil {
		t.Fatalf("read auth.json: %v", err)
	}
	var doc struct {
		APIKey string `json:"apiKey"`
	}
	if json.Unmarshal(data, &doc) != nil || doc.APIKey != key {
		t.Fatalf("auth.json = %s", data)
	}
	// index has no key
	idx, err := os.ReadFile(s.IndexPath())
	if err != nil {
		t.Fatalf("read index: %v", err)
	}
	if strings.Contains(string(idx), key) {
		t.Fatalf("index leaks key: %s", idx)
	}
	rows, err := s.Rows()
	if err != nil || len(rows) != 1 {
		t.Fatalf("rows = %+v, err = %v", rows, err)
	}
	r := rows[0]
	if r.ID != id || r.Label != "Work" || r.Region != RegionCN || r.AuthScheme != SchemeBearer {
		t.Fatalf("row = %+v", r)
	}
	if r.KeyHint == "" || strings.Contains(r.KeyHint, key) {
		t.Fatalf("keyHint = %q", r.KeyHint)
	}
	if runtime.GOOS != "windows" {
		checkPerm(t, s.CredentialPath(id), 0o600)
		checkPerm(t, s.AccountDir(id), 0o700)
		checkPerm(t, s.IndexPath(), 0o600)
		checkPerm(t, filepath.Join(s.root, "accounts"), 0o700)
	}
	// dir name carries the account prefix
	if !strings.HasPrefix(filepath.Base(s.AccountDir(id)), accountPrefix) {
		t.Fatalf("account dir = %q", s.AccountDir(id))
	}
}

func checkPerm(t *testing.T, path string, want os.FileMode) {
	t.Helper()
	info, err := os.Stat(path)
	if err != nil {
		t.Fatalf("stat %s: %v", path, err)
	}
	if info.Mode().Perm() != want {
		t.Fatalf("%s perm = %o, want %o", path, info.Mode().Perm(), want)
	}
}

func TestAddValidatesInput(t *testing.T) {
	s := newStore(t)
	if _, err := s.Add("x", "", "intl", "raw"); err == nil {
		t.Fatal("empty key must be rejected")
	}
	if _, err := s.Add("x", "k", "eu", "raw"); err == nil {
		t.Fatal("bad region must be rejected")
	}
	if _, err := s.Add("x", "k", "intl", "oauth"); err == nil {
		t.Fatal("bad scheme must be rejected")
	}
}

func TestAddDefaultsAndLoadKey(t *testing.T) {
	s := newStore(t)
	id, err := s.Add("", "sk-default-key-1", "", "")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	rows, _ := s.Rows()
	if rows[0].Region != RegionIntl || rows[0].AuthScheme != SchemeRaw {
		t.Fatalf("defaults = %+v", rows[0])
	}
	got, err := s.LoadKey(id)
	if err != nil || got != "sk-default-key-1" {
		t.Fatalf("LoadKey = %q, err = %v", got, err)
	}
	if _, err := s.LoadKey("nope"); err == nil {
		t.Fatal("unknown id must fail")
	}
}

func TestAddSameKeyIsStable(t *testing.T) {
	s := newStore(t)
	a, _ := s.Add("first", "sk-same-key", "intl", "raw")
	b, err := s.Add("second", "sk-same-key", "intl", "raw")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if a != b {
		t.Fatalf("ids differ: %q vs %q", a, b)
	}
	rows, _ := s.Rows()
	if len(rows) != 1 || rows[0].Label != "second" {
		t.Fatalf("rows = %+v", rows)
	}
}

func TestAddFirstAccountBecomesCurrent(t *testing.T) {
	s := newStore(t)
	id, err := s.Add("only", "sk-cur-key-1", "intl", "raw")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if cur, _ := s.Current(); cur != id {
		t.Fatalf("current = %q, want %q", cur, id)
	}
}

func TestAddSecondAccountKeepsCurrent(t *testing.T) {
	s := newStore(t)
	a, _ := s.Add("a", "sk-cur-key-2", "intl", "raw")
	b, err := s.Add("b", "sk-cur-key-3", "intl", "raw")
	if err != nil {
		t.Fatalf("Add: %v", err)
	}
	if b == a {
		t.Fatal("ids must differ")
	}
	if cur, _ := s.Current(); cur != a {
		t.Fatalf("current = %q, want %q", cur, a)
	}
}

func TestReAddSameKeyKeepsCurrent(t *testing.T) {
	s := newStore(t)
	a, _ := s.Add("first", "sk-cur-key-4", "intl", "raw")
	if _, err := s.Add("again", "sk-cur-key-4", "intl", "raw"); err != nil {
		t.Fatalf("Add: %v", err)
	}
	if cur, _ := s.Current(); cur != a {
		t.Fatalf("current = %q, want %q", cur, a)
	}
}

// writeIndex writes a raw index document to test Current self-healing on
// stores the normal API cannot produce.
func writeIndex(t *testing.T, s *Store, idx index) {
	t.Helper()
	if err := s.saveIndex(idx); err != nil {
		t.Fatalf("saveIndex: %v", err)
	}
}

func TestCurrentSelfHealsEmptyCurrent(t *testing.T) {
	s := newStore(t)
	a, _ := s.Add("a", "sk-cur-key-5", "intl", "raw")
	writeIndex(t, s, index{Version: indexVersion, Rows: []Row{{ID: a}}})
	if cur, _ := s.Current(); cur != a {
		t.Fatalf("current = %q, want %q", cur, a)
	}
	// self-heal must not persist
	data, err := os.ReadFile(s.IndexPath())
	if err != nil || strings.Contains(string(data), `"current": "`+a) {
		t.Fatalf("index persisted current: %s, err = %v", data, err)
	}
}

func TestCurrentSelfHealsStaleCurrent(t *testing.T) {
	s := newStore(t)
	a, _ := s.Add("a", "sk-cur-key-6", "intl", "raw")
	writeIndex(t, s, index{Version: indexVersion, Current: "ghost", Rows: []Row{{ID: a}}})
	if cur, _ := s.Current(); cur != a {
		t.Fatalf("current = %q, want %q", cur, a)
	}
}

func TestCurrentEmptyWithMultipleRowsAndNoCurrent(t *testing.T) {
	s := newStore(t)
	a, _ := s.Add("a", "sk-cur-key-7", "intl", "raw")
	b, _ := s.Add("b", "sk-cur-key-8", "intl", "raw")
	writeIndex(t, s, index{Version: indexVersion, Rows: []Row{{ID: a}, {ID: b}}})
	if cur, _ := s.Current(); cur != "" {
		t.Fatalf("current = %q, want empty", cur)
	}
}

func TestDeleteCurrentWithOneRemainingSelfHeals(t *testing.T) {
	s := newStore(t)
	a, _ := s.Add("a", "sk-cur-key-9", "intl", "raw")
	b, _ := s.Add("b", "sk-cur-key-10", "intl", "raw")
	if err := s.SetCurrent(a); err != nil {
		t.Fatalf("SetCurrent: %v", err)
	}
	if err := s.Delete(a); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	// The stored selection is cleared, but the sole remaining row
	// self-heals as the current account without rewriting the index.
	if cur, _ := s.Current(); cur != b {
		t.Fatalf("current = %q, want sole remaining %q", cur, b)
	}
	if idx, err := os.ReadFile(s.IndexPath()); err == nil && bytes.Contains(idx, []byte(`"current":"`+b+`"`)) {
		t.Fatal("self-heal must not persist the selection")
	}
}

func TestUpdateEmptyKeyKeepsOld(t *testing.T) {
	s := newStore(t)
	id, _ := s.Add("Work", "sk-orig-key-42", "intl", "raw")
	empty := ""
	if err := s.Update(id, "Home", &empty, "cn", "bearer"); err != nil {
		t.Fatalf("Update: %v", err)
	}
	got, err := s.LoadKey(id)
	if err != nil || got != "sk-orig-key-42" {
		t.Fatalf("key = %q, err = %v", got, err)
	}
	rows, _ := s.Rows()
	if rows[0].Label != "Home" || rows[0].Region != RegionCN || rows[0].AuthScheme != SchemeBearer {
		t.Fatalf("row = %+v", rows[0])
	}
	if rows[0].KeyHint == "" {
		t.Fatal("keyHint lost")
	}
	// nil key pointer also keeps the old key
	if err := s.Update(id, "Home2", nil, "", ""); err != nil {
		t.Fatalf("Update nil: %v", err)
	}
	if got, _ := s.LoadKey(id); got != "sk-orig-key-42" {
		t.Fatalf("key = %q", got)
	}
	// replacing the key rotates it and the hint
	newKey := "sk-rotated-key-7"
	if err := s.Update(id, "Home2", &newKey, "", ""); err != nil {
		t.Fatalf("Update rotate: %v", err)
	}
	if got, _ := s.LoadKey(id); got != newKey {
		t.Fatalf("key = %q", got)
	}
	if err := s.Update("nope", "x", nil, "", ""); err == nil {
		t.Fatal("unknown id must fail")
	}
}

func TestDeleteCurrentClearsCurrent(t *testing.T) {
	s := newStore(t)
	a, _ := s.Add("a", "sk-key-a", "intl", "raw")
	b, _ := s.Add("b", "sk-key-b", "intl", "raw")
	if err := s.SetCurrent(a); err != nil {
		t.Fatalf("SetCurrent: %v", err)
	}
	if cur, _ := s.Current(); cur != a {
		t.Fatalf("current = %q", cur)
	}
	if err := s.Delete(b); err != nil {
		t.Fatalf("Delete: %v", err)
	}
	if cur, _ := s.Current(); cur != a {
		t.Fatalf("current after unrelated delete = %q", cur)
	}
	if err := s.Delete(a); err != nil {
		t.Fatalf("Delete current: %v", err)
	}
	if cur, _ := s.Current(); cur != "" {
		t.Fatalf("current = %q, want empty", cur)
	}
	if rows, _ := s.Rows(); len(rows) != 0 {
		t.Fatalf("rows = %+v", rows)
	}
	if _, err := s.LoadKey(a); err == nil {
		t.Fatal("deleted credential must be gone")
	}
	if err := s.Delete(a); err == nil {
		t.Fatal("double delete must fail")
	}
	if err := s.SetCurrent(b); err == nil {
		t.Fatal("SetCurrent of deleted id must fail")
	}
}

func TestSetCurrentRoundTrip(t *testing.T) {
	s := newStore(t)
	a, _ := s.Add("a", "sk-key-a", "intl", "raw")
	b, _ := s.Add("b", "sk-key-b", "intl", "raw")
	if err := s.SetCurrent(a); err != nil {
		t.Fatalf("SetCurrent: %v", err)
	}
	if err := s.SetCurrent(b); err != nil {
		t.Fatalf("SetCurrent: %v", err)
	}
	cur, err := s.Current()
	if err != nil || cur != b {
		t.Fatalf("current = %q, err = %v", cur, err)
	}
	idx, _ := os.ReadFile(s.IndexPath())
	var doc struct {
		Version int    `json:"version"`
		Current string `json:"current"`
		Rows    []Row  `json:"rows"`
	}
	if json.Unmarshal(idx, &doc) != nil {
		t.Fatalf("index unreadable: %s", idx)
	}
	if doc.Version != 1 || doc.Current != b || len(doc.Rows) != 2 {
		t.Fatalf("index = %s", idx)
	}
	if !strings.Contains(string(idx), `"keyHint"`) {
		t.Fatalf("index missing keyHint: %s", idx)
	}
}
