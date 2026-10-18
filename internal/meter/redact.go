package meter

import (
	"crypto/sha256"
	"encoding/hex"
	"math"
	"strings"
)

// PercentEncode encodes s byte by byte with uppercase hexadecimal escapes,
// leaving ASCII alphanumerics and -_.~ unescaped (RFC 3986 unreserved).
func PercentEncode(s string) string {
	const hexDigits = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c >= 'A' && c <= 'Z', c >= 'a' && c <= 'z', c >= '0' && c <= '9',
			c == '-', c == '_', c == '.', c == '~':
			b.WriteByte(c)
		default:
			b.WriteByte('%')
			b.WriteByte(hexDigits[c>>4])
			b.WriteByte(hexDigits[c&0xF])
		}
	}
	return b.String()
}

// Redact masks every occurrence of each secret in s, longest first, with its
// first four characters plus an ellipsis. Besides the raw secret it also
// masks its percent-encoded form, so a request echo cannot leak either
// variant. Secrets shorter than five characters are masked entirely.
func Redact(s string, secrets ...string) string {
	type variant struct{ from, to string }
	var all []variant
	for _, secret := range secrets {
		if secret == "" {
			continue
		}
		mask := secret[:min(4, len(secret))] + "…"
		if len(secret) < 5 {
			mask = "…"
		}
		for _, form := range []string{secret, PercentEncode(secret)} {
			if form != "" {
				all = append(all, variant{form, mask})
			}
		}
	}
	// Replace longer candidates first so a combined form cannot survive.
	for i := 1; i < len(all); i++ {
		for j := i; j > 0 && len(all[j].from) > len(all[j-1].from); j-- {
			all[j], all[j-1] = all[j-1], all[j]
		}
	}
	for _, v := range all {
		s = strings.ReplaceAll(s, v.from, v.to)
	}
	return s
}

// KeyHint is a non-secret fingerprint of an API key for index rows: the
// first four characters and eight hex characters of the SHA-256 digest.
func KeyHint(key string) string {
	if key == "" {
		return ""
	}
	sum := sha256.Sum256([]byte(key))
	return key[:min(4, len(key))] + "-" + hex.EncodeToString(sum[:4])
}

// Has reports whether a normalized float is present (not NaN).
func Has(v float64) bool { return !math.IsNaN(v) }

// NaN is the absent value of a normalized float.
func NaN() float64 { return math.NaN() }

// Float keeps v when ok, otherwise the absent NaN.
func Float(v float64, ok bool) float64 {
	if ok {
		return v
	}
	return math.NaN()
}
