package importer

import (
	"crypto"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
)

var errCallback = errors.New("callback failed")

func TestParse(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []File
		// wantErrIs is the error the parse must fail with, matched with
		// errors.Is. Nil means the parse is expected to succeed.
		wantErrIs error
		// failCallback makes the FileFunc return errCallback.
		failCallback bool
	}{
		{
			name: "valid input version 0",
			input: `1673815645.7979772,sha512,deadbeef bar foo/bar/baz xer/file.txt
# comments
# supported
,md5,ffffff foo/bar
,sha256,ababab xer/foo.bin`,
			expected: []File{
				{
					Name:               "file.txt",
					PathRelativeToRoot: "bar foo/bar/baz xer/",
					MTime:              time.Unix(1673815645, 797977200),
					SizeInBytes:        0,
					HashType:           base.HashType{Hash: crypto.SHA512},
					Hash:               []byte{0xde, 0xad, 0xbe, 0xef},
				},
				{
					Name:               "bar",
					PathRelativeToRoot: "foo/",
					MTime:              time.Time{},
					SizeInBytes:        0,
					HashType:           base.HashType{Hash: crypto.MD5},
					Hash:               []byte{0xff, 0xff, 0xff},
				},
				{
					Name:               "foo.bin",
					PathRelativeToRoot: "xer/",
					MTime:              time.Time{},
					SizeInBytes:        0,
					HashType:           base.HashType{Hash: crypto.SHA256},
					Hash:               []byte{0xab, 0xab, 0xab},
				},
			},
		},
		{
			name: "valid input version 1",
			input: `# version 1
# comments
1673815645.7979772,1337,sha512,deadbeef bar foo/bar/baz xer/file.txt
# supported
,,md5,ffffff foo/bar
,42069,sha256,ababab xer/foo.bin`,
			expected: []File{
				{
					Name:               "file.txt",
					PathRelativeToRoot: "bar foo/bar/baz xer/",
					MTime:              time.Unix(1673815645, 797977200),
					SizeInBytes:        1337,
					HashType:           base.HashType{Hash: crypto.SHA512},
					Hash:               []byte{0xde, 0xad, 0xbe, 0xef},
				},
				{
					Name:               "bar",
					PathRelativeToRoot: "foo/",
					MTime:              time.Time{},
					SizeInBytes:        0,
					HashType:           base.HashType{Hash: crypto.MD5},
					Hash:               []byte{0xff, 0xff, 0xff},
				},
				{
					Name:               "foo.bin",
					PathRelativeToRoot: "xer/",
					MTime:              time.Time{},
					SizeInBytes:        42069,
					HashType:           base.HashType{Hash: crypto.SHA256},
					Hash:               []byte{0xab, 0xab, 0xab},
				},
			},
		},
		{
			// Dedup used to be HashCollection's job; Parse now reports
			// duplicates to the callback as-is.
			name: "duplicate path version 0",
			input: `,md5,ffffff foo/bar
,sha256,ababab foo/bar`,
			expected: []File{
				{
					Name:               "bar",
					PathRelativeToRoot: "foo/",
					MTime:              time.Time{},
					SizeInBytes:        0,
					HashType:           base.HashType{Hash: crypto.MD5},
					Hash:               []byte{0xff, 0xff, 0xff},
				},
				{
					Name:               "bar",
					PathRelativeToRoot: "foo/",
					MTime:              time.Time{},
					SizeInBytes:        0,
					HashType:           base.HashType{Hash: crypto.SHA256},
					Hash:               []byte{0xab, 0xab, 0xab},
				},
			},
		},
		{
			name: "duplicate path version 1",
			input: `# version 1
,,md5,ffffff foo/bar
,,sha256,ababab foo/bar`,
			expected: []File{
				{
					Name:               "bar",
					PathRelativeToRoot: "foo/",
					MTime:              time.Time{},
					SizeInBytes:        0,
					HashType:           base.HashType{Hash: crypto.MD5},
					Hash:               []byte{0xff, 0xff, 0xff},
				},
				{
					Name:               "bar",
					PathRelativeToRoot: "foo/",
					MTime:              time.Time{},
					SizeInBytes:        0,
					HashType:           base.HashType{Hash: crypto.SHA256},
					Hash:               []byte{0xab, 0xab, 0xab},
				},
			},
		},
		{
			name:      "invalid input version 0: invalid line",
			input:     `1673815645.7979772,sha512 bar foo/bar/baz xer/file.txt`,
			wantErrIs: ErrMissingField,
		},
		{
			name: "invalid input version 1: invalid line",
			input: `# version 1
1673815645.7979772,,sha512 bar foo/bar/baz xer/file.txt`,
			wantErrIs: ErrMissingField,
		},
		{
			name:         "callback error aborts the parse",
			input:        ",md5,ffffff foo/bar\n,sha256,ababab foo/bar",
			failCallback: true,
			wantErrIs:    errCallback,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var got []File
			err := ParseCshd(strings.NewReader(tt.input), func(f File) error {
				if tt.failCallback {
					return errCallback
				}
				got = append(got, f)
				return nil
			})

			if tt.wantErrIs != nil {
				assertErr(t, err)
				if !errors.Is(err, tt.wantErrIs) {
					t.Fatalf("expected error %v, got %v", tt.wantErrIs, err)
				}
				return
			}
			assertNoErr(t, err)

			assertFilesEqual(t, got, tt.expected)
		})
	}
}

func TestParseLine(t *testing.T) {
	tests := []struct {
		name         string
		version      int
		line         string
		expectedFile File
		wantErr      bool
	}{
		{
			name:    "full valid line version 0",
			version: 0,
			line:    "1673815645.7979772,sha512,deadbeef foo/bar/baz xer/file.txt",
			expectedFile: File{
				Name:               "file.txt",
				PathRelativeToRoot: "foo/bar/baz xer/",
				SizeInBytes:        0,
				MTime:              time.Unix(1673815645, 797977200),
				HashType:           base.HashType{Hash: crypto.SHA512},
				Hash:               []byte{0xde, 0xad, 0xbe, 0xef},
			},
			wantErr: false,
		},
		{
			name:    "valid line with empty fields version 0",
			version: 0,
			line:    ",sha512,deadbeef foo/bar/baz xer/file.txt",
			expectedFile: File{
				Name:               "file.txt",
				PathRelativeToRoot: "foo/bar/baz xer/",
				SizeInBytes:        0,
				MTime:              time.Time{},
				HashType:           base.HashType{Hash: crypto.SHA512},
				Hash:               []byte{0xde, 0xad, 0xbe, 0xef},
			},
			wantErr: false,
		},
		{
			name:    "full valid line version 1",
			version: 1,
			line:    "1673815645.7979772,1337,sha512,deadbeef foo/bar/baz xer/file.txt",
			expectedFile: File{
				Name:               "file.txt",
				PathRelativeToRoot: "foo/bar/baz xer/",
				SizeInBytes:        1337,
				MTime:              time.Unix(1673815645, 797977200),
				HashType:           base.HashType{Hash: crypto.SHA512},
				Hash:               []byte{0xde, 0xad, 0xbe, 0xef},
			},
			wantErr: false,
		},
		{
			name:    "valid line with empty fields version 1",
			version: 1,
			line:    ",,sha512,deadbeef foo/bar/baz xer/file.txt",
			expectedFile: File{
				Name:               "file.txt",
				PathRelativeToRoot: "foo/bar/baz xer/",
				SizeInBytes:        0,
				MTime:              time.Time{},
				HashType:           base.HashType{Hash: crypto.SHA512},
				Hash:               []byte{0xde, 0xad, 0xbe, 0xef},
			},
			wantErr: false,
		},
		{
			name:         "invalid line version 1: missing hash type",
			version:      1,
			line:         "1673815645.7979772,1337,,deadbeef foo/bar/baz xer/file.txt",
			expectedFile: File{},
			wantErr:      true,
		},
		{
			name:         "invalid line version 1: missing hash",
			version:      1,
			line:         "1673815645.7979772,1337,sha512, foo/bar/baz xer/file.txt",
			expectedFile: File{},
			wantErr:      true,
		},
		{
			name:         "invalid line version 1: invalid missing space",
			version:      1,
			line:         "1673815645.7979772,1337,sha512,ffffffffabcd",
			expectedFile: File{},
			wantErr:      true,
		},
		{
			name:         "invalid line version 1: not enough fields",
			version:      1,
			line:         "1673815645.7979772,sha512,ffff foo/bar/baz xer/file.txt",
			expectedFile: File{},
			wantErr:      true,
		},
		{
			name:         "invalid line version 0: not enough fields",
			version:      0,
			line:         "sha512,ffff foo/bar/baz xer/file.txt",
			expectedFile: File{},
			wantErr:      true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			file, err := parseLine(tt.line, tt.version)
			if tt.wantErr {
				assertErr(t, err)
				return
			}
			assertNoErr(t, err)

			assertFilesEqual(t, []File{file}, []File{tt.expectedFile})
		})
	}
}

func TestParseMTime(t *testing.T) {
	tests := []struct {
		name          string
		input         string
		expectedMTime time.Time
		wantErr       bool
	}{
		{
			name:          "empty string",
			input:         "",
			expectedMTime: time.Time{},
			wantErr:       false,
		},
		{
			name:          "valid float",
			input:         "1673815645.7979772",
			expectedMTime: mTimeF64ToTime(1673815645.7979772),
			wantErr:       false,
		},
		{
			name:    "invalid float",
			input:   "not-a-number",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseMTime(tt.input)

			if tt.wantErr {
				assertErr(t, err)
				return
			}
			assertNoErr(t, err)

			if !got.Equal(tt.expectedMTime) {
				t.Fatalf("got %v, want %v", got, tt.expectedMTime)
			}
		})
	}
}

func TestParseSize(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected uint64
		wantErr  bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: 0,
			wantErr:  false,
		},
		{
			name:     "valid number",
			input:    "1673815645",
			expected: 1673815645,
			wantErr:  false,
		},
		{
			name:    "invalid number",
			input:   "not-a-number",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseSize(tt.input)

			if tt.wantErr {
				assertErr(t, err)
				return
			}
			assertNoErr(t, err)

			assertEqual(t, got, tt.expected)
		})
	}
}

func TestParseHashType(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected base.HashType
		wantErr  bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: base.HashType{},
			wantErr:  true,
		},
		{
			name:     "valid hash type",
			input:    "sha512",
			expected: base.HashType{Hash: crypto.SHA512},
			wantErr:  false,
		},
		{
			name:    "invalid hash type",
			input:   "foo",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseHashType(tt.input)

			if tt.wantErr {
				assertErr(t, err)
				return
			}
			assertNoErr(t, err)

			assertEqual(t, got, tt.expected)
		})
	}
}

func TestParseHash(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected []byte
		wantErr  bool
	}{
		{
			name:     "empty string",
			input:    "",
			expected: nil,
			wantErr:  true,
		},
		{
			name:     "valid hash",
			input:    "deadbeef",
			expected: []byte{0xde, 0xad, 0xbe, 0xef},
			wantErr:  false,
		},
		{
			name:    "invalid hash",
			input:   "deadbeefzz",
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseHash(tt.input)

			if tt.wantErr {
				assertErr(t, err)
				return
			}
			assertNoErr(t, err)

			assertSliceEqual(t, got, tt.expected)
		})
	}
}

func TestParseHeader(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected int
		wantErr  bool
	}{
		{
			name:     "missing",
			input:    "",
			expected: 0,
			wantErr:  false,
		},
		{
			name:     "only whitespace",
			input:    "     \t  ",
			expected: 0,
			wantErr:  false,
		},
		{
			name:     "version 1",
			input:    "# version 1",
			expected: 1,
			wantErr:  false,
		},
		{
			name:     "version 1 with extra whitespace",
			input:    "# version    \t 1",
			expected: 1,
			wantErr:  false,
		},
		{
			name:     "version invalid",
			input:    "# version foo",
			expected: 0,
			wantErr:  true,
		},
		{
			name:     "comment",
			input:    "# foo bar",
			expected: 0,
			wantErr:  false,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := parseHeader(tt.input)

			if tt.wantErr {
				assertErr(t, err)
				return
			}
			assertNoErr(t, err)

			assertEqual(t, got, tt.expected)
		})
	}
}
