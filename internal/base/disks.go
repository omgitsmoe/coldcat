package base

type DiskField[T any] struct {
	Present bool
	Value   *T
}

type UpdateDiskRequest struct {
	Label    DiskField[string]
	Notes    DiskField[string]
	Serial   DiskField[string]
	Capacity DiskField[int64]
}

type DiskSummary struct {
	Disk           Disk
	LatestSnapshot *Snapshot
}

type DiskCataloged struct {
	FileCount            int64
	ContentCount         int64
	KnownBytes           int64
	UnknownSizeFileCount int64
	SizeComplete         bool
}

type DiskDetail struct {
	DiskSummary
	Cataloged *DiskCataloged
}

type DiskPage struct {
	Catalog    CatalogState
	MaxID      DiskId
	Items      []DiskSummary
	NextCursor string
}
