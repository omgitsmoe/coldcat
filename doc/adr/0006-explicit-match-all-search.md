# ADR 0006: Explicit match-all search with standalone `*`

## Status

Accepted. Partially supersedes the indexable-term requirement in ADRs 0003 and 0004.

## Context

Search advanced filters can select files by membership and redundancy, but requiring
an indexable name/path term prevents browsing all qualifying files. For example,
finding every file with exactly one other disk copy does not need a name predicate.
The three-code-point minimum protects trigram retrieval, not replica filtering.

## Decision

Accept a standalone unquoted `*` in `syntax` and `whole`, ignoring outer whitespace,
as an explicit match-all request. Bypass parsing and trigram retrieval for this one
input and enumerate canonical search paths with constant `match` relevance. Reuse
complete-snapshot visibility, scope, membership and replica filtering, catalog-wide
counts, path/observation-ID ordering and revision-bound keyset pagination.

Do not accept this exception inside expressions or groups, or for other unindexable
patterns. Contains, Exact and legacy API modes retain their existing semantics.
No schema migration, index changes or cursor-version change is required: this input
was previously rejected and all previously valid queries keep their meaning.

## Alternatives considered

- A new All files mode: explicit, but adds another API/UI mode and query lifecycle.
- Empty queries: conflicts with the browser idle state and required API parameter.
- General short-pattern support: broadens the scan contract beyond this need.
- Allowing `*` within expressions: adds compiler complexity and potentially makes
  an entire OR expression nonselective; advanced filters already express the need.

## Consequences

Users can apply advanced filters without remembering a filename. Results are file
observations rather than unique contents. Replica bounds still require current
scope, and membership does not narrow copy counts. Pagination bounds returned rows,
not the work needed to find them; selective or empty replica-filtered requests can
inspect many paths. This is an intentional broad request, not a hidden scan fallback.
