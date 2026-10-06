package importer

import (
	"bytes"
	"crypto"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestSemanticDigest(t *testing.T) {
	digest := func(input string) [32]byte {
		t.Helper()
		d := newInventoryDigest()
		assertNoErr(t, ParseCshd(strings.NewReader(input), d.add))
		return d.sum()
	}
	original := digest(",sha256," + fixtureSHA256AB + " dir/file\n,sha256," + fixtureSHA256CD + " second\n")
	for _, input := range []string{
		"# comment\r\n# version 0\r\n,sha256," + strings.ToUpper(fixtureSHA256AB) +
			" dir/file\r\n,sha256," + fixtureSHA256CD + " second",
		"# version 1\n,,sha256," + fixtureSHA256AB + " dir/file\n,,sha256," + fixtureSHA256CD + " second\n",
	} {
		if got := digest(input); got != original {
			t.Fatalf("equivalent records have different digest: %q", input)
		}
	}

	for name, input := range map[string]string{
		"order":     ",sha256," + fixtureSHA256CD + " second\n,sha256," + fixtureSHA256AB + " dir/file\n",
		"path":      ",sha256," + fixtureSHA256AB + " dir/other\n,sha256," + fixtureSHA256CD + " second\n",
		"hash":      ",sha256," + fixtureSHA256AC + " dir/file\n,sha256," + fixtureSHA256CD + " second\n",
		"algorithm": ",sha3_256," + fixtureSHA256AB + " dir/file\n,sha256," + fixtureSHA256CD + " second\n",
		"known zero": "# version 1\n,0,sha256," + fixtureSHA256AB +
			" dir/file\n,,sha256," + fixtureSHA256CD + " second\n",
		"known mtime":  "0,sha256," + fixtureSHA256AB + " dir/file\n,sha256," + fixtureSHA256CD + " second\n",
		"multiplicity": ",sha256," + fixtureSHA256AB + " dir/file\n",
	} {
		t.Run(name, func(t *testing.T) {
			if digest(input) == original {
				t.Fatal("different records have identical digest")
			}
		})
	}

	if digest("1,sha256,"+fixtureSHA256AB+" file\n") !=
		digest("1.000000000,sha256,"+fixtureSHA256AB+" file") {
		t.Fatal("equivalent timestamps differ")
	}
}

func TestDigestUsesOnlyKnownMetadataAndCanonicalPath(t *testing.T) {
	file := File{
		Name:               "file",
		PathRelativeToRoot: "dir/",
		HashType:           base.HashType{Hash: crypto.SHA256},
		Hash:               []byte{0xab},
	}
	first := newInventoryDigest()
	assertNoErr(t, first.add(file))
	file.Name = "dir/file"
	file.PathRelativeToRoot = ""
	file.SizeInBytes = 99
	file.MTime = time.Now()
	second := newInventoryDigest()
	assertNoErr(t, second.add(file))
	if first.sum() != second.sum() {
		t.Fatal("path split or unknown metadata changed digest")
	}

	if bytes.Equal(first.h.Sum(nil), newInventoryDigest().h.Sum(nil)) {
		t.Fatal("record omitted from digest")
	}
}
