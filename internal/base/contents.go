package base

type ReplicaMetric string

const (
	ReplicaDisks     ReplicaMetric = "disks"
	ReplicaLocations ReplicaMetric = "locations"
)

type ContentFilters struct {
	Scope            Scope         `json:"scope"`
	DiskID           DiskId        `json:"disk_id"`
	Directory        string        `json:"directory"`
	ReplicaMetric    ReplicaMetric `json:"replica_metric"`
	OtherReplicas    *int64        `json:"other_replicas"`
	MinOtherReplicas *int64        `json:"min_other_replicas"`
	MaxOtherReplicas *int64        `json:"max_other_replicas"`
}

type ContentPage struct {
	Filters    ContentFilters
	Catalog    CatalogState
	Items      []ContentSummary
	NextCursor string
}
