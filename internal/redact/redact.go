// Package redact removes secret values from metadata before any renderer can
// see them. Only values are hidden; key names are always preserved.
package redact

import "strings"

// Marker replaces every redacted value.
const Marker = "[redacted]"

// sensitiveParts are matched against the normalized key (lowercase, letters and
// digits only). Substring matching is deliberate: keys such as db_password,
// API-Key, and authToken all match.
var sensitiveParts = []string{
	"password",
	"passwd",
	"pwd",
	"secret",
	"token",
	"key",
	"credential",
}

// Sensitive reports whether a key name suggests its value is a secret.
func Sensitive(key string) bool {
	normalized := normalize(key)
	for _, part := range sensitiveParts {
		if strings.Contains(normalized, part) {
			return true
		}
	}
	return false
}

// Value returns the value unchanged for non-sensitive keys, or Marker when the
// key looks sensitive. Empty values stay empty.
func Value(key, value string) string {
	if value == "" || !Sensitive(key) {
		return value
	}
	return Marker
}

// Metadata returns a redacted copy of m. The input map is never modified.
func Metadata(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	out := make(map[string]string, len(m))
	for key, value := range m {
		out[key] = Value(key, value)
	}
	return out
}

func normalize(key string) string {
	var b strings.Builder
	for _, r := range strings.ToLower(key) {
		if (r >= 'a' && r <= 'z') || (r >= '0' && r <= '9') {
			b.WriteRune(r)
		}
	}
	return b.String()
}
