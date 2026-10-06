package httpapi

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/url"
	"strconv"

	"github.com/omgitsmoe/coldcat/internal/app"
	"github.com/omgitsmoe/coldcat/internal/base"
	"github.com/omgitsmoe/coldcat/internal/database"
)

type errorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type errorDTO struct {
	Error errorBody `json:"error"`
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	data, err := json.Marshal(value)
	if err != nil {
		slog.Error("encode HTTP response", "error", err)
		status = http.StatusInternalServerError
		data = []byte(`{"error":{"code":"internal_error","message":"internal server error"}}`)
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if _, err := w.Write(append(data, '\n')); err != nil {
		slog.Debug("write HTTP response", "error", err)
	}
}

func writeError(w http.ResponseWriter, err error) {
	status, code, message := http.StatusInternalServerError, "internal_error", "internal server error"
	switch {
	case errors.Is(err, database.ErrValidation):
		status, code, message = 400, "invalid_request", err.Error()
	case errors.Is(err, database.ErrNotFound):
		status, code, message = 404, "not_found", "resource not found"
	case errors.Is(err, database.ErrStaleCursor):
		status, code, message = 409, "stale_cursor", err.Error()
	case errors.Is(err, database.ErrConflict):
		status, code, message = 409, "conflict", "request conflicts with catalog state"
	case errors.Is(err, database.ErrBusy):
		status, code, message = 503, "catalog_unavailable", "catalog is unavailable"
	case errors.Is(err, context.DeadlineExceeded):
		status, code, message = 503, "catalog_unavailable", "request deadline exceeded"
	case errors.Is(err, context.Canceled):
		return
	default:
		slog.Error("HTTP request failed", "error", err)
	}

	writeJSON(w, status, errorDTO{Error: errorBody{Code: code, Message: message}})
}

func query(r *http.Request, allowed ...string) (url.Values, error) {
	values, err := url.ParseQuery(r.URL.RawQuery)
	if err != nil {
		return nil, fmt.Errorf("%w: malformed query", database.ErrValidation)
	}

	for key, items := range values {
		found := false
		for _, name := range allowed {
			if key == name {
				found = true
				break
			}
		}

		if !found || len(items) != 1 || items[0] == "" {
			return nil, fmt.Errorf(
				"%w: unknown, repeated, or empty parameter %q",
				database.ErrValidation,
				key,
			)
		}
	}

	return values, nil
}

func scope(values url.Values) (base.Scope, error) {
	value := base.ScopeCurrent
	if values.Has("scope") {
		value = base.Scope(values.Get("scope"))
	}

	if value != base.ScopeCurrent && value != base.ScopeHistory {
		return "", fmt.Errorf("%w: scope must be current or history", database.ErrValidation)
	}

	return value, nil
}

func resourceID(r *http.Request) (int64, error) {
	value, err := strconv.ParseInt(r.PathValue("id"), 10, 64)
	if err != nil || value <= 0 {
		return 0, fmt.Errorf("%w: id must be a positive 64-bit integer", database.ErrValidation)
	}

	return value, nil
}

func New(a *app.App) http.Handler {
	mux := http.NewServeMux()
	register := func(pattern string, handler func(http.ResponseWriter, *http.Request) error) {
		mux.HandleFunc(pattern, func(w http.ResponseWriter, r *http.Request) {
			if r.Method != http.MethodGet && r.Method != http.MethodHead {
				w.Header().Set("Allow", "GET, HEAD")
				writeJSON(
					w,
					405,
					errorDTO{
						Error: errorBody{Code: "method_not_allowed", Message: "method not allowed"},
					},
				)
				return
			}

			if err := handler(w, r); err != nil {
				writeError(w, err)
			}
		})
	}
	register("/healthz", func(w http.ResponseWriter, r *http.Request) error {
		if _, err := query(r); err != nil {
			return err
		}

		if err := a.CheckReadiness(r.Context()); err != nil {
			slog.Error("catalog readiness failed", "error", err)
			writeJSON(
				w,
				503,
				errorDTO{
					Error: errorBody{
						Code:    "catalog_unavailable",
						Message: "catalog is unavailable",
					},
				},
			)
			return nil
		}

		writeJSON(w, 200, map[string]string{"status": "ready"})
		return nil
	})
	register("/api/v1/contents/lookup", func(w http.ResponseWriter, r *http.Request) error {
		values, err := query(r, "hash_type", "hash", "scope")
		if err != nil {
			return err
		}

		applied, err := scope(values)
		if err != nil {
			return err
		}

		result, err := a.LookupContent(
			r.Context(),
			app.LookupContentRequest{
				HashType: values.Get("hash_type"),
				Hash:     values.Get("hash"),
				Scope:    applied,
			},
		)
		if err != nil {
			return err
		}

		dto, err := contentResponse(result)
		if err != nil {
			return err
		}

		writeJSON(w, 200, dto)
		return nil
	})
	register("/api/v1/contents/{id}", func(w http.ResponseWriter, r *http.Request) error {
		id, err := resourceID(r)
		if err != nil {
			return err
		}

		values, err := query(r, "scope")
		if err != nil {
			return err
		}

		applied, err := scope(values)
		if err != nil {
			return err
		}

		result, err := a.GetContentSummary(r.Context(), base.ContentId(id), applied)
		if err != nil {
			return err
		}

		dto, err := contentResponse(result)
		if err != nil {
			return err
		}

		writeJSON(w, 200, dto)
		return nil
	})
	register(
		"/api/v1/contents/{id}/observations",
		func(w http.ResponseWriter, r *http.Request) error {
			id, err := resourceID(r)
			if err != nil {
				return err
			}

			values, err := query(r, "scope", "limit", "cursor")
			if err != nil {
				return err
			}

			applied, err := scope(values)
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

			result, err := a.ListContentObservations(
				r.Context(),
				app.ListContentObservationsRequest{
					ContentID: base.ContentId(id),
					Scope:     applied,
					Limit:     limit,
					Cursor:    values.Get("cursor"),
				},
			)
			if err != nil {
				return err
			}

			writeJSON(w, 200, pageResponse(result))
			return nil
		},
	)
	register("/api/v1/observations/{id}", func(w http.ResponseWriter, r *http.Request) error {
		id, err := resourceID(r)
		if err != nil {
			return err
		}

		if _, err := query(r); err != nil {
			return err
		}

		result, err := a.GetObservationSummary(r.Context(), base.FileObservationId(id))
		if err != nil {
			return err
		}

		content, err := contentResponse(result.Content)
		if err != nil {
			return err
		}

		writeJSON(
			w,
			200,
			observationSummaryDTO{
				Observation:        observationResponse(result.Observation),
				Snapshot:           snapshotResponse(result.Snapshot),
				Disk:               diskResponse(result.Disk),
				Content:            content,
				OtherLocationCount: decimal(result.OtherLocationCount),
				OtherDiskCount:     decimal(result.OtherDiskCount),
			},
		)
		return nil
	})
	register("/api/v1/snapshots/{id}", func(w http.ResponseWriter, r *http.Request) error {
		id, err := resourceID(r)
		if err != nil {
			return err
		}

		if _, err := query(r); err != nil {
			return err
		}

		result, err := a.GetCompleteSnapshot(r.Context(), base.SnapshotId(id))
		if err != nil {
			return err
		}

		writeJSON(w, 200, snapshotResponse(result))
		return nil
	})
	mux.HandleFunc(
		"/",
		func(w http.ResponseWriter, r *http.Request) { writeError(w, database.ErrNotFound) },
	)
	return mux
}
