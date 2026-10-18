package zai

import (
	"bytes"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"github.com/licoy/quosparr/internal/codex"
	"github.com/licoy/quosparr/internal/meter"
	"github.com/licoy/quosparr/internal/platform"
)

// Region and auth scheme values of an account row.
const (
	RegionIntl    = "intl"
	RegionCN      = "cn"
	SchemeRaw     = "raw"
	SchemeBearer  = "bearer"
	indexVersion  = 1
	indexFileName = "zai-index.json"
	authFileName  = "auth.json"
	accountPrefix = "zai-api-"
	storeFilePerm = 0o600
)

// Row is one stored Z.ai API-key account. It never contains the key, only
// the non-secret KeyHint fingerprint.
type Row struct {
	ID         string    `json:"id"`
	Label      string    `json:"label"`
	Region     string    `json:"region"`
	AuthScheme string    `json:"authScheme"`
	KeyHint    string    `json:"keyHint"`
	UpdatedAt  time.Time `json:"updatedAt"`
}

type index struct {
	Version int    `json:"version"`
	Current string `json:"current"`
	Rows    []Row  `json:"rows"`
}

// Store is the local library of Z.ai API-key accounts under
// <root>/accounts. The root is a parameter so the app decides where
// ~/.quosparr lives; there is no global state. Credentials are written
// atomically (temp file plus rename in the same directory) with owner-only
// permissions, mirroring the Claude account store.
type Store struct {
	root string
	mu   sync.Mutex
}

// LoadStore opens the store under root, creating <root>/accounts when
// missing. A missing index file is an empty store.
func LoadStore(root string) (*Store, error) {
	s := &Store{root: root}
	if err := platform.EnsurePrivateDirectory(s.accountsDir()); err != nil {
		return nil, err
	}
	return s, nil
}

func (s *Store) accountsDir() string { return filepath.Join(s.root, "accounts") }

// IndexPath is zai-index.json inside the accounts directory.
func (s *Store) IndexPath() string { return filepath.Join(s.accountsDir(), indexFileName) }

// AccountDir is the directory of one account id.
func (s *Store) AccountDir(id string) string {
	return filepath.Join(s.accountsDir(), accountPrefix+sanitizeID(id))
}

// CredentialPath is the managed auth.json of one account id.
func (s *Store) CredentialPath(id string) string {
	return filepath.Join(s.AccountDir(id), authFileName)
}

func sanitizeID(id string) string {
	var b strings.Builder
	for _, r := range id {
		switch {
		case r >= 'a' && r <= 'z', r >= 'A' && r <= 'Z', r >= '0' && r <= '9', r == '-':
			b.WriteRune(r)
		default:
			b.WriteByte('_')
		}
	}
	return b.String()
}

// Rows returns the stored account rows.
func (s *Store) Rows() ([]Row, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, err := s.loadIndex()
	if err != nil {
		return nil, err
	}
	return idx.Rows, nil
}

// Current returns the current account id, empty when none is set.
func (s *Store) Current() (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, err := s.loadIndex()
	if err != nil {
		return "", err
	}
	return idx.Current, nil
}

// LoadKey returns the stored API key of one account.
func (s *Store) LoadKey(id string) (string, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	data, err := os.ReadFile(s.CredentialPath(id))
	if errors.Is(err, os.ErrNotExist) {
		return "", &AccountNotFoundError{ID: id}
	}
	if err != nil {
		return "", err
	}
	var doc struct {
		APIKey string `json:"apiKey"`
	}
	if json.Unmarshal(data, &doc) != nil || strings.TrimSpace(doc.APIKey) == "" {
		return "", errors.New("zai: stored credential is unreadable")
	}
	return doc.APIKey, nil
}

// Add stores a new account and returns its generated id. The id is stable
// for the same key (hash of the trimmed key), so re-adding a key updates
// its row instead of duplicating it. region must be "intl" or "cn";
// authScheme must be "raw" or "bearer"; the key must not be empty.
func (s *Store) Add(label, key, region, authScheme string) (string, error) {
	key = strings.TrimSpace(key)
	if key == "" {
		return "", errors.New("zai: API key is required")
	}
	region, authScheme, err := normalizeSettings(region, authScheme)
	if err != nil {
		return "", err
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, err := s.loadIndex()
	if err != nil {
		return "", err
	}
	id := codex.StableHash(key)
	row := Row{
		ID: id, Label: label, Region: region, AuthScheme: authScheme,
		KeyHint: meter.KeyHint(key), UpdatedAt: time.Now().UTC(),
	}
	if err := s.saveCredential(id, key); err != nil {
		return "", err
	}
	upsertRow(&idx, row)
	if err := s.saveIndex(idx); err != nil {
		return "", err
	}
	return id, nil
}

// Update changes the metadata of one account. A nil or empty key keeps the
// stored key (and its hint); a non-empty key replaces it.
func (s *Store) Update(id, label string, key *string, region, authScheme string) error {
	region, authScheme, err := normalizeSettings(region, authScheme)
	if err != nil {
		return err
	}
	if key != nil {
		trimmed := strings.TrimSpace(*key)
		key = &trimmed
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, err := s.loadIndex()
	if err != nil {
		return err
	}
	i, ok := findRow(idx, id)
	if !ok {
		return &AccountNotFoundError{ID: id}
	}
	row := idx.Rows[i]
	row.Label = label
	row.Region = region
	row.AuthScheme = authScheme
	if key != nil && *key != "" {
		if err := s.saveCredential(id, *key); err != nil {
			return err
		}
		row.KeyHint = meter.KeyHint(*key)
	}
	row.UpdatedAt = time.Now().UTC()
	idx.Rows[i] = row
	return s.saveIndex(idx)
}

// Delete removes one account. Deleting the current account clears the
// current selection.
func (s *Store) Delete(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, err := s.loadIndex()
	if err != nil {
		return err
	}
	i, ok := findRow(idx, id)
	if !ok {
		return &AccountNotFoundError{ID: id}
	}
	idx.Rows = append(idx.Rows[:i], idx.Rows[i+1:]...)
	if idx.Current == id {
		idx.Current = ""
	}
	if err := s.saveIndex(idx); err != nil {
		return err
	}
	os.RemoveAll(s.AccountDir(id))
	return nil
}

// SetCurrent marks one account current. The id must exist.
func (s *Store) SetCurrent(id string) error {
	s.mu.Lock()
	defer s.mu.Unlock()
	idx, err := s.loadIndex()
	if err != nil {
		return err
	}
	if _, ok := findRow(idx, id); !ok {
		return &AccountNotFoundError{ID: id}
	}
	idx.Current = id
	return s.saveIndex(idx)
}

// AccountNotFoundError is returned for an id that is not in the store.
type AccountNotFoundError struct{ ID string }

func (e *AccountNotFoundError) Error() string { return "zai: account not found: " + e.ID }

func normalizeSettings(region, authScheme string) (string, string, error) {
	switch region {
	case RegionIntl, RegionCN:
	case "":
		region = RegionIntl
	default:
		return "", "", errors.New("zai: region must be intl or cn")
	}
	switch authScheme {
	case SchemeRaw, SchemeBearer:
	case "":
		authScheme = SchemeRaw
	default:
		return "", "", errors.New("zai: auth scheme must be raw or bearer")
	}
	return region, authScheme, nil
}

func findRow(idx index, id string) (int, bool) {
	for i := range idx.Rows {
		if idx.Rows[i].ID == id {
			return i, true
		}
	}
	return 0, false
}

func upsertRow(idx *index, row Row) {
	if i, ok := findRow(*idx, row.ID); ok {
		idx.Rows[i] = row
		return
	}
	idx.Rows = append(idx.Rows, row)
}

func (s *Store) loadIndex() (index, error) {
	data, err := os.ReadFile(s.IndexPath())
	if errors.Is(err, os.ErrNotExist) {
		return index{Version: indexVersion, Rows: []Row{}}, nil
	}
	if err != nil {
		return index{}, err
	}
	var idx index
	if json.Unmarshal(data, &idx) != nil {
		return index{}, errors.New("zai: zai-index.json is unreadable")
	}
	if idx.Version != indexVersion {
		return index{}, errors.New("zai: unsupported zai-index.json version")
	}
	if idx.Rows == nil {
		idx.Rows = []Row{}
	}
	return idx, nil
}

func (s *Store) saveIndex(idx index) error {
	if idx.Rows == nil {
		idx.Rows = []Row{}
	}
	if idx.Version == 0 {
		idx.Version = indexVersion
	}
	var buf bytes.Buffer
	enc := json.NewEncoder(&buf)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(idx); err != nil {
		return err
	}
	return platform.WriteFileAtomic(s.IndexPath(), buf.Bytes(), storeFilePerm)
}

func (s *Store) saveCredential(id, key string) error {
	if err := platform.EnsurePrivateDirectory(s.AccountDir(id)); err != nil {
		return err
	}
	body, err := json.Marshal(struct {
		APIKey string `json:"apiKey"`
	}{APIKey: key})
	if err != nil {
		return err
	}
	return platform.WriteFileAtomic(s.CredentialPath(id), body, storeFilePerm)
}
