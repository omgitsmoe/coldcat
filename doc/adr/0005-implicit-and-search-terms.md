# ADR 0005: Implicit AND between search terms

- Status: Accepted
- Date: 2026-10-12
- Supersedes: ADR 0004's multiword-term behavior

## Context

The owner requested that unquoted search terms be combined implicitly with AND,
and that literal multiword matches require double quotes. Previously, `tax return`
was a single substring, preventing ordinary space-separated queries from finding
terms separated by other text or appearing in the opposite order.

## Decision

In `syntax` and `whole`, unquoted whitespace separates terms. Adjacent terms or
parenthesized groups imply AND, with the same precedence as explicit AND and
higher precedence than OR. Thus `personal OR tax return` means
`personal OR (tax AND return)`. Each term matches the same selected field;
implicit AND does not impose ordering or proximity.

Double quotes preserve literal content, including spaces, operators, parentheses,
and wildcards. Quoted and unquoted segments without intervening whitespace still
form one pattern, such as `report*".pdf"`. Whole-field syntax continues to anchor
each term independently. Contains, Exact, and legacy API modes are unchanged.

Retain existing validation and indexing limits: each term needs a literal run of
at least three Unicode code points, with at most 32 terms, 32 indexed runs, and
16 parenthesis levels. Invalid queries fail rather than falling back to literals.
Reuse the existing expression compiler and indexes; no catalog rebuild is needed.
Version syntax/whole cursors separately from the previous grammar so pagination
cannot silently continue with different matching semantics.

## Alternatives considered

- Retain single multiword substrings: contradicts the requested search behavior.
- Change only default syntax: makes the two syntax modes use different grammars.
- Apply implicit AND to Contains or legacy expression: breaks explicit literal
  mode or legacy API contracts unnecessarily.
- Accept short terms through an unindexed fallback: hides potentially expensive
  scans and violates the existing indexability requirement.

## Consequences

Existing unquoted multiword syntax queries change meaning. Quote phrases to
preserve contiguous literal matches, or use Contains for uninterpreted input.
Short words that previously belonged to a longer phrase now fail validation when
searched as separate terms; a quoted phrase may remain indexable.
Whole-field `tax return` requires both entire-field patterns to match, unlike
`"tax return"`; use surrounding wildcards for separate fragments in that mode.
Old syntax/whole cursors require restarting pagination. Other search contracts,
catalog storage, import indexing, filters, and ordering remain unchanged.
