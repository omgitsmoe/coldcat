package database

import (
	"database/sql/driver"
	"fmt"
	"strings"
	"unicode"
	"unicode/utf8"

	"modernc.org/sqlite"
)

func init() {
	sqlite.MustRegisterDeterministicScalarFunction("coldcat_wildcard_v1", 2,
		func(_ *sqlite.FunctionContext, args []driver.Value) (driver.Value, error) {
			text, textOK := args[0].(string)
			pattern, patternOK := args[1].(string)
			if !textOK || !patternOK {
				return nil, fmt.Errorf("wildcard verification requires text arguments")
			}
			matched, err := searchWildcardMatches(text, pattern)
			if err != nil {
				return nil, err
			}
			if matched {
				return int64(1), nil
			}
			return int64(0), nil
		})
}

func searchWildcardPredicate(column, query, pattern string) (string, []any) {
	if strings.ContainsAny(query, "\ufffe\uffff") {
		return "coldcat_wildcard_v1(" + column + ",?)", []any{searchFold(query)}
	}
	if !strings.ContainsRune(query, '\ufffd') {
		return column + " GLOB ?", []any{pattern}
	}
	// SQLite's GLOB decoder maps U+FFFE/U+FFFF to U+FFFD. Keep its fast path
	// for ordinary text, but verify noncharacters without that substitution.
	return "CASE WHEN instr(" + column + ",?)>0 OR instr(" + column + ",?)>0" +
			" THEN coldcat_wildcard_v1(" + column + ",?) ELSE " + column + " GLOB ? END",
		[]any{"\ufffe", "\uffff", searchFold(query), pattern}
}

type wildcardRune struct {
	literal rune
	op      rune
}

func searchWildcardMatches(text, pattern string) (bool, error) {
	var tokens []wildcardRune
	runes := []rune(pattern)
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		switch r {
		case '\\':
			i++
			if i == len(runes) || !strings.ContainsRune(`*?\`, runes[i]) {
				return false, fmt.Errorf("invalid wildcard escape")
			}
			tokens = append(tokens, wildcardRune{literal: runes[i]})
		case '*', '?':
			tokens = append(tokens, wildcardRune{op: r})
		default:
			tokens = append(tokens, wildcardRune{literal: r})
		}
	}
	input := []rune(text)
	i, j, star, retry := 0, 0, -1, 0
	for i < len(input) {
		if j < len(tokens) && tokens[j].op == '*' {
			star, retry = j, i
			j++
		} else if j < len(tokens) &&
			(tokens[j].op == '?' || tokens[j].op == 0 && tokens[j].literal == input[i]) {
			i++
			j++
		} else if star >= 0 {
			retry++
			i, j = retry, star+1
		} else {
			return false, nil
		}
	}
	for j < len(tokens) && tokens[j].op == '*' {
		j++
	}
	return j == len(tokens), nil
}

const (
	searchMaxTerms = 32
	searchMaxDepth = 16
)

type searchExpression struct {
	literal   string
	wildcard  string
	glob      string
	fragments []string
	op        string
	left      *searchExpression
	right     *searchExpression
}

func (e *searchExpression) compile(field string) (string, string, []any) {
	if e.op == "" {
		if e.wildcard != "" {
			terms := make([]string, 0, len(e.fragments))
			for _, fragment := range e.fragments {
				terms = append(terms, "("+searchTrigramMatch(field, fragment)+")")
			}
			predicate, args := searchWildcardPredicate("p."+field+"_fold", e.wildcard, e.glob)
			return "(" + strings.Join(terms, " AND ") + ")", predicate, args
		}
		return searchTrigramMatch(field, e.literal),
			"instr(p." + field + "_fold,?)>0", []any{e.literal}
	}
	lm, lp, la := e.left.compile(field)
	rm, rp, ra := e.right.compile(field)
	return "(" + lm + " " + e.op + " " + rm + ")",
		"(" + lp + " " + e.op + " " + rp + ")", append(la, ra...)
}

type searchParser struct {
	input       []rune
	pos         int
	terms       int
	indexedRuns int
	unified     bool
	wholeField  bool
}

func parseSearchSyntax(query string, wholeField bool) (*searchExpression, error) {
	p := searchParser{input: []rune(query), unified: true, wholeField: wholeField}
	return p.parse()
}

func parseSearchExpression(query string) (*searchExpression, error) {
	p := searchParser{input: []rune(query)}
	return p.parse()
}

func (p *searchParser) parse() (*searchExpression, error) {
	e, err := p.expression(0, 0)
	if err != nil {
		return nil, err
	}
	p.space()
	if p.pos != len(p.input) {
		return nil, fmt.Errorf("expected AND or OR at character %d", p.pos+1)
	}
	return e, nil
}

func (p *searchParser) space() {
	for p.pos < len(p.input) && unicode.IsSpace(p.input[p.pos]) {
		p.pos++
	}
}

func (p *searchParser) operator() (string, int) {
	p.space()
	start := p.pos
	for p.pos < len(p.input) && !unicode.IsSpace(p.input[p.pos]) &&
		p.input[p.pos] != '(' && p.input[p.pos] != ')' {
		p.pos++
	}
	op := string(p.input[start:p.pos])
	p.pos = start
	switch op {
	case "OR":
		return op, 1
	case "AND":
		return op, 2
	default:
		return "", 0
	}
}

func (p *searchParser) expression(minPrecedence, depth int) (*searchExpression, error) {
	if depth > searchMaxDepth {
		return nil, fmt.Errorf("expression nesting exceeds %d", searchMaxDepth)
	}
	left, err := p.atom(depth)
	if err != nil {
		return nil, err
	}
	for {
		op, precedence := p.operator()
		if op == "" || precedence < minPrecedence {
			return left, nil
		}
		p.pos += len(op)
		right, err := p.expression(precedence+1, depth)
		if err != nil {
			return nil, err
		}
		left = &searchExpression{op: op, left: left, right: right}
	}
}

func (p *searchParser) atom(depth int) (*searchExpression, error) {
	p.space()
	if p.pos == len(p.input) {
		return nil, fmt.Errorf("expected a literal or parenthesized expression")
	}
	if p.input[p.pos] == '(' {
		p.pos++
		e, err := p.expression(0, depth+1)
		if err != nil {
			return nil, err
		}
		p.space()
		if p.pos == len(p.input) || p.input[p.pos] != ')' {
			return nil, fmt.Errorf("missing closing parenthesis")
		}
		p.pos++
		return e, nil
	}
	if p.unified {
		return p.pattern()
	}
	var literal strings.Builder
	quoted := p.input[p.pos] == '"'
	if quoted {
		p.pos++
	}
	closed := !quoted
	for p.pos < len(p.input) {
		r := p.input[p.pos]
		if quoted && r == '"' {
			p.pos++
			closed = true
			break
		}
		if !quoted && (unicode.IsSpace(r) || r == '(' || r == ')') {
			break
		}
		if !quoted && (r == '"' || r == '*' || r == '?') {
			return nil, fmt.Errorf("quote literal quotes and wildcard characters in expressions")
		}
		if r == '\\' {
			p.pos++
			if p.pos == len(p.input) ||
				(p.input[p.pos] != '\\' && p.input[p.pos] != '"') {
				return nil, fmt.Errorf("only backslash and double quote may be escaped")
			}
			r = p.input[p.pos]
		}
		literal.WriteRune(r)
		p.pos++
	}
	text := literal.String()
	if !closed {
		return nil, fmt.Errorf("unterminated quoted literal")
	}
	if !quoted && (text == "AND" || text == "OR" || text == "NOT") {
		return nil, fmt.Errorf("expected a literal; quote operator names to search for them")
	}
	if utf8.RuneCountInString(text) < 3 {
		return nil, fmt.Errorf("each expression literal requires at least three characters")
	}
	p.terms++
	if p.terms > searchMaxTerms {
		return nil, fmt.Errorf("expression exceeds %d literals", searchMaxTerms)
	}
	return &searchExpression{literal: searchFold(text)}, nil
}

func (p *searchParser) pattern() (*searchExpression, error) {
	var pattern, pendingSpace strings.Builder
	quoted := false
	started := false
	writeLiteral := func(r rune) {
		if strings.ContainsRune(`*?\`, r) {
			pattern.WriteRune('\\')
		}
		pattern.WriteRune(r)
	}
	for p.pos < len(p.input) {
		r := p.input[p.pos]
		if !quoted {
			if r == '(' || r == ')' {
				break
			}
			if unicode.IsSpace(r) {
				pendingSpace.WriteRune(r)
				p.pos++
				continue
			}
			if !started || pendingSpace.Len() > 0 {
				op, _ := p.operator()
				if op != "" {
					break
				}
				end := p.pos
				for end < len(p.input) && !unicode.IsSpace(p.input[end]) &&
					p.input[end] != '(' && p.input[end] != ')' {
					end++
				}
				if string(p.input[p.pos:end]) == "NOT" {
					return nil, fmt.Errorf("NOT is unsupported; quote operator names to search for them")
				}
			}
		}
		pattern.WriteString(pendingSpace.String())
		pendingSpace.Reset()
		started = true
		p.pos++
		if r == '"' {
			quoted = !quoted
			continue
		}
		if r == '\\' {
			if p.pos == len(p.input) || !strings.ContainsRune(`*?\"()`, p.input[p.pos]) {
				return nil, fmt.Errorf(`syntax escapes must be \*, \?, \\, \", \( or \)`)
			}
			writeLiteral(p.input[p.pos])
			p.pos++
		} else if quoted {
			writeLiteral(r)
		} else {
			pattern.WriteRune(r)
		}
	}
	if quoted {
		return nil, fmt.Errorf("unterminated quoted literal")
	}
	if pattern.Len() == 0 {
		return nil, fmt.Errorf("expected a literal or wildcard pattern; quote operator names")
	}
	query := pattern.String()
	if !p.wholeField {
		query = "*" + query + "*"
	}
	glob, fragments, err := searchWildcard(query)
	if err != nil {
		return nil, err
	}
	p.terms++
	p.indexedRuns += len(fragments)
	if p.terms > searchMaxTerms || p.indexedRuns > searchMaxTerms {
		return nil, fmt.Errorf("syntax exceeds %d terms or indexed literal runs", searchMaxTerms)
	}
	return &searchExpression{wildcard: query, glob: glob, fragments: fragments}, nil
}

// SQLite GLOB has no escape clause. Encode escaped metacharacters as singleton
// classes, keeping user character classes out of the public wildcard syntax.
func searchGlob(query string) (string, error) {
	pattern, _, err := searchWildcard(query)
	return pattern, err
}

func searchWildcard(query string) (string, []string, error) {
	runes := []rune(query)
	var pattern strings.Builder
	var literal strings.Builder
	var fragments []string
	flush := func() {
		if utf8.RuneCountInString(literal.String()) >= 3 {
			fragments = append(fragments, searchFold(literal.String()))
		}
		literal.Reset()
	}
	for i := 0; i < len(runes); i++ {
		r := runes[i]
		wild := r == '*' || r == '?'
		if r == '\\' {
			i++
			if i == len(runes) || !strings.ContainsRune(`*?\`, runes[i]) {
				return "", nil, fmt.Errorf("wildcard escapes must be \\*, \\? or \\\\")
			}
			r, wild = runes[i], false
		}
		if wild {
			pattern.WriteRune(r)
			flush()
			continue
		}
		literal.WriteRune(r)
		switch r {
		case '*', '?', '[':
			pattern.WriteRune('[')
			pattern.WriteRune(r)
			pattern.WriteRune(']')
		default:
			pattern.WriteString(searchFold(string(r)))
		}
	}
	flush()
	if len(fragments) == 0 {
		return "", nil, fmt.Errorf("wildcard search requires a literal run of at least three characters")
	}
	if len(fragments) > searchMaxTerms {
		return "", nil, fmt.Errorf("wildcard search exceeds %d indexed literal runs", searchMaxTerms)
	}
	return pattern.String(), fragments, nil
}
