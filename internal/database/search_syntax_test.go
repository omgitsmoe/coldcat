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
