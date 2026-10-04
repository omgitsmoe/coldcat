package database

import (
	"time"
)

// timeLayout is the on-disk encoding of the TEXT timestamp columns
// (snapshot.created_at, observation.mtime).
//
// Binding a time.Time as a query argument would not produce this layout:
// the driver formats it with time.Time.String(), which embeds the writing
// machine's local time zone and is not parsable again.
const timeLayout = time.RFC3339Nano

func FormatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}
