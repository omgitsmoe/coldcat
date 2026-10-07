package database

import (
	"errors"
	"strings"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestDirectoryComparisonDoublestarFilters(t *testing.T) {
	tests := []struct {
		name     string
		allow    []string
		block    []string
		path     string
		selected bool
	}{
		{name: "star segment", allow: []string{"*.txt"}, path: "file.txt", selected: true},
		{name: "star boundary", allow: []string{"*.txt"}, path: "nested/file.txt"},
		{name: "doublestar root", block: []string{"**/*.log"}, path: "debug.log"},
		{name: "doublestar nested", block: []string{"**/*.log"}, path: "a/debug.log"},
		{name: "question", allow: []string{"file?.txt"}, path: "file1.txt", selected: true},
		{name: "class", allow: []string{"file[0-9].txt"}, path: "file4.txt", selected: true},
		{name: "alternative", allow: []string{"*.{jpg,png}"}, path: "image.png", selected: true},
		{name: "escaped star", allow: []string{`literal\*.txt`}, path: "literal*.txt", selected: true},
		{name: "case sensitive", allow: []string{"*.TXT"}, path: "file.txt"},
		{name: "block precedence", allow: []string{"**"}, block: []string{"private/**"},
			path: "private/file", selected: false},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			filters, err := NormalizeDirectoryComparisonFilters(base.DirectoryComparisonFilters{
				SnapshotID: 1,
				Allow:      test.allow,
				Block:      test.block,
			})
			if err != nil {
				t.Fatal(err)
			}

			if selected := comparisonSelected(filters, test.path); selected != test.selected {
				t.Fatalf("selected %v, want %v", selected, test.selected)
			}
		})
	}

	for _, pattern := range []string{"", "[", "/absolute/**", "bad\x00pattern"} {
		_, err := NormalizeDirectoryComparisonFilters(base.DirectoryComparisonFilters{
			SnapshotID: 1,
			Allow:      []string{pattern},
		})
		if !errors.Is(err, ErrValidation) {
			t.Fatalf("accepted invalid pattern %q: %v", pattern, err)
		}
	}

	tooMany := make([]string, maxDirectoryPatterns+1)
	for i := range tooMany {
		tooMany[i] = "a"
	}
	if _, err := NormalizeDirectoryComparisonFilters(base.DirectoryComparisonFilters{
		SnapshotID: 1,
		Allow:      tooMany,
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("accepted too many patterns: %v", err)
	}

	if _, err := NormalizeDirectoryComparisonFilters(base.DirectoryComparisonFilters{
		SnapshotID: 1,
		Allow:      []string{strings.Repeat("a", maxDirectoryPatternBytes+1)},
	}); !errors.Is(err, ErrValidation) {
		t.Fatalf("accepted oversized pattern: %v", err)
	}
}
