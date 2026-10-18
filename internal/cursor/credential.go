package cursor

import (
	"context"
	"encoding/base64"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"

	"github.com/egoist/mygo/plugins/sqlite"
)

// ErrInvalidToken reports a stored access token that is not a usable JWT:
// the payload is undecodable or carries no subject.
var ErrInvalidToken = errors.New("cursor: invalid access token")

// StateDBPath returns the path of Cursor's state.vscdb under home.
func StateDBPath(home string) string {
	if runtime.GOOS == "darwin" {
		return filepath.Join(home, "Library/Application Support/Cursor/User/globalStorage/state.vscdb")
	}
	return filepath.Join(home, ".config/Cursor/User/globalStorage/state.vscdb")
}

// Credential is the Cursor session derived from the stored access token.
// Token is the raw JWT; User is the part of its subject after the last '|'.
type Credential struct {
	Token     string
	User      string
	ExpiresAt time.Time
	HasExpiry bool
}

// LoadCredential reads the access token from Cursor's state database at
// dbPath (opened read-only) and decodes it. A missing database, missing key
// or empty token means the provider is not configured: (zero, false, nil).
// A present but unusable token returns ErrInvalidToken; database read
// failures return the underlying error.
func LoadCredential(ctx context.Context, dbPath string) (Credential, bool, error) {
	if _, err := os.Stat(dbPath); err != nil {
		return Credential{}, false, nil
	}
	db, err := sqlite.Open(ctx, dbPath, sqlite.OpenOptions{ReadOnly: true})
	if err != nil {
		return Credential{}, false, err
	}
	defer db.Close()
	rows, err := db.Query(ctx, `SELECT value FROM ItemTable WHERE key = 'cursorAuth/accessToken'`)
	if err != nil {
		return Credential{}, false, err
	}
	var token string
	if len(rows.Values) > 0 && len(rows.Values[0]) > 0 {
		if s, ok := rows.Values[0][0].(string); ok {
			token = s
		}
	}
	if token == "" {
		return Credential{}, false, nil
	}
	return decodeJWT(token)
}

// decodeJWT extracts the user and expiry from the JWT payload (its second
// segment, base64url, padding tolerated).
func decodeJWT(token string) (Credential, bool, error) {
	parts := strings.Split(token, ".")
	if len(parts) < 2 {
		return Credential{Token: token}, false, ErrInvalidToken
	}
	payload, err := base64.RawURLEncoding.DecodeString(strings.TrimRight(parts[1], "="))
	if err != nil {
		return Credential{Token: token}, false, ErrInvalidToken
	}
	var claims struct {
		Sub string `json:"sub"`
		Exp *int64 `json:"exp"`
	}
	if err := json.Unmarshal(payload, &claims); err != nil || claims.Sub == "" {
		return Credential{Token: token}, false, ErrInvalidToken
	}
	cred := Credential{Token: token, User: claims.Sub[strings.LastIndex(claims.Sub, "|")+1:]}
	if claims.Exp != nil {
		cred.ExpiresAt = time.Unix(*claims.Exp, 0).UTC()
		cred.HasExpiry = true
	}
	return cred, true, nil
}
