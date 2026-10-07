package database

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"modernc.org/sqlite"
)

func searchFold(text string) string {
	return strings.Map(func(r rune) rune {
		canonical := unicode.ToLower(r)
		for next := unicode.SimpleFold(r); next != r; next = unicode.SimpleFold(next) {
			canonical = min(canonical, unicode.ToLower(next))
		}
		return canonical
	}, text)
}

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("coldcat_fold_v1", 1,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			text, ok := args[0].(string)
			if !ok || !utf8.ValidString(text) {
				return nil, fmt.Errorf("invalid UTF-8 search term")
			}
			return searchFold(text), nil
		})
}
