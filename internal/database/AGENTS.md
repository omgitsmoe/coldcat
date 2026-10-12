# Search syntax feature checks

- Focused Go tests: `go test ./internal/database ./internal/app ./internal/httpapi -run 'TestSearch' -count=1`.
- `BenchmarkSearchSyntax` uses disposable 50,000/1,000,000 distinct-path fixtures with the production index, first/next-page cases, and native-GLOB versus explicit-trigram verification cases. Fixture creation is untimed; results are warm serial measurements, not cold-cache or p95 measurements.
- Distinct-path-only benchmark: `go test ./internal/database -run '^$' -bench '^BenchmarkSearchSyntax$/' -benchtime=3x -benchmem -v -timeout=30m`. Go benchmark component regexes match substrings; `^BenchmarkSearchSyntax/` also selects `BenchmarkSearchSyntaxHistory`.
- Focused small history benchmark: `go test ./internal/database -run '^$' -bench '^BenchmarkSearchSyntaxHistory$/50000/' -benchtime=3x -benchmem -v -timeout=30m`. Successfully measured 50,000 current/250,000 historical and 1,000,000 current/5,000,000 historical observations across 3 disks/5 snapshots, with current/history first/next pages. Fixture creation is untimed; results are warm serial means, not cold-cache or p95 measurements.
- Never test against the workspace catalog.
- Unified syntax benchmark selection: `go test ./internal/database -run '^$' -bench '^BenchmarkSearchSyntax$/50000/^(syntax_selective|syntax_broad|whole_combined)$/' -benchtime=3x -benchmem -v -timeout=30m`; use `/1000000/` instead for large scale. These use disposable fixtures and report warm serial means, not p95 or cold timings.
- Match-all benchmark: `go test ./internal/database -run '^$' -bench '^BenchmarkSearchMatchAll$' -benchtime=3x -benchmem -v`. Fixture: 50,000 current observations across two disks; unfiltered, exactly one other disk, and no-match location bounds. Results are warm serial first-page means, not cold or p95 measurements.

# Directory enrichment checks

- Focused tests: `go test ./internal/database -run '^TestDirectoryEnrichment' -count=1`.
- Distinct-content benchmark: `go test ./internal/database -run '^$' -bench '^BenchmarkDirectoryEnrichmentDistinct/' -benchtime=1x -count=3 -benchmem -timeout=10m`.
- The benchmark isolates the enrichment transaction, not a full import. Fixtures use
  20,000/100,000 distinct staged content sizes across two disks with one complete snapshot
  each and 50 contents per leaf directory. Disposable fixture setup and checks are excluded
  from measurements.
