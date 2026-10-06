package base

import "time"

type DirectorySummary struct {
	Path                    string
	FileCount               int64
	ContentCount            int64
	KnownBytes              int64
	UnknownSizeFileCount    int64
	UniqueContentKnownBytes int64
	UnknownSizeContentCount int64
	MaxKnownMTime           *time.Time
}

type RedundancyBucket struct {
	OtherDiskCount int64
	FileCount      int64
}

type DirectoryContext struct {
	Snapshot  Snapshot
	Catalog   CatalogState
	IsCurrent bool
}

type DirectoryDetail struct {
	DirectoryContext
	DirectorySummary
	RedundancyHistogram []RedundancyBucket
}

type DirectoryFilters struct {
	SnapshotID       SnapshotId    `json:"snapshot_id"`
	Path             string        `json:"path"`
	DirectoriesOnly  bool          `json:"directories_only"`
	Recursive        bool          `json:"recursive"`
	ReplicaMetric    ReplicaMetric `json:"replica_metric"`
	OtherReplicas    *int64        `json:"other_replicas"`
	MinOtherReplicas *int64        `json:"min_other_replicas"`
	MaxOtherReplicas *int64        `json:"max_other_replicas"`
}

type DirectoryEntry struct {
	ID                 int64
	Path               string
	Kind               string
	Directory          *DirectorySummary
	Observation        *FileObservation
	Content            *Content
	OtherLocationCount int64
	OtherDiskCount     int64
}

type DirectoryAnchor struct {
	ID   int64  `json:"id"`
	Kind string `json:"kind"`
}

type DirectoryPage struct {
	DirectoryContext
	Filters    DirectoryFilters
	Items      []DirectoryEntry
	NextCursor string
}
