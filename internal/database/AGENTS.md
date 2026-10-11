# Search syntax feature checks

- Focused Go tests: `go test ./internal/database ./internal/app ./internal/httpapi -run 'TestSearch' -count=1`.
- `BenchmarkSearchSyntax` uses disposable 50,000/1,000,000 distinct-path fixtures with the production index, first/next-page cases, and native-GLOB versus explicit-trigram verification cases. Fixture creation is untimed; results are warm serial measurements, not cold-cache or p95 measurements.
- Distinct-path-only benchmark: `go test ./internal/database -run '^$' -bench '^BenchmarkSearchSyntax$/' -benchtime=3x -benchmem -v -timeout=30m`. Go benchmark component regexes match substrings; `^BenchmarkSearchSyntax/` also selects `BenchmarkSearchSyntaxHistory`.
- Focused small history benchmark: `go test ./internal/database -run '^$' -bench '^BenchmarkSearchSyntaxHistory$/50000/' -benchtime=3x -benchmem -v -timeout=30m`. Successfully measured 50,000 current/250,000 historical and 1,000,000 current/5,000,000 historical observations across 3 disks/5 snapshots, with current/history first/next pages. Fixture creation is untimed; results are warm serial means, not cold-cache or p95 measurements.
- Never test against the workspace catalog.
