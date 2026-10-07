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

type DirectoryComparisonFilters struct {
	SnapshotID SnapshotId `json:"snapshot_id"`
	Path       string     `json:"path"`
	Allow      []string   `json:"allow"`
	Block      []string   `json:"block"`
}

type DirectorySelection struct {
	RetainedFileCount    int64
	ExcludedFileCount    int64
	ContentCount         int64
	KnownBytes           int64
	UnknownSizeFileCount int64
	EmptyComparison      bool
}

type DirectoryReplica struct {
	DirectoryID       int64
	Disk              Disk
	Snapshot          Snapshot
	Path              string
	SameDisk          bool
	WholeTreeEqual    bool
	RetainedFileCount int64
	ExcludedFileCount int64
}

type DirectoryReplicaAnchor struct {
	DiskID      DiskId `json:"disk_id"`
	Path        string `json:"path"`
	DirectoryID int64  `json:"directory_id"`
}

type DirectoryReplicaPage struct {
	DirectoryContext
	Filters    DirectoryComparisonFilters
	Selection  DirectorySelection
	Items      []DirectoryReplica
	NextCursor string
}

type DirectoryCoverage struct {
	Disk                    Disk
	Snapshot                Snapshot
	CoveredFileCount        int64
	MissingFileCount        int64
	CoveredContentCount     int64
	MissingContentCount     int64
	CoveredKnownBytes       int64
	MissingKnownBytes       int64
	CoveredUnknownSizeFiles int64
	MissingUnknownSizeFiles int64
	Complete                bool
}

type DirectoryCoveragePage struct {
	DirectoryContext
	Filters    DirectoryComparisonFilters
	Selection  DirectorySelection
	Items      []DirectoryCoverage
	NextCursor string
}
