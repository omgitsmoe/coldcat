# Search syntax

Search syntax is the default. All modes ignore case using the catalog's existing
Unicode simple folding; punctuation, original spelling, and Unicode normalization
behavior remain unchanged. **Name** searches the last `/`-separated segment;
**Path** searches the entire disk-relative path.

| Mode | Query | Meaning |
| --- | --- | --- |
| Search syntax | `gwa*cshd` | Ordered fragments anywhere in Name or Path |
| Search syntax | `gwa*cshd OR report*.pdf` | Either wildcard pattern, anywhere in the field |
| Search syntax | `(personal OR work) AND "tax return"` | Alternatives with a shared literal substring |
| Search syntax | `tax return` | One literal substring, including the space |
| Search syntax | `"report*.pdf"` | Literal substring containing an actual asterisk |
| Whole-field syntax | `gwa*cshd` | Entire field begins with `gwa` and ends with `cshd` |
| Whole-field syntax | `(personal/* OR work/*) AND *report*` | Whole-field patterns combined logically |
| Contains | `tax return` | Literal substring, including the space |
| Exact | `foo` | Entire selected field equals the literal |

Wildcard `*` matches zero or more characters, including `/`; `?` matches one Unicode
code point. Escape literal syntax with `\*`, `\?`, `\\`, `\"`, `\(`, and `\)`.
Brackets are literal, not character classes. Each term must include an uninterrupted
literal run of at least three Unicode code points. At most 32 terms and 32 indexed
literal runs across the entire query are allowed; every OR branch must be indexable.

Both syntax options use explicit uppercase `AND` and `OR`; `AND` binds more tightly.
Plain multiword text is one term, not implicit AND. Leading/trailing unquoted
whitespace is ignored around terms; quoted whitespace is preserved. Double quotes
make their contents literal, including operators, parentheses and wildcards.
Quoted and unquoted segments can be concatenated: `report*".pdf"` retains the wildcard.
Operators must be separated by whitespace or parentheses. At most 16 parenthesis
levels are allowed. NOT and regex are not supported. Invalid syntax returns an error,
not a literal-search fallback; choose Contains or Exact for uninterpreted input.

Default syntax matches each term anywhere, equivalent to adding `*` at both ends of
each pattern. Whole-field syntax anchors **each term**, not the expression as a whole.
For Path, `gwa*cshd` therefore matches `backups/gwa-2024.cshd` by default but not in
Whole-field syntax; use `*gwa*cshd` there to allow parent directories.

Contains requires three code points; Exact permits shorter queries. All queries are
limited to 1024 UTF-8 bytes. Invalid or unindexable queries return HTTP 400; there is
no hidden full-catalog scan fallback or candidate cap.

Syntax results have relevance `match` and sort by original binary
path, then observation ID. Contains still ranks exact matches before prefixes before
other substrings. Current/history scope, membership filters, catalog-wide replica
counts, and revision-bound pagination apply to every mode. Keep query, filters, and
page size unchanged when following cursors.

## API modes

Omitted `match` defaults to `syntax`; `whole` selects whole-field syntax.
`substring` and `exact` remain literal. Explicit legacy `wildcard` (anchored patterns,
no Boolean operators) and `expression` (Boolean literal substrings, no wildcard
patterns) remain available through the API, but are no longer browser options.
There is no legacy bookmark migration. New syntax cursors are separate from legacy
mode cursors. No schema migration, additional index or catalog rebuild is required.

## Performance measurements

### Unified syntax

Measured with the same distinct-path fixture and environment described below,
three iterations per first/next-page case, 50 items per page:

```sh
go test ./internal/database -run '^$' \
  -bench '^BenchmarkSearchSyntax$/50000/^(syntax_selective|syntax_broad|whole_combined)$/' \
  -benchtime=3x -benchmem -v -timeout=30m
```

Replace `/50000/` with `/1000000/` for the large fixture. Fixture construction is
untimed. These are warm serial means, not cold-cache latency or p95. Other checks,
including race tests, ran concurrently; do not infer precise speedups over the
historical measurements below.

| Query / mode | 50,000 paths: first / next | 1,000,000 paths: first / next |
| --- | ---: | ---: |
| `archive*0000005 OR personal*0000010` / syntax | 3.64 ms / no next page | 30.8 ms / no next page |
| `archive*txt OR personal*foo` / syntax | 62.3 / 63.5 ms | 1,249 / 1,297 ms |
| `(archive* OR personal*) AND *report*txt` / whole | 69.2 / 76.7 ms | 1,428 / 1,516 ms |

At one million paths, first-page allocations were about 29 KB for the selective
query and 127–129 KB for the broad queries. No storage or import-index changes were
needed. Broad queries remain potentially expensive despite indexed retrieval.

### Original separate modes

The following tables record the original separate wildcard/expression modes,
not measurements of unified syntax.

Measured on Linux/amd64, AMD Ryzen 5 9600X, Go's modernc SQLite driver.

The measurements below were acquired after rebasing onto the virtual-basename
optimization (`272325a`). The integrated implementation retains that optimization
for all modes. Select the distinct-path benchmark with an anchored name:

```sh
go test ./internal/database -run '^$' -bench '^BenchmarkSearchSyntax$/' \
  -benchtime=3x -benchmem -v -timeout=30m
```

The disposable fixture has one complete snapshot, unique content per path, and
50,000 or 1,000,000 distinct paths. Every tenth path is
`personal/é猫Σ/report-NNNNNNN-foo.txt`; others are `archive/report-NNNNNNN.txt`.
Fixture construction is untimed. These are warm serial means from three iterations,
not cold-cache timings, HTTP latency, p95, or import throughput. Other checks ran
concurrently, so small differences should not be interpreted as precise speedups.

| First-page query (50 items) | 50,000 paths | 1,000,000 paths |
| --- | ---: | ---: |
| Contains name `0000005` | 0.970 ms | 1.37 ms |
| Contains name `report` | 60.9 ms | 1,252 ms |
| Wildcard path `archive/*0000005*` | 2.62 ms | 30.0 ms |
| Wildcard path `personal*foo.txt` | 9.96 ms | 182 ms |
| Wildcard name `*report*txt` | 53.9 ms | 1,119 ms |
| Wildcard path `*é猫σ*foo*` | 7.66 ms | 134 ms |
| Expression path `archive AND 0000005` | 2.20 ms | 26.1 ms |
| Expression name `0000005 OR 0000006` | 1.47 ms | 1.78 ms |
| Expression path `archive OR personal` | 50.9 ms | 1,078 ms |

Second pages remain comparable: at one million paths, ordered wildcard took 186 ms,
broad leading wildcard 1,161 ms, and broad OR 1,121 ms. First-page allocations were
about 24–26 KB for selective new-mode queries and 124–127 KB for broad new-mode queries.

Verification-only comparisons count all matching paths, without observation joins or
pagination. At one million paths, explicit trigram retrieval plus GLOB verification
took 90.6 ms versus native FTS5 GLOB's 125 ms for `personal*foo.txt` (100,000 matches),
257 ms versus 769 ms for `*report*txt` (1,000,000 matches), and 0.651 ms versus 1.56 ms
for `*0000005*` (one match).

The existing index is reused without schema migration or added import writes. Broad
patterns still enumerate many candidates: indexed search is not a fixed-latency
guarantee. Prefer selective literal runs and membership filters when possible.

History-heavy measurements use a separate fixture:

```sh
go test ./internal/database -run '^$' -bench '^BenchmarkSearchSyntaxHistory$/50000/' \
  -benchtime=3x -benchmem -v -timeout=30m
```

This has 50,000 current observations and 250,000 total historical observations across
three disks and five snapshots per disk, mostly reusing paths. The final run also
measured 1,000,000 current / 5,000,000 historical observations. The original unanchored
`^BenchmarkSearchSyntax/` command selected both benchmark families; use the anchored
commands above to avoid unintended expensive fixture creation.

| Query / scope | 50,000 current: first / next | 1,000,000 current: first / next |
| --- | ---: | ---: |
| Wildcard `disk-01*report*txt` / current | 34.4 / 35.7 ms | 756 / 799 ms |
| Wildcard `disk-01*report*txt` / history | 22.6 / 23.1 ms | 517 / 533 ms |
| Expression `disk-01 AND report` / current | 33.2 / 34.3 ms | 733 / 770 ms |
| Expression `disk-01 AND report` / history | 21.4 / 22.5 ms | 490 / 502 ms |
| Contains `report` / current | 106 / 114 ms | 2,264 / 2,368 ms |
| Contains `report` / history | 62.8 / 64.3 ms | 1,276 / 1,315 ms |

Wildcard/AND queries above select one disk's path prefix; the broader substring
selects all three, so these are not equivalent selectivity comparisons. The rare
Unicode noncharacter verification path was correctness-tested, not benchmarked.
