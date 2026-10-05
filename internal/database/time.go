package database

import (
	"time"
)

// Fixed precision makes UTC timestamps sort chronologically in SQLite TEXT indexes.
const timeLayout = "2006-01-02T15:04:05.000000000Z"

func FormatTime(t time.Time) string {
	return t.UTC().Format(timeLayout)
}

func ParseTime(value string) (time.Time, error) { return time.Parse(time.RFC3339Nano, value) }
