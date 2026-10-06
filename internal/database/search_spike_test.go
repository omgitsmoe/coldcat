package database

import (
	"fmt"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
	"unicode"
)

func TestSearchTrigramCapabilities(t *testing.T) {
	db, err := Open(filepath.Join(t.TempDir(), "catalog.sqlite"))
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.db.Exec(`CREATE VIRTUAL TABLE spike USING fts5(
 name, tokenize='trigram case_sensitive 1');
 INSERT INTO spike(name) VALUES('report.txt'),('é猫%_\\file'),('ab');`)
	if err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		query string
		want  int
	}{
		{`"report"`, 1}, {`"Report"`, 0}, {`"é猫%"`, 1},
		{`"%_\"`, 1}, {`"ab"`, 0}, {`"repotr"`, 0},
	} {
		var count int
		if err := db.db.QueryRow(`SELECT count(*) FROM spike WHERE spike MATCH ?`,
			test.query).Scan(&count); err != nil {
			t.Fatal(err)
		}
		if count != test.want {
			t.Fatalf("%s: got %d, want %d", test.query, count, test.want)
		}
	}
}

type deletionLexicon struct {
	terms      map[string]bool
	signatures map[string][]string
}

func spikeFold(text string) string {
	return strings.Map(func(r rune) rune {
		canonical := unicode.ToLower(r)
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			canonical = min(canonical, unicode.ToLower(next))
		}
		return canonical
	}, text)
}

func deleteOne(text string) []string {
	runes := []rune(text)
	values := []string{text}
	for i := range runes {
		values = append(values, string(runes[:i])+string(runes[i+1:]))
	}
	return values
}

func newDeletionLexicon(terms []string) deletionLexicon {
	index := deletionLexicon{terms: map[string]bool{}, signatures: map[string][]string{}}
	for _, original := range terms {
		term := spikeFold(original)
		if index.terms[term] {
			continue
		}
		index.terms[term] = true
		for _, signature := range deleteOne(term) {
			index.signatures[signature] = append(index.signatures[signature], term)
		}
	}
	return index
}

func (index deletionLexicon) candidates(query string) []string {
	query = spikeFold(query)
	seen := map[string]bool{}
	for _, signature := range deleteOne(query) {
		for _, term := range index.signatures[signature] {
			seen[term] = true
		}
	}
	runes := []rune(query)
	for i := 0; i+1 < len(runes); i++ {
		runes[i], runes[i+1] = runes[i+1], runes[i]
		if term := string(runes); index.terms[term] {
			seen[term] = true
		}
		runes[i], runes[i+1] = runes[i+1], runes[i]
	}
	var result []string
	for term := range seen {
		result = append(result, term)
	}
	slices.Sort(result)
	return result
}

func TestSearchFuzzyDeletionRecallSpike(t *testing.T) {
	terms := []string{"a", "ab", "abc", "report.txt", "é猫%_\\file", "a-b.txt", "Σ.txt"}
	index := newDeletionLexicon(terms)
	for _, term := range terms {
		runes := []rune(term)
		queries := deleteOne(term)
		for i := range runes {
			queries = append(queries, string(runes[:i])+"?"+string(runes[i+1:]))
			queries = append(queries, string(runes[:i])+"?"+string(runes[i:]))
			if i+1 < len(runes) {
				runes[i], runes[i+1] = runes[i+1], runes[i]
				queries = append(queries, string(runes))
				runes[i], runes[i+1] = runes[i+1], runes[i]
			}
		}
		queries = append(queries, term+"?", strings.ToUpper(term))
		for _, query := range queries {
			if query == "" {
				continue
			}
			if !slices.Contains(index.candidates(query), spikeFold(term)) {
				t.Fatalf("missed %q for %q", term, query)
			}
			if spikeDistance(spikeFold(query), spikeFold(term)) > 1 {
				t.Fatalf("reranking lost %q for %q", term, query)
			}
		}
	}
}

func spikeDistance(a, b string) int {
	left, right := []rune(a), []rune(b)
	rows := make([][]int, len(left)+1)
	for i := range rows {
		rows[i] = make([]int, len(right)+1)
		rows[i][0] = i
	}
	for j := range rows[0] {
		rows[0][j] = j
	}
	for i := 1; i <= len(left); i++ {
		for j := 1; j <= len(right); j++ {
			cost := 1
			if left[i-1] == right[j-1] {
				cost = 0
			}
			rows[i][j] = min(rows[i-1][j]+1, rows[i][j-1]+1, rows[i-1][j-1]+cost)
			if i > 1 && j > 1 && left[i-1] == right[j-2] && left[i-2] == right[j-1] {
				rows[i][j] = min(rows[i][j], rows[i-2][j-2]+1)
			}
		}
	}
	return rows[len(left)][len(right)]
}

func BenchmarkSearchFuzzyDeletionSpike(b *testing.B) {
	terms := make([]string, 50000)
	for i := range terms {
		terms[i] = fmt.Sprintf("report-%05d.txt", i)
	}
	var before, after runtime.MemStats
	runtime.ReadMemStats(&before)
	started := time.Now()
	index := newDeletionLexicon(append(terms, "a", "ab", "abc"))
	runtime.ReadMemStats(&after)
	b.Logf("prototype: %d terms, %d signatures, built in %s, %d allocated bytes",
		len(index.terms), len(index.signatures), time.Since(started), after.TotalAlloc-before.TotalAlloc)
	for _, query := range []string{"reprot-00005.txt", "repor-00005.txt", "ac", "xy"} {
		b.Run(query, func(b *testing.B) {
			b.ReportAllocs()
			for b.Loop() {
				candidates := index.candidates(query)
				retained := 0
				for _, term := range candidates {
					if spikeDistance(spikeFold(query), term) <= 1 {
						retained++
					}
				}
				b.ReportMetric(float64(len(candidates)), "candidates/op")
				b.ReportMetric(float64(retained), "matches/op")
			}
		})
	}
}
