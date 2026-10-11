# ADR 0003: Indexed wildcard and Boolean substring search

- Status: Superseded for browser options and default syntax by ADR 0004;
  explicit legacy API modes remain supported.
- Date: 2026-10-11

## Context

Remembered paths often consist of separated fragments, such as `personal*foo`
matching `personal/bar/foo`, or alternatives rather than a single literal substring.
The existing folded FTS5 trigram index records column membership without positions.
FTS5 MATCH prefix syntax does not express arbitrary character gaps. Native indexed
GLOB can express them, but escaped metacharacters encoded as singleton classes can
break the literal runs used by its optimizer.

## Decision

Keep exact and substring modes unchanged. Add explicit wildcard and expression modes
to the existing API and browser UI, with mode-specific syntax hints.

Wildcard matches the entire selected field. `*` spans zero or more characters,
including `/`; `?` spans one Unicode code point. Backslash escapes `*`, `?`, or
backslash. Brackets remain literal. Extract contiguous literal runs before folding,
intersect their trigram postings, and verify the complete folded pattern with GLOB.
SQLite's GLOB decoder maps U+FFFE/U+FFFF to U+FFFD. Queries containing those
noncharacters use a deterministic rune-based verifier. Queries containing literal
U+FFFD also use that verifier when the candidate contains a noncharacter.
Translate escaped GLOB metacharacters to singleton classes for verification only.
Require at least one literal run of three code points and at most 32 indexed runs.

Expressions combine literal substrings with explicit uppercase AND/OR, parentheses,
and double-quoted literals. AND binds before OR; there is no implicit operator.
Backslash escapes double quote or backslash. Require three code points per literal,
at most 32 literals, and 16 parenthesis levels. Compile the same expression tree into
an FTS5 candidate expression and full `instr` verification predicates. Never use
trigram membership alone as evidence of a complete literal or ordered match.

All modes retain version-1 Unicode simple folding, complete-snapshot visibility,
scope/membership filters, catalog-wide replica counts, and exhaustive retrieval.
Wildcard and expression results use constant relevance `match`, ordered by original
binary path and observation ID. Their cursors use a separate syntax/order version;
existing exact/substring cursors retain their format. Verify matches before applying
the existing path-window pagination optimization, including direct anchor validation.

## Alternatives considered

- Raw FTS5 syntax: incompatible with arbitrary gaps and the column-only index's phrase
  restrictions; exposes tokenizer semantics instead of literal path semantics.
- Native FTS5 GLOB retrieval: useful comparison, but escaped literals may lose the
  useful indexed constraints. Explicit candidate compilation guarantees the required
  literal grams are used, including escaped wildcard characters.
- Full positional FTS index: unnecessary storage and import cost for these semantics.
- Regex: requires safe mandatory-literal extraction and still admits unindexable
  patterns. Defer rather than silently scan the catalog.
- NOT or short literals: complicate safe candidate selection; defer initially.

## Consequences

No schema migration, additional index, or catalog rebuild is required. Existing
literal queries do not acquire syntax accidentally. Unindexable or malformed queries
fail validation instead of falling back to a full-table scan. Query size remains
bounded at 1024 UTF-8 bytes.

Common literal fragments and broad OR expressions can still produce large posting
lists; indexability is not a latency guarantee. Benchmark first/next pages at 50,000
and 1,000,000 distinct paths and compare native GLOB verification against explicit
candidate retrieval. Warm serial timings do not establish cold latency or p95.
