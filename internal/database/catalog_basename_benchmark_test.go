package database

import (
	"testing"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func BenchmarkCatalogBasenameWorkflows(b *testing.B) {
	db := buildCatalogStorageFixture(b, nil)
	ctx := b.Context()
	if _, err := db.db.ExecContext(ctx, "VACUUM"); err != nil {
		b.Fatal(err)
	}
	for _, path := range []string{"", catalogStoragePrefix + "group-050"} {
		name := "detail_root"
		if path != "" {
			name = "detail_nested"
		}
		b.Run(name, func(b *testing.B) {
			for b.Loop() {
				if _, err := db.GetDirectory(ctx, 3, path); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
	for _, name := range []string{
		"entries_first", "entries_deep", "entries_history", "entries_filtered",
		"entries_recursive", "entries_root",
	} {
		b.Run(name, func(b *testing.B) {
			f := base.DirectoryFilters{
				SnapshotID: 3, Path: catalogStoragePrefix + "group-050",
				ReplicaMetric: base.ReplicaDisks,
			}
			switch name {
			case "entries_history":
				f.SnapshotID = 1
			case "entries_filtered":
				minimum := int64(0)
				f.MinOtherReplicas = &minimum
			case "entries_recursive":
				f.Path = catalogStoragePrefix[:len(catalogStoragePrefix)-1]
				f.Recursive = true
			case "entries_root":
				f.Path = ""
			}
			var anchor base.DirectoryAnchor
			var state *base.CatalogState
			if name == "entries_deep" {
				page, err := db.ListDirectoryEntries(ctx, f, 100, anchor, nil)
				if err != nil || len(page.Items) != 101 {
					b.Fatalf("deep anchor fixture: %d, %v", len(page.Items), err)
				}
				item := page.Items[99]
				anchor = base.DirectoryAnchor{ID: item.ID, Kind: item.Kind}
				state = &page.Catalog
			}
			for b.Loop() {
				if _, err := db.ListDirectoryEntries(ctx, f, 50, anchor, state); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
	for _, test := range []struct {
		name, field, match, query string
	}{
		{"name_exact_hit", "name", "exact", "document-0000050-revised-猫.txt"},
		{"name_exact_miss", "name", "exact", "猫"},
		{"path_exact_hit", "path", "exact", catalogStoragePrefix + "group-050/document-0000050-revised-猫.txt"},
		{"path_exact_miss", "path", "exact", catalogStoragePrefix + "猫"},
		{"name_substring_common", "name", "substring", "document"},
		{"name_substring_rare", "name", "substring", "document-0000050"},
		{"name_substring_unicode", "name", "substring", "猫.txt"},
		{"path_substring_common", "path", "substring", "collection"},
		{"path_substring_rare", "path", "substring", "group-050/document-0000050"},
		{"path_substring_unicode", "path", "substring", "地域と資料"},
	} {
		b.Run(test.name, func(b *testing.B) {
			f := base.SearchFilters{
				Query: test.query, Field: test.field, Match: test.match,
				ContentFilters: base.ContentFilters{
					Scope: base.ScopeCurrent, ReplicaMetric: base.ReplicaDisks,
				},
			}
			for b.Loop() {
				if _, err := db.Search(ctx, f, 50, base.SearchAnchor{}, nil); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}
