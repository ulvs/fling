package api

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/ulvs/fling/gateway/internal/storage"
)

const (
	// abandonedUpload: an upload URL lasts 15 minutes, so an upload still pending after this never finished.
	abandonedUpload = 24 * time.Hour
	cleanupBatch    = 500
)

// DeleteExpiredFiles removes files from storage that nobody can download any more: their last
// link ended (expired or turned off) DeleteAfter ago, or the upload never finished. It returns
// how many it deleted; storage.ErrAccessDenied means the storage key may not delete.
func (s *Server) DeleteExpiredFiles(ctx context.Context) (int, error) {
	if !s.DeleteFiles {
		return 0, nil
	}
	now := s.Now()
	files, err := s.Store.FilesToDelete(ctx, now.Add(-s.DeleteAfter), now.Add(-abandonedUpload), cleanupBatch)
	if err != nil {
		return 0, err
	}
	deleted := 0
	for _, f := range files {
		if err := s.Objects.Delete(ctx, f.ObjectKey); err != nil {
			if errors.Is(err, storage.ErrAccessDenied) {
				return deleted, err
			}
			s.Log.Warn("delete file from storage", "err", err)
			continue
		}
		if err := s.Store.MarkFileDeleted(ctx, f.ID, now); err != nil {
			return deleted, err
		}
		deleted++
	}
	if deleted > 0 {
		s.Log.Info("deleted files nobody can download any more", "count", deleted)
	}
	return deleted, nil
}

// humanDelay: 24h -> "a day", 48h -> "2 days", 6h -> "6 hours".
func humanDelay(d time.Duration) string {
	switch {
	case d == 24*time.Hour:
		return "a day"
	case d%(24*time.Hour) == 0:
		return strconv.Itoa(int(d/(24*time.Hour))) + " days"
	case d == time.Hour:
		return "an hour"
	case d%time.Hour == 0:
		return strconv.Itoa(int(d.Hours())) + " hours"
	}
	return d.String()
}
