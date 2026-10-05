package importer

import (
	"strings"
	"testing"
)

func TestParserPreservesMetadataPresenceAndPathSpelling(t *testing.T) {
	for _, test := range []struct {
		input                 string
		sizeKnown, mtimeKnown bool
		path                  string
	}{
		{",sha256,ab foo/É[?]*.txt", false, false, "foo/É[?]*.txt"},
		{"# version 1\n0,0,sha256,ab literal\\name", true, true, "literal\\name"},
		{"# comment\n# version 1\n,,sha256,ab unknown", false, false, "unknown"},
	} {
		var got File
		if err := ParseCshd(strings.NewReader(test.input), func(f File) error { got = f; return nil }); err != nil {
			t.Fatal(err)
		}
		if got.SizeKnown != test.sizeKnown || got.MTimeKnown != test.mtimeKnown || got.path() != test.path {
			t.Fatalf("presence/spelling: %+v", got)
		}
	}
}

func TestParserRejectsInvalidRecordsAndScannerFailure(t *testing.T) {
	for _, input := range []string{
		"# version -1\n", "# version 1\n# version 1\n", ",sha256,ab a\n# version 1\n",
		"NaN,sha256,ab a", "+Inf,sha256,ab a", "253402300800,sha256,ab a",
		",sha256,ab C:/absolute", ",sha256,ab foo//bar", ",sha256,ab foo/./bar", ",sha256,ab foo/", ",sha256,ab ",
		",sha256,ab " + strings.Repeat("x", 70_000),
	} {
		if err := ParseCshd(strings.NewReader(input), func(File) error { return nil }); err == nil {
			t.Fatalf("accepted input: %.80s", input)
		}
	}
}
