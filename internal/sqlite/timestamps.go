package sqlite

// Derived from rhizome-mcp (https://github.com/Odrin/rhizome-mcp), via
// boboty/agent-board-rhizome-poc, licensed under Apache-2.0. See NOTICE.
// Modified for Agent Board.

import (
	"errors"
	"time"
)

// storageTimestampLayout is the fixed-width canonical form for every stored
// timestamp: UTC with exactly nine fractional digits. time.RFC3339Nano trims
// trailing zeros, which makes SQLite's byte-wise TEXT comparison disagree with
// chronological order ("...05Z" sorts after "...05.1Z"). Fixed width keeps
// both orders identical.
const storageTimestampLayout = "2006-01-02T15:04:05.000000000Z"

// FormatTime renders t in the fixed-width canonical storage form.
func FormatTime(t time.Time) string {
	return t.UTC().Format(storageTimestampLayout)
}

// ParseTime parses a stored timestamp and rejects non-UTC offsets. It parses
// with RFC3339Nano because only a nines-style layout accepts any fraction width.
func ParseTime(value string) (time.Time, error) {
	parsed, err := time.Parse(time.RFC3339Nano, value)
	if err != nil {
		return time.Time{}, err
	}
	if _, offset := parsed.Zone(); offset != 0 {
		return time.Time{}, errors.New("storage timestamp must be UTC")
	}
	return parsed.UTC(), nil
}
