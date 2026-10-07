package database

import (
	"database/sql/driver"
	"encoding/json"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/omgitsmoe/coldcat/internal/base"
	"modernc.org/sqlite"
)

func fuzzyFold(text string) string {
	return strings.Map(func(r rune) rune {
		canonical := unicode.ToLower(r)
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			canonical = min(canonical, unicode.ToLower(next))
		}
		return canonical
	}, text)
}

func fuzzyOne(left, right string) bool {
	a, b := []rune(left), []rune(right)
	if len(a) > len(b) {
		a, b = b, a
	}
	if len(b)-len(a) > 1 {
		return false
	}
	i := 0
	for i < len(a) && a[i] == b[i] {
		i++
	}
	if i == len(a) {
		return true
	}
	equal := func(a, b []rune) bool {
		if len(a) != len(b) {
			return false
		}
		for i := range a {
			if a[i] != b[i] {
				return false
			}
		}
		return true
	}
	if len(a) != len(b) {
		return equal(a[i:], b[i+1:])
	}
	return equal(a[i+1:], b[i+1:]) ||
		(i+1 < len(a) && a[i] == b[i+1] && a[i+1] == b[i] && equal(a[i+2:], b[i+2:]))
}

// Fixed-width rolling fingerprints avoid storing quadratic bytes for long paths.
// Collisions only add candidates: fuzzyOne always verifies the original folded term.
func fuzzySignatures(text string, swaps bool) []int64 {
	runes := []rune(text)
	const radix uint64 = 1000003
	powers, prefix := make([]uint64, len(runes)+1), make([]uint64, len(runes)+1)
	powers[0] = 1
	for i, r := range runes {
		powers[i+1] = powers[i] * radix
		prefix[i+1] = prefix[i]*radix + uint64(r) + 1
	}
	seen := make(map[int64]bool, len(runes)+1)
	values := make([]int64, 0, len(runes)+1)
	add := func(hash uint64) {
		key := int64(hash)
		if !seen[key] {
			seen[key] = true
			values = append(values, key)
		}
	}
	n := len(runes)
	add(prefix[n])
	for i := range runes {
		suffix := prefix[n] - prefix[i+1]*powers[n-i-1]
		add(prefix[i]*powers[n-i-1] + suffix)
		if swaps && i+1 < n {
			delta := uint64(runes[i+1]) - uint64(runes[i])
			add(prefix[n] + delta*(powers[n-i-1]-powers[n-i-2]))
		}
	}
	return values
}

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("coldcat_fold_v1", 1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			text, ok := args[0].(string)
			if !ok || !utf8.ValidString(text) {
				return nil, fmt.Errorf("invalid UTF-8 fuzzy term")
			}
			return fuzzyFold(text), nil
		})
	sqlite.MustRegisterDeterministicScalarFunction("coldcat_fuzzy_one_v1", 2,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			left, lok := args[0].(string)
			right, rok := args[1].(string)
			if !lok || !rok {
				return nil, fmt.Errorf("invalid fuzzy terms")
			}
			if fuzzyOne(left, right) {
				return int64(1), nil
			}
			return int64(0), nil
		})
	sqlite.MustRegisterDeterministicScalarFunction("coldcat_fuzzy_signatures_v1", 2,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			type posting struct {
				Field string `json:"field"`
				Key   int64  `json:"key"`
			}
			var values []posting
			for i, field := range []string{"name", "path"} {
				text, ok := args[i].(string)
				if !ok || !utf8.ValidString(text) {
					return nil, fmt.Errorf("invalid UTF-8 fuzzy term")
				}
				for _, key := range fuzzySignatures(text, false) {
					values = append(values, posting{field, key})
				}
			}
			data, err := json.Marshal(values)
			return string(data), err
		})
}

func fuzzyPaths(f base.SearchFilters) (string, []any) {
	query := fuzzyFold(f.Query)
	column := "p." + f.Field + "_fold"
	var literal string
	var args []any
	if utf8.RuneCountInString(query) < 3 {
		literal = `SELECT path_id AS id FROM search_short WHERE field=? AND gram=?
 UNION SELECT path_id AS id FROM search_fold_short WHERE field=? AND gram=?`
		args = []any{f.Field, query, f.Field, query}
	} else {
		literal = `SELECT rowid AS id FROM search_trigram WHERE search_trigram MATCH ?
 UNION SELECT rowid AS id FROM search_fold_trigram WHERE search_fold_trigram MATCH ?`
		phrase := `:"` + strings.ReplaceAll(query, `"`, `""`) + `"`
		args = []any{f.Field + phrase, f.Field + "_fold" + phrase}
	}
	keys, _ := json.Marshal(fuzzySignatures(query, true))
	source := `WITH candidate_ids AS (
 ` + literal + ` UNION
 SELECT g.path_id FROM json_each(?) k CROSS JOIN search_fuzzy_signature g
 ON g.field=? AND g.signature=k.value
 ) SELECT p.id,p.path,p.name,
 CASE WHEN ` + column + `=? THEN 0
 WHEN substr(` + column + `,1,length(?))=? THEN 1
 WHEN instr(` + column + `,?)>0 THEN 2 ELSE 3 END AS rank
 FROM candidate_ids c CROSS JOIN search_path p ON p.id=c.id
 WHERE instr(` + column + `,?)>0 OR coldcat_fuzzy_one_v1(` + column + `,?)=1`
	args = append(args, string(keys), f.Field, query, query, query, query, query, query)
	return source, args
}
