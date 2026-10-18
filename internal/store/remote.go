package store

import (
	"encoding/json"
	"fmt"
	"strings"
)

// RemoteSource is the global data-source switch: local files or the same
// files over SSH on a remote machine. It is Go-only: written as one object,
// left out while every field is at its default, and an unreadable value is
// the default (nil).
type RemoteSource struct {
	Enabled bool   `json:"enabled"` // true = remote mode
	Host    string `json:"host"`    // "user@host" or "host", passed to ssh as is
	Port    int    `json:"port"`    // 0 = the default 22
	Dir     string `json:"dir"`     // the remote ~/.quosparr; empty = ~/.quosparr
}

// clone returns a deep copy; the receiver is a value type, so this only
// exists for the Preferences.Clone pointer field.
func (s RemoteSource) clone() RemoteSource { return s }

// isZero reports whether no field was set, the form that is not written.
func (s RemoteSource) isZero() bool {
	return !s.Enabled && s.Host == "" && s.Port == 0 && s.Dir == ""
}

// hasControlChars reports whether s contains a byte in \x00-\x1f.
func hasControlChars(s string) bool {
	return strings.IndexFunc(s, func(r rune) bool { return r < 0x20 }) >= 0
}

// validate reports the first field a user could not have meant: a remote
// mode without a host, a port that is neither 0 nor a TCP port, and a host
// or directory with a control character.
func (s RemoteSource) validate() error {
	if s.isZero() {
		return nil
	}
	if s.Enabled && s.Host == "" {
		return fmt.Errorf("preferences: remote source is enabled but has no host")
	}
	if s.Port != 0 && (s.Port < 1 || s.Port > 65535) {
		return fmt.Errorf("preferences: invalid remote source port %d", s.Port)
	}
	if hasControlChars(s.Host) {
		return fmt.Errorf("preferences: invalid remote source host")
	}
	if hasControlChars(s.Dir) {
		return fmt.Errorf("preferences: invalid remote source directory")
	}
	return nil
}

// readRemoteSource decodes the Go-only remote source leniently, as
// readFloatingWindow: a missing key or an unreadable object is nil.
func readRemoteSource(r *reader, key string) *RemoteSource {
	raw, ok := r.take(key)
	if !ok {
		return nil
	}
	var s RemoteSource
	if json.Unmarshal(raw, &s) != nil {
		return nil
	}
	return &s
}
