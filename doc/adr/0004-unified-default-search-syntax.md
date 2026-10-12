# ADR 0004: Unified search syntax by default

- Status: Accepted
- Date: 2026-10-11
- Supersedes: ADR 0003's separate browser modes and literal default
- Partially superseded by: ADR 0005's implicit AND between terms

## Context

Separate wildcard and AND/OR modes prevent combining ordered fragments with
alternatives and require choosing syntax before searching. Anchored wildcard
patterns also behave differently when switching from Name to a nested Path.
The owner requested unified syntax by default, retaining explicit whole-field
and literal matching options, and confirmed there are no bookmarks to migrate.

## Decision

Default `match=syntax` combines wildcard patterns, uppercase AND/OR, and
parentheses. Each term matches anywhere within the selected Name or Path.
`match=whole` uses the same grammar but anchors each term to the whole field.
Keep literal Contains (`substring`) and Exact (`exact`) as browser options.

AND binds before OR. Plain multiword text is one term, not implicit AND.
Quotes make their contents literal, including wildcard characters; quoted and
unquoted segments can be concatenated. Backslash escapes wildcard characters,
backslash, double quote, and parentheses. Ignore outer unquoted whitespace;
preserve quoted whitespace. Reject malformed syntax instead of guessing intent.

Reuse trigram postings for each term's mandatory literal runs and combine those
candidates with the expression tree. Verify every full pattern before pagination.
Every term must be indexable, including every OR branch. Retain limits of 1024
UTF-8 bytes, 32 terms, 32 indexed runs in total, and 16 parenthesis levels.
Keep folding, visibility, filters, replica counts and exhaustive retrieval unchanged.
Both new modes use constant relevance `match`, binary path/ID ordering, and a new
cursor version. No index or schema change is required.

Keep explicit legacy wildcard/expression API semantics, but do not add browser
compatibility options or bookmark migration. Omitted API match now means syntax.

## Alternatives considered

- Keep separate modes: preserves literal defaults but cannot combine wildcards
  and Boolean operators and adds unnecessary mode selection.
- Auto-detect syntax and fall back to literal input: hides malformed queries
  and makes interpretation unpredictable.
- Implicit AND for whitespace: changes familiar multiword substring searches.
- Anchor default patterns: requires leading/trailing stars for common remembered
  fragments, especially when searching nested paths.
- Remove legacy API modes: unnecessary breakage for explicit callers; keeping
  their existing implementations does not require browser migration machinery.

## Consequences

Unspecified-mode API calls now interpret syntax characters. Callers needing literal
input must explicitly select Contains or Exact. Whole-field AND operates on anchored
patterns, not ordinary substrings, so its meaning must be visible in help text.
Broad wildcard/OR queries remain potentially expensive despite being indexed.
Existing performance measurements for legacy modes are not evidence of unified
mode latency; focused combined-pattern benchmarks are required for comparison.
