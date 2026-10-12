package base

type DirectorySearchFilters struct {
	Query      string     `json:"q"`
	Field      string     `json:"field"`
	Match      string     `json:"match"`
	Scope      Scope      `json:"scope"`
	DiskID     DiskId     `json:"disk_id"`
	SnapshotID SnapshotId `json:"snapshot_id"`
}

type DirectorySearchItem struct {
	ID        int64
	Directory DirectorySummary
	Snapshot  Snapshot
	Disk      Disk
	Basename  string
	IsCurrent bool
}

type DirectorySearchPage struct {
	Filters    DirectorySearchFilters
	Catalog    CatalogState
	Items      []DirectorySearchItem
	NextCursor string
}
