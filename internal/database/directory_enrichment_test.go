package database

import (
	"errors"
	"strings"
	"testing"
)

func TestDirectoryEnrichmentDistinct(t *testing.T) {
	for _, legacy := range []bool{false, true} {
		name := "normalized"
		if legacy {
			name = "legacy"
		}
		t.Run(name, func(t *testing.T) {
			db := enrichmentDistinctFixture(t, 1000, legacy)
			rollback := errors.New("rollback enrichment")
			err := db.TransactionContext(t.Context(), func(tx *Tx) error {
				if err := enrichDirectories(t.Context(), tx, 3); err != nil {
					return err
				}
				return rollback
			})
			if !errors.Is(err, rollback) {
				t.Fatal(err)
			}
			var wrong int
			err = db.db.QueryRow(`SELECT COUNT(*) FROM directory WHERE known_bytes!=0
 OR unique_content_known_bytes!=0 OR unknown_size_file_count!=file_count
 OR unknown_size_content_count!=content_count`).Scan(&wrong)
			if err != nil || wrong != 0 {
				t.Fatalf("enrichment did not roll back: %d rows, %v", wrong, err)
			}
			err = db.TransactionContext(t.Context(), func(tx *Tx) error {
				return enrichDirectories(t.Context(), tx, 3)
			})
			if err != nil {
				t.Fatal(err)
			}
			checkDistinctEnrichment(t, db, 1000)
		})
	}
}

func TestDirectoryEnrichmentUsesMembershipSeeks(t *testing.T) {
	db := enrichmentDistinctFixture(t, 1000, false)
	rows, err := db.db.Query("EXPLAIN QUERY PLAN "+directoryEnrichmentUpdate,
		1, "leaf-0001", 3, 1, "leaf-0001", 3,
		1, "leaf-0001", 3, 1, "leaf-0001", 3, 1, "leaf-0001")
	if err != nil {
		t.Fatal(err)
	}
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var id, parent, unused int
		var detail string
		if err := rows.Scan(&id, &parent, &unused, &detail); err != nil {
			t.Fatal(err)
		}
		plan = append(plan, detail)
	}
	if err := rows.Err(); err != nil {
		t.Fatal(err)
	}
	text := strings.Join(plan, "\n")
	t.Log(text)
	if strings.Count(text, "USING PRIMARY KEY (directory_id=?)") != 4 ||
		strings.Count(text, "(snapshot_id=? AND content_id=?)") != 4 {
		t.Fatalf("aggregates must seek memberships before looking up staged sizes:\n%s", text)
	}
}
