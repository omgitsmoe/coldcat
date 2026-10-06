package base

type SearchFilters struct {
	Query      string     `json:"q"`
	Field      string     `json:"field"`
	Match      string     `json:"match"`
	SnapshotID SnapshotId `json:"snapshot_id"`
	ContentFilters
}

type SearchAnchor struct {
	Relevance string            `json:"relevance"`
	Path      string            `json:"path,omitempty"`
	ID        FileObservationId `json:"id"`
}

type SearchItem struct {
	Observation FileObservation
	Snapshot    Snapshot
	Disk        Disk
	Content     ContentSummary
	Basename    string
	IsCurrent   bool
	Relevance   string
}

type SearchPage struct {
	Filters    SearchFilters
	Catalog    CatalogState
	Items      []SearchItem
	NextCursor string
}
