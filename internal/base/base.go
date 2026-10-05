package base

import (
	"time"
)

type DiskId int64
type SnapshotId int64
type ContentId int64
type FileObservationId int64

type Disk struct {
	Id       DiskId
	Label    string
	Serial   string
	Capacity uint64
}

type Snapshot struct {
	Id                SnapshotId
	DiskId            DiskId
	CapturedAt        time.Time
	ImportedAt        time.Time
	CaptureProvenance string
	InputPath         string
	InputFormat       string
	FileCount         int64
	ContentCount      int64
}

type Content struct {
	Id       ContentId
	Size     *int64
	HashType HashType
	Hash     []byte
}

type FileObservation struct {
	Id         FileObservationId
	SnapshotId SnapshotId
	ContentId  ContentId

	Path  string
	MTime *time.Time
}

type Scope string

const (
	ScopeCurrent Scope = "current"
	ScopeHistory Scope = "history"
)

type ContentSummary struct {
	Content              Content
	Scope                Scope
	LocationCount        int64
	DiskCount            int64
	ObservationCount     int64
	CurrentLocationCount int64
	CurrentDiskCount     int64
}

type ObservationSummary struct {
	Observation        FileObservation
	Snapshot           Snapshot
	Disk               Disk
	Content            ContentSummary
	OtherLocationCount int64
	OtherDiskCount     int64
}
