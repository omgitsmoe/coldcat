package importer

import (
	"path/filepath"
	"testing"

	"github.com/omgitsmoe/coldcat/internal/database"
)

func TestCleanupProfileCatalogLifetime(t *testing.T) {
	d := importDistributions(100)[1]
	baseline := distributionBaseline(t, d)
	for range 3 {
		c := openDistributionCatalog(t, d, baseline)
		raw := c.raw
		if err := closeCleanupCatalog(&c); err != nil {
			t.Fatal(err)
		}
		if c.raw != nil || c.db != nil || raw.Stats().OpenConnections != 0 {
			t.Fatal("catalog handles or raw connections retained")
		}
		if err := closeCleanupCatalog(&c); err != nil {
			t.Fatal(err)
		}
		if err := checkCleanupCatalogBeforeRecovery(t.Context(), d, c); err != nil {
			t.Fatal(err)
		}
		reopened, err := database.OpenContext(t.Context(), c.path)
		if err != nil {
			t.Fatalf("catalog lock retained: %v", err)
		}
		if err := reopened.Close(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestCleanupProfilePreRecoveryRejectsLeftovers(t *testing.T) {
	for _, mutation := range []struct {
		name string
		sql  string
	}{
		{"snapshot", `INSERT INTO snapshot(disk_id,state,captured_at,imported_at,capture_provenance)
 VALUES(1,'importing','2023-01-01','2023-01-01','explicit')`},
		{"FTS", "INSERT INTO search_trigram(search_trigram) VALUES('delete-all')"},
	} {
		t.Run(mutation.name, func(t *testing.T) {
			d := importDistributions(100)[1]
			c := openDistributionCatalog(t, d, distributionBaseline(t, d))
			if _, err := c.raw.ExecContext(t.Context(), mutation.sql); err != nil {
				t.Fatal(err)
			}
			if err := closeCleanupCatalog(&c); err != nil {
				t.Fatal(err)
			}
			if err := checkCleanupCatalogBeforeRecovery(t.Context(), d, c); err == nil {
				t.Fatal("pre-recovery check accepted damaged catalog")
			}
			raw, err := openCleanupCatalogReadOnly(c.path)
			if err != nil {
				t.Fatal(err)
			}
			defer raw.Close()
			query := "SELECT COUNT(*) FROM snapshot WHERE state='importing'"
			want := int64(1)
			if mutation.name == "FTS" {
				query = "SELECT COUNT(*) FROM search_trigram WHERE search_trigram MATCH 'report'"
				want = 0
			}
			if got := distributionCount(t, raw, query); got != want {
				t.Fatalf("inspection healed catalog: %d, want %d", got, want)
			}
		})
	}
}

func TestCleanupProfileReadOnlyEscapedPath(t *testing.T) {
	path := filepath.Join(t.TempDir(), "catalog ?#%.sqlite")
	db, err := database.OpenContext(t.Context(), path)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Close(); err != nil {
		t.Fatal(err)
	}
	raw, err := openCleanupCatalogReadOnly(path)
	if err != nil {
		t.Fatal(err)
	}
	defer raw.Close()
	if got := distributionCount(t, raw, "SELECT COUNT(*) FROM snapshot"); got != 0 {
		t.Fatal("unexpected snapshots")
	}
	if _, err := raw.ExecContext(t.Context(), "INSERT INTO disk(label,capacity) VALUES('write',0)"); err == nil {
		t.Fatal("read-only connection accepted a write")
	}
}
