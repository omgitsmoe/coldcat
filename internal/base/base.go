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
	Id        SnapshotId
	DiskId    DiskId
	CreatedAt time.Time
}

type Content struct {
	Id       ContentId
	Size     int64
	HashType HashType
	Hash     []byte
}

type FileObservation struct {
	Id         FileObservationId
	SnapshotId SnapshotId
	ContentId  ContentId

	Path  string
	MTime time.Time
}
