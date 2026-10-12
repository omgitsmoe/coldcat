# ADR 0007: Search directories independently of files

Status: Accepted

## Context

Remembering a folder name should find that folder, not every file whose path contains it.
The catalog already stores a directory tree and recursive summaries for each inventory.
File-search results and copy counts describe content observations, not directory identities.

## Decision

Add Files / Directories controls to the existing Search workspace. Files remain the default;
`target=directories` is bookmarkable frontend state selecting `GET /api/v1/search/directories`.
Directory search returns one row per stored directory per complete inventory, with disk,
inventory, recursive file counts and known bytes. Root has empty path/name and is searchable
with standalone `*`; empty nonroot directories cannot be reconstructed from imported files.
Partial sizes remain explicitly partial rather than implying a complete total.

Reuse the existing Unicode simple fold, literal exact/substring semantics and unified
syntax/whole parser and verifier, including implicit AND, quoting, wildcard rules, short-query
limits and complexity bounds. Search only the selected directory name/path, never filenames.
All directory modes sort by original binary path then stable directory ID, without ranking.
Current/history and disk/inventory filters retain file-search visibility semantics. File-copy
bounds and file membership filters are rejected by the directory API and hidden in its UI.
Switching result type clears file-only filters while keeping the query and shared filters.

Use revision-, filter-, limit- and kind-bound ID cursors, reconstructing the ordered path and
validating anchor eligibility inside a read transaction. IDs avoid long-path cursor growth.
Opening a result navigates to the existing browser; a validated search-return link and retained
workspace restore the query, result type, pages and selection. Mode changes retire in-flight
requests and selection using the existing workspace lifecycle.

Initially query the stored directory tree using deterministic folded-field SQL and compiled
matching predicates. Do not add persistent search tables, migrate catalogs or modify imports.
Fetch summaries and cached inventory/disk metadata only for the selected page plus lookahead.

## Alternatives

- A separate directory-search page duplicates search controls and navigation state.
- Grouping file hits into parent directories misses ancestor folders, depends on file
  pagination, and does not actually search directory names.
- A persistent directory trigram/name index improves selective search but adds storage,
  migration, import, recovery and integrity obligations before directory-scale measurements.
- Reusing content-copy filters suggests unsupported equivalence between entire folders.

## Consequences

Existing file API/default bookmarks and catalog schema remain unchanged. Directory results
can repeat across disks and historical inventories and do not claim directory replica identity.
Query work can scan all eligible directories and sort matching rows even for a small or empty
page; pagination bounds response size, not latency. This is a deliberate first-version tradeoff,
not an indexed-search performance claim. Measure representative directory workloads before
adding a persistent index; syntax length limits remain consistent despite scan-based retrieval.
