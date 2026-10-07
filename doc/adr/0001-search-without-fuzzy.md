# ADR 0001: Indexed case-insensitive search without typo matching

- Status: Accepted
- Date: 2026-10-07

## Context

The catalog must make remembered filenames and disk-relative paths searchable without
disproportionate storage or import costs. The persisted fuzzy benchmark with one million
distinct short paths created 41.8 million signature postings and a 5.09 GB catalog.
Fixture construction took 11m12.5s; broad folded-prefix search averaged 1.67 seconds.
Those are raw fixture measurements, not streaming-import throughput or incremental
fuzzy-index bytes. A user import also reported about 200 committed files per second.
The fuzzy and short-character posting structures amplify writes substantially, though
the user's import has not been profiled to establish their individual contributions.

## Decision

Remove typo-tolerant search and its signature indexes. Support only `match=exact` and
`match=substring`, both case-insensitive. Require at least three Unicode code points for
substring queries, returning a validation error for shorter terms. Exact queries may
still contain one or two code points and use B-tree lookups.

Use one external-content FTS5 trigram index over folded basename and full relative path.
Use explicit version-1 Unicode simple folding for indexed text and query verification,
rather than changing normalization to the tokenizer's built-in case-insensitive rules.
Preserve punctuation and original spelling; do not canonically normalize Unicode or
equate `ß` with `ss`. Keep deterministic exact/prefix/substring ranking, exhaustive
retrieval, original-path ordering, replica filters, and revision-bound pagination.

Remove both literal and folded short-character posting tables and the separate folded
FTS index. Change the initial schema under the existing pre-0.1 recreation policy;
do not silently rebuild or modify existing catalogs. Reject the obsolete search schema
at catalog open with a recreation/reimport instruction. Invalidate old search cursors
because case sensitivity and ranking semantics have changed.

## Alternatives considered

- Keep fuzzy search: preserves typo recall but retains substantial posting and write costs.
- Keep one-/two-character substring postings: preserves short queries but retains another
  large derived index family and additional import work.
- Scan for short substrings: avoids those indexes but creates unpredictable catalog-wide
  interactive work; reject unsupported queries instead of adding a hidden fallback.
- Use FTS5's default case-insensitive tokenizer directly: simpler, but would change the
  explicit Unicode folding contract without necessity.

## Consequences

Misspellings such as `reprot` no longer match `report`. Clients must not offer fuzzy mode
and must submit at least three code points for substring search. Case-insensitive exact
queries can return multiple differently cased paths, while directory identity remains
case-sensitive. Existing development catalogs need recreation and reimport with approval;
the workspace catalog is not recreated as part of this change.

Storage and import write amplification should decrease, but acceptance still requires
before/after measurements on equivalent fixtures and streaming imports. This decision
does not claim that broad queries, directory construction, or recovery are already fast
enough, and does not close the frontend handoff gate.
