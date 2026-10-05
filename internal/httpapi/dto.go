package httpapi

import (
	"encoding/hex"
	"strconv"
	"time"

	"github.com/omgitsmoe/coldcat/internal/base"
)

func decimal[T ~int64 | ~uint64](value T) string { return strconv.FormatUint(uint64(value), 10) }

func timestamp(value time.Time) string { return value.UTC().Format(time.RFC3339Nano) }

func nullableTime(value *time.Time) *string {
	if value == nil {
		return nil
	}
	result := timestamp(*value)
	return &result
}

func nullableSize(value *int64) *string {
	if value == nil {
		return nil
	}
	result := decimal(*value)
	return &result
}

type hashDTO struct {
	Algorithm string `json:"algorithm"`
	Hex       string `json:"hex"`
}

type contentDTO struct {
	ID                   string     `json:"id"`
	Hash                 hashDTO    `json:"hash"`
	Size                 *string    `json:"size"`
	Scope                base.Scope `json:"scope"`
	LocationCount        string     `json:"location_count"`
	DiskCount            string     `json:"disk_count"`
	ObservationCount     string     `json:"observation_count"`
	CurrentLocationCount string     `json:"current_location_count"`
	CurrentDiskCount     string     `json:"current_disk_count"`
}

func contentResponse(value base.ContentSummary) (contentDTO, error) {
	algorithm, err := value.Content.HashType.ToIdentifier()
	if err != nil {
		return contentDTO{}, err
	}
	return contentDTO{ID: decimal(value.Content.Id), Hash: hashDTO{Algorithm: algorithm, Hex: hex.EncodeToString(value.Content.Hash)}, Size: nullableSize(value.Content.Size), Scope: value.Scope,
		LocationCount: decimal(value.LocationCount), DiskCount: decimal(value.DiskCount), ObservationCount: decimal(value.ObservationCount), CurrentLocationCount: decimal(value.CurrentLocationCount), CurrentDiskCount: decimal(value.CurrentDiskCount)}, nil
}

type diskDTO struct {
	ID       string `json:"id"`
	Label    string `json:"label"`
	Serial   string `json:"serial"`
	Capacity string `json:"capacity"`
}

func diskResponse(value base.Disk) diskDTO {
	return diskDTO{ID: decimal(value.Id), Label: value.Label, Serial: value.Serial, Capacity: decimal(value.Capacity)}
}

type snapshotDTO struct {
	ID                string `json:"id"`
	DiskID            string `json:"disk_id"`
	State             string `json:"state"`
	CapturedAt        string `json:"captured_at"`
	ImportedAt        string `json:"imported_at"`
	CaptureProvenance string `json:"capture_provenance"`
	InputPath         string `json:"input_path"`
	InputFormat       string `json:"input_format"`
	FileCount         string `json:"file_count"`
	ContentCount      string `json:"content_count"`
}

func snapshotResponse(value base.Snapshot) snapshotDTO {
	return snapshotDTO{ID: decimal(value.Id), DiskID: decimal(value.DiskId), State: "complete", CapturedAt: timestamp(value.CapturedAt), ImportedAt: timestamp(value.ImportedAt), CaptureProvenance: value.CaptureProvenance, InputPath: value.InputPath, InputFormat: value.InputFormat, FileCount: decimal(value.FileCount), ContentCount: decimal(value.ContentCount)}
}

type observationDTO struct {
	ID         string  `json:"id"`
	ContentID  string  `json:"content_id"`
	SnapshotID string  `json:"snapshot_id"`
	Path       string  `json:"path"`
	MTime      *string `json:"mtime"`
}

func observationResponse(value base.FileObservation) observationDTO {
	return observationDTO{ID: decimal(value.Id), ContentID: decimal(value.ContentId), SnapshotID: decimal(value.SnapshotId), Path: value.Path, MTime: nullableTime(value.MTime)}
}

type observationSummaryDTO struct {
	Observation        observationDTO `json:"observation"`
	Snapshot           snapshotDTO    `json:"snapshot"`
	Disk               diskDTO        `json:"disk"`
	Content            contentDTO     `json:"content"`
	OtherLocationCount string         `json:"other_location_count"`
	OtherDiskCount     string         `json:"other_disk_count"`
}

type contentObservationDTO struct {
	Observation observationDTO `json:"observation"`
	Snapshot    snapshotDTO    `json:"snapshot"`
	Disk        diskDTO        `json:"disk"`
	Size        *string        `json:"size"`
	IsCurrent   bool           `json:"is_current"`
}

type observationPageDTO struct {
	Scope      base.Scope              `json:"scope"`
	Revision   string                  `json:"revision"`
	Items      []contentObservationDTO `json:"items"`
	NextCursor *string                 `json:"next_cursor"`
}

func pageResponse(value base.ContentObservationPage) observationPageDTO {
	result := observationPageDTO{Scope: value.Scope, Revision: decimal(value.Catalog.Revision), Items: make([]contentObservationDTO, 0, len(value.Items))}
	if value.NextCursor != "" {
		result.NextCursor = &value.NextCursor
	}
	for _, item := range value.Items {
		result.Items = append(result.Items, contentObservationDTO{Observation: observationResponse(item.Observation), Snapshot: snapshotResponse(item.Snapshot), Disk: diskResponse(item.Disk), Size: nullableSize(item.Size), IsCurrent: item.IsCurrent})
	}
	return result
}
