package interp

import "time"

// parseInstant parses an RFC 3339 date-time (offset or local) into a comparable
// instant (Unix nanoseconds). Local forms are interpreted in UTC for a naive
// comparison; range constraints are same-kind (enforced at meta-schema time), so
// offset-vs-local mixing never reaches here.
func parseInstant(s string) (int64, bool) {
	if t, err := time.Parse(time.RFC3339, s); err == nil {
		return t.UnixNano(), true
	}
	if t, err := time.Parse("2006-01-02T15:04:05", s); err == nil {
		return t.UnixNano(), true
	}
	if t, err := time.Parse("2006-01-02T15:04:05.999999999", s); err == nil {
		return t.UnixNano(), true
	}
	return 0, false
}
