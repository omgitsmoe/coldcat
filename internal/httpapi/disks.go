package httpapi

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"mime"
	"net/http"
	"strconv"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

var errUnsupportedMediaType = errors.New("unsupported media type")
var errBodyTooLarge = errors.New("request body too large")

func decodeBody(w http.ResponseWriter, r *http.Request, dst any) error {
	media, _, err := mime.ParseMediaType(r.Header.Get("Content-Type"))
	if err != nil || media != "application/json" {
		return errUnsupportedMediaType
	}

	r.Body = http.MaxBytesReader(w, r.Body, 65536)
	decoder := json.NewDecoder(r.Body)
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(dst); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return errBodyTooLarge
		}

		return fmt.Errorf("%w: invalid JSON body", database.ErrValidation)
	}

	if err := decoder.Decode(new(any)); err != io.EOF {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			return errBodyTooLarge
		}

		return fmt.Errorf("%w: body must contain one JSON object", database.ErrValidation)
	}
	return nil
}

type diskJSONField[T any] struct {
	base.DiskField[T]
}

func (f *diskJSONField[T]) UnmarshalJSON(data []byte) error {
	f.Present = true
	return json.Unmarshal(data, &f.Value)
}

type diskWriteDTO struct {
	Label    diskJSONField[string] `json:"label"`
	Notes    diskJSONField[string] `json:"notes"`
	Serial   diskJSONField[string] `json:"serial"`
	Capacity diskJSONField[string] `json:"capacity"`
}

func capacityField(value diskJSONField[string]) (base.DiskField[int64], error) {
	result := base.DiskField[int64]{Present: value.Present}
	if !value.Present || value.Value == nil {
		return result, nil
	}

	text := *value.Value
	if text == "" {
		return result, fmt.Errorf("%w: capacity must be a decimal byte count", database.ErrValidation)
	}
	for _, char := range text {
		if char < '0' || char > '9' {
			return result, fmt.Errorf(
				"%w: capacity must be a decimal byte count", database.ErrValidation,
			)
		}
	}
	number, err := strconv.ParseInt(text, 10, 64)
	if err != nil {
		return result, fmt.Errorf("%w: capacity exceeds signed 64-bit range", database.ErrValidation)
	}

	result.Value = &number
	return result, nil
}

func createDisk(a *app.App, w http.ResponseWriter, r *http.Request) error {
	if _, err := query(r); err != nil {
		return err
	}

	var body diskWriteDTO
	if err := decodeBody(w, r, &body); err != nil {
		return err
	}

	capacity, err := capacityField(body.Capacity)
	if err != nil {
		return err
	}

	if body.Label.Value == nil || capacity.Value == nil {
		return fmt.Errorf("%w: label and capacity required", database.ErrValidation)
	}

	req := app.CreateDiskRequest{Label: *body.Label.Value, Capacity: *capacity.Value}
	if body.Notes.Value != nil {
		req.Notes = *body.Notes.Value
	}
	if body.Serial.Value != nil {
		req.Serial = *body.Serial.Value
	}
	id, err := a.CreateDiskWithRequest(r.Context(), req)
	if err != nil {
		return err
	}

	result, err := a.GetDisk(r.Context(), id)
	if err != nil {
		return err
	}

	w.Header().Set("Location", "/api/v1/disks/"+decimal(id))
	writeJSON(w, http.StatusCreated, diskDetailResponse(result))
	return nil
}

func getDisk(a *app.App, w http.ResponseWriter, r *http.Request) error {
	id, err := resourceID(r)
	if err != nil {
		return err
	}

	if _, err := query(r); err != nil {
		return err
	}

	result, err := a.GetDisk(r.Context(), base.DiskId(id))
	if err != nil {
		return err
	}

	writeJSON(w, 200, diskDetailResponse(result))
	return nil
}

func updateDisk(a *app.App, w http.ResponseWriter, r *http.Request) error {
	id, err := resourceID(r)
	if err != nil {
		return err
	}

	if _, err := query(r); err != nil {
		return err
	}

	var body diskWriteDTO
	if err := decodeBody(w, r, &body); err != nil {
		return err
	}

	capacity, err := capacityField(body.Capacity)
	if err != nil {
		return err
	}

	result, err := a.UpdateDisk(r.Context(), base.DiskId(id), app.UpdateDiskRequest{
		Label: body.Label.DiskField, Notes: body.Notes.DiskField,
		Serial: body.Serial.DiskField, Capacity: capacity,
	})
	if err != nil {
		return err
	}

	writeJSON(w, 200, diskDetailResponse(result))
	return nil
}

func listDisks(a *app.App, w http.ResponseWriter, r *http.Request) error {
	values, err := query(r, "limit", "cursor")
	if err != nil {
		return err
	}

	limit := 50
	if values.Has("limit") {
		limit, err = strconv.Atoi(values.Get("limit"))
		if err != nil || limit < 1 || limit > 200 {
			return fmt.Errorf("%w: limit must be between 1 and 200", database.ErrValidation)
		}
	}
	result, err := a.ListDisks(r.Context(), app.ListDisksRequest{
		Limit: limit, Cursor: values.Get("cursor"),
	})
	if err != nil {
		return err
	}

	writeJSON(w, 200, diskPageResponse(result))
	return nil
}

type diskMetadataDTO struct {
	ID       string  `json:"id"`
	Label    string  `json:"label"`
	Notes    *string `json:"notes"`
	Serial   *string `json:"serial"`
	Capacity string  `json:"capacity"`
}

type diskSummaryDTO struct {
	diskMetadataDTO
	LatestSnapshot *snapshotDTO `json:"latest_snapshot"`
}

type diskCatalogedDTO struct {
	FileCount            string `json:"file_count"`
	ContentCount         string `json:"content_count"`
	KnownBytes           string `json:"known_bytes"`
	UnknownSizeFileCount string `json:"unknown_size_file_count"`
	SizeComplete         bool   `json:"size_complete"`
}

type diskDetailDTO struct {
	diskSummaryDTO
	Cataloged *diskCatalogedDTO `json:"cataloged"`
}

type diskPageDTO struct {
	Revision   string           `json:"revision"`
	Items      []diskSummaryDTO `json:"items"`
	NextCursor *string          `json:"next_cursor"`
}

func optionalText(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func diskSummaryResponse(value base.DiskSummary) diskSummaryDTO {
	disk := value.Disk
	result := diskSummaryDTO{diskMetadataDTO: diskMetadataDTO{
		ID: decimal(disk.Id), Label: disk.Label, Notes: optionalText(disk.Notes),
		Serial: optionalText(disk.Serial), Capacity: decimal(disk.Capacity),
	}}
	if value.LatestSnapshot != nil {
		snapshot := snapshotResponse(*value.LatestSnapshot)
		result.LatestSnapshot = &snapshot
	}
	return result
}

func diskDetailResponse(value base.DiskDetail) diskDetailDTO {
	result := diskDetailDTO{diskSummaryDTO: diskSummaryResponse(value.DiskSummary)}
	if c := value.Cataloged; c != nil {
		result.Cataloged = &diskCatalogedDTO{
			FileCount: decimal(c.FileCount), ContentCount: decimal(c.ContentCount),
			KnownBytes: decimal(c.KnownBytes), UnknownSizeFileCount: decimal(c.UnknownSizeFileCount),
			SizeComplete: c.SizeComplete,
		}
	}
	return result
}

func diskPageResponse(value base.DiskPage) diskPageDTO {
	result := diskPageDTO{
		Revision: decimal(value.Catalog.Revision), Items: make([]diskSummaryDTO, 0, len(value.Items)),
	}
	if value.NextCursor != "" {
		result.NextCursor = &value.NextCursor
	}
	for _, item := range value.Items {
		result.Items = append(result.Items, diskSummaryResponse(item))
	}
	return result
}
