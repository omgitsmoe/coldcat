package database

import (
	"errors"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func TestSearchSyntaxMatches(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedSearchPaths(t, db, []string{
		"personal/bar/foo", "personal/foo/bar", "prefix/personal/bar/foo/suffix",
		"foo/bar/personal", "work/tax return", "personal/tax return",
		"personal/é猫Σ", "personal/É猫ς", "personal/a*b?c", "personal/[foo]",
		"personal/a\\b", "personal/abXbcYcd", "personal/abc", "personal/AND",
	})
	for _, test := range []struct {
		mode, field, query string
		want               []string
	}{
		{"wildcard", "path", "personal*foo", []string{"personal/bar/foo"}},
		{"wildcard", "path", "PERSONAL*FOO", []string{"personal/bar/foo"}},
		{"wildcard", "path", "*personal*foo*", []string{
			"personal/[foo]", "personal/bar/foo", "personal/foo/bar", "prefix/personal/bar/foo/suffix",
		}},
		{"wildcard", "path", "personal/???", []string{
			"personal/AND", "personal/a\\b", "personal/abc", "personal/É猫ς", "personal/é猫Σ",
		}},
		{"wildcard", "path", `personal/a\*b\?c`, []string{"personal/a*b?c"}},
		{"wildcard", "path", `personal/a\\b`, []string{"personal/a\\b"}},
		{"wildcard", "name", "[foo]", []string{"personal/[foo]"}},
		{"wildcard", "name", `a\*b\?c`, []string{"personal/a*b?c"}},
		{"wildcard", "name", "é猫σ", []string{"personal/É猫ς", "personal/é猫Σ"}},
		{"wildcard", "name", "*abcd*", nil},
		{"expression", "path", "personal AND foo", []string{
			"foo/bar/personal", "personal/[foo]", "personal/bar/foo", "personal/foo/bar",
			"prefix/personal/bar/foo/suffix",
		}},
		{"expression", "path", `(personal OR work) AND "tax return"`, []string{
			"personal/tax return", "work/tax return",
		}},
		{"expression", "path", `work OR personal AND abc`, []string{
			"personal/abc", "work/tax return",
		}},
		{"expression", "path", "personal AND personal", []string{
			"foo/bar/personal", "personal/AND", "personal/[foo]", "personal/a*b?c", "personal/a\\b",
			"personal/abXbcYcd", "personal/abc", "personal/bar/foo", "personal/foo/bar",
			"personal/tax return", "personal/É猫ς", "personal/é猫Σ", "prefix/personal/bar/foo/suffix",
		}},
		{"expression", "name", `"AND" OR "a*b?c"`, []string{"personal/AND", "personal/a*b?c"}},
		{"expression", "name", "abcd", nil},
		{"expression", "name", "missing OR foo", []string{
			"personal/[foo]", "personal/bar/foo",
		}},
	} {
		t.Run(test.mode+"/"+test.field+"/"+test.query, func(t *testing.T) {
			f := base.SearchFilters{Query: test.query, Field: test.field, Match: test.mode,
				ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
					ReplicaMetric: base.ReplicaDisks}}
			page, err := db.Search(t.Context(), f, 50, base.SearchAnchor{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, item := range page.Items {
				got = append(got, item.Observation.Path)
				if item.Relevance != "match" {
					t.Fatalf("unexpected relevance: %s", item.Relevance)
				}
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("got %q want %q", got, test.want)
			}
			verifySearchWindowPages(t, db, f)
			query, args, err := searchWindow(f, 50, base.SearchAnchor{})
			if err != nil {
				t.Fatal(err)
			}
			if plan := searchWindowPlan(t, db, query, args); !strings.Contains(plan, "VIRTUAL TABLE INDEX 0:M") {
				t.Fatalf("missing indexed MATCH constraint:\n%s", plan)
			}
			if len(page.Items) > 0 {
				first := page.Items[0]
				after := base.SearchAnchor{ID: first.Observation.Id,
					Path: first.Observation.Path, Relevance: "match"}
				query, args, err := searchWindow(f, 0, after)
				if err != nil {
					t.Fatal(err)
				}
				if plan := searchWindowPlan(t, db, query, args); strings.Contains(plan, "VIRTUAL TABLE INDEX") {
					t.Fatalf("anchor used postings:\n%s", plan)
				}
				after.Relevance = "exact"
				if _, err := db.Search(t.Context(), f, 1, after, nil); !errors.Is(err, ErrValidation) {
					t.Fatalf("accepted wrong anchor rank: %v", err)
				}
			}
		})
	}
}

func TestSearchWildcardPreservesUnicodeNoncharacters(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedSearchPaths(t, db, []string{"abc\ufffd", "abc\ufffe", "abc\uffff", "abc/\ufffe*?"})
	for _, test := range []struct {
		query string
		want  []string
	}{
		{"abc*\ufffd", []string{"abc\ufffd"}},
		{"abc*\ufffe", []string{"abc\ufffe"}},
		{"abc*\uffff", []string{"abc\uffff"}},
		{"abc?", []string{"abc\ufffd", "abc\ufffe", "abc\uffff"}},
		{"abc/\ufffe\\*\\?", []string{"abc/\ufffe*?"}},
	} {
		f := base.SearchFilters{Query: test.query, Field: "path", Match: "wildcard",
			ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
				ReplicaMetric: base.ReplicaDisks}}
		page, err := db.Search(t.Context(), f, 50, base.SearchAnchor{}, nil)
		if err != nil {
			t.Fatal(err)
		}
		var got []string
		for _, item := range page.Items {
			got = append(got, item.Observation.Path)
		}
		if !slices.Equal(got, test.want) {
			t.Errorf("%q: got %q want %q", test.query, got, test.want)
		}
		verifySearchWindowPages(t, db, f)
	}
}

func TestSearchWildcardVerifier(t *testing.T) {
	for _, test := range []struct {
		text, pattern string
		want          bool
	}{
		{"", "*", true}, {"abc", "abc*", true}, {"abc", "abc?", false},
		{"abc/foo/bar", "abc*foo*bar", true}, {"abc/foo/bar", "abc*bar*foo", false},
		{"abcfoo", "abc*foo*foo", false}, {"abcfoofoo", "abc*foo*foo", true},
		{"abc猫", "abc?", true}, {"abc\ufffe", "abc\ufffd", false},
		{"abc\ufffe", "abc?", true}, {"abc/\ufffe*?", "abc/\ufffe\\*\\?", true},
		{"a\\b", `a\\b`, true}, {"[foo]", "[foo]", true},
	} {
		got, err := searchWildcardMatches(test.text, test.pattern)
		if err != nil || got != test.want {
			t.Errorf("%q %q: got %t want %t: %v", test.text, test.pattern, got, test.want, err)
		}
	}
}

func TestSearchSyntaxValidation(t *testing.T) {
	for _, test := range []struct{ mode, query string }{
		{"wildcard", "*"}, {"wildcard", "a*b?c"}, {"wildcard", `abc\`},
		{"wildcard", `abc\x`}, {"wildcard", strings.Repeat("abc*", 33)},
		{"expression", "abc def"}, {"expression", "abc AND"}, {"expression", "OR abc"},
		{"expression", "abc NOT def"}, {"expression", "abc and def"},
		{"expression", "abc OR ab"}, {"expression", "()"}, {"expression", "(abc"},
		{"expression", "abc)"}, {"expression", `"abc`}, {"expression", `""`},
		{"expression", `"abc\q"`}, {"expression", "abc*"},
		{"expression", strings.Repeat("(", 17) + "abc" + strings.Repeat(")", 17)},
		{"expression", strings.Repeat("abc OR ", 32) + "def"},
	} {
		f := base.SearchFilters{Query: test.query, Field: "path", Match: test.mode,
			ContentFilters: base.ContentFilters{Scope: base.ScopeCurrent,
				ReplicaMetric: base.ReplicaDisks}}
		if err := ValidateSearchFilters(f); !errors.Is(err, ErrValidation) {
			t.Errorf("accepted %s %q: %v", test.mode, test.query, err)
		}
	}
	for _, query := range []string{
		strings.Repeat("(", 16) + "abc" + strings.Repeat(")", 16),
		strings.Repeat("abc OR ", 31) + "def", `"a\"b" AND "a\\b"`,
	} {
		if _, err := parseSearchExpression(query); err != nil {
			t.Errorf("rejected valid boundary %q: %v", query, err)
		}
	}
}

func TestSearchUnifiedSyntax(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()
	seedSearchPaths(t, db, []string{
		"backups/gwa-2024-09-27_2024-09-27.cshd", "gwa-2024.cshd",
		"prefix/gwa-2024.cshd.suffix", "work/report-2024.pdf", "work/report*.pdf",
		"work/tax return", "work/tax other return", "work/AND", "work/a(b)c",
		"work/a\"b", "work/a\\b", "work/é猫Σ.txt", "work/abc\ufffe", "work/abc\ufffd",
	})
	for _, test := range []struct {
		mode, field, query string
		want               []string
	}{
		{"syntax", "name", "gwa*cshd", []string{
			"backups/gwa-2024-09-27_2024-09-27.cshd", "gwa-2024.cshd", "prefix/gwa-2024.cshd.suffix",
		}},
		{"syntax", "path", "GWA*CSHD", []string{
			"backups/gwa-2024-09-27_2024-09-27.cshd", "gwa-2024.cshd", "prefix/gwa-2024.cshd.suffix",
		}},
		{"whole", "path", "gwa*cshd", []string{"gwa-2024.cshd"}},
		{"whole", "name", "gwa*cshd", []string{
			"backups/gwa-2024-09-27_2024-09-27.cshd", "gwa-2024.cshd",
		}},
		{"syntax", "path", "(gwa*cshd OR report*.pdf) AND 2024", []string{
			"backups/gwa-2024-09-27_2024-09-27.cshd", "gwa-2024.cshd",
			"prefix/gwa-2024.cshd.suffix", "work/report-2024.pdf",
		}},
		{"whole", "path", "(backups/* OR work/*) AND *2024*", []string{
			"backups/gwa-2024-09-27_2024-09-27.cshd", "work/report-2024.pdf",
		}},
		{"syntax", "name", "missing OR tax AND return", []string{
			"work/tax other return", "work/tax return",
		}},
		{"syntax", "name", "tax return", []string{"work/tax other return", "work/tax return"}},
		{"syntax", "name", "return tax", []string{"work/tax other return", "work/tax return"}},
		{"syntax", "name", "tax\t\n\u2003return", []string{
			"work/tax other return", "work/tax return",
		}},
		{"syntax", "name", "missing OR tax return", []string{
			"work/tax other return", "work/tax return",
		}},
		{"syntax", "name", "tax return OR report*.pdf", []string{
			"work/report*.pdf", "work/report-2024.pdf", "work/tax other return", "work/tax return",
		}},
		{"syntax", "name", "tax (missing OR return)", []string{
			"work/tax other return", "work/tax return",
		}},
		{"syntax", "name", "(tax)(return)", []string{
			"work/tax other return", "work/tax return",
		}},
		{"syntax", "name", `"tax" "return"`, []string{
			"work/tax other return", "work/tax return",
		}},
		{"syntax", "name", `"tax "return`, []string{"work/tax return"}},
		{"syntax", "name", `"tax other" return`, []string{"work/tax other return"}},
		{"syntax", "name", "report* pdf", []string{"work/report*.pdf", "work/report-2024.pdf"}},
		{"syntax", "name", `"tax return"`, []string{"work/tax return"}},
		{"whole", "path", "tax return", nil},
		{"whole", "name", "tax return", nil},
		{"whole", "name", `"tax return"`, []string{"work/tax return"}},
		{"whole", "name", "*tax* *return*", []string{
			"work/tax other return", "work/tax return",
		}},
		{"substring", "name", "tax return", []string{"work/tax return"}},
		{"syntax", "name", `"report*.pdf"`, []string{"work/report*.pdf"}},
		{"syntax", "name", `report\*.pdf`, []string{"work/report*.pdf"}},
		{"syntax", "name", `report*".pdf"`, []string{"work/report*.pdf", "work/report-2024.pdf"}},
		{"syntax", "path", `"AND"`, []string{"work/AND"}},
		{"syntax", "name", `a\(b\)c`, []string{"work/a(b)c"}},
		{"syntax", "name", `"a(b)c"`, []string{"work/a(b)c"}},
		{"syntax", "name", `"a\"b"`, []string{"work/a\"b"}},
		{"syntax", "name", `a\\b`, []string{"work/a\\b"}},
		{"syntax", "name", `é猫σ?txt`, []string{"work/é猫Σ.txt"}},
		{"syntax", "path", "abc\ufffe OR absent", []string{"work/abc\ufffe"}},
		{"syntax", "path", "abc\ufffd", []string{"work/abc\ufffd"}},
		{"syntax", "path", "  tax return  ", []string{
			"work/tax other return", "work/tax return",
		}},
		{"syntax", "path", "missing OR(tax AND return)", []string{
			"work/tax other return", "work/tax return",
		}},
	} {
		t.Run(test.mode+"/"+test.field+"/"+test.query, func(t *testing.T) {
			f := base.SearchFilters{Query: test.query, Field: test.field, Match: test.mode,
				ContentFilters: base.ContentFilters{
					Scope: base.ScopeCurrent, ReplicaMetric: base.ReplicaDisks,
				}}
			page, err := db.Search(t.Context(), f, 50, base.SearchAnchor{}, nil)
			if err != nil {
				t.Fatal(err)
			}
			var got []string
			for _, item := range page.Items {
				got = append(got, item.Observation.Path)
				if test.mode != "substring" && item.Relevance != "match" {
					t.Fatalf("unexpected relevance %q", item.Relevance)
				}
			}
			if !slices.Equal(got, test.want) {
				t.Fatalf("got %q want %q", got, test.want)
			}
			verifySearchWindowPages(t, db, f)
			query, args, err := searchWindow(f, 50, base.SearchAnchor{})
			if err != nil {
				t.Fatal(err)
			}
			if plan := searchWindowPlan(t, db, query, args); !strings.Contains(plan, "VIRTUAL TABLE INDEX 0:M") {
				t.Fatalf("missing indexed MATCH constraint:\n%s", plan)
			}
			if test.mode != "substring" && len(page.Items) > 0 {
				first := page.Items[0]
				after := base.SearchAnchor{ID: first.Observation.Id,
					Path: first.Observation.Path, Relevance: "exact"}
				if _, err := db.Search(t.Context(), f, 1, after, nil); !errors.Is(err, ErrValidation) {
					t.Fatalf("accepted wrong anchor rank: %v", err)
				}
			}
		})
	}
}

func TestSearchUnifiedSyntaxValidation(t *testing.T) {
	for _, mode := range []string{"syntax", "whole"} {
		for _, query := range []string{
			"*", "abc OR a*b", "abc AND ??", "abc AND", "OR abc", "abc NOT def",
			"abc OR", "(abc", "abc)", "()", `"abc`, `""`, `abc\`, `abc\x`,
			strings.Repeat("(", 17) + "abc" + strings.Repeat(")", 17),
			strings.Repeat("abc OR ", 32) + "def",
			strings.Repeat("abc ", 32) + "def", "abc ab", "abc AND OR def",
			"abc (def OR)", "abc ()", "abc NOT", "abc OR NOT def",
			strings.Repeat("abc*", 17) + " OR " + strings.Repeat("def*", 16),
		} {
			f := base.SearchFilters{Query: query, Field: "path", Match: mode,
				ContentFilters: base.ContentFilters{
					Scope: base.ScopeCurrent, ReplicaMetric: base.ReplicaDisks,
				}}
			if err := ValidateSearchFilters(f); !errors.Is(err, ErrValidation) {
				t.Errorf("accepted %s %q: %v", mode, query, err)
			}
		}
		for _, query := range []string{
			strings.Repeat("(", 16) + "abc" + strings.Repeat(")", 16),
			strings.Repeat("abc OR ", 31) + "def", `"NOT"`, `abc?`,
			strings.Repeat("abc ", 31) + "def", `"a quote should be used"`,
		} {
			if _, err := parseSearchSyntax(query, mode == "whole"); err != nil {
				t.Errorf("rejected %s %q: %v", mode, query, err)
			}
		}
	}
}
