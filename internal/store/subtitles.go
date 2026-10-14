package store

import (
	"context"
	"errors"
	"strings"
)

type SubtitleUpdate struct {
	ID       int64  `json:"id"`
	ISBN     string `json:"isbn"`
	SourceID string `json:"source_id"`
	Revision int    `json:"revision"`
	Subtitle string `json:"subtitle"`
}
type SubtitleResult struct {
	Changed int `json:"changed"`
	Skipped int `json:"skipped"`
}

// FillSubtitles is a fill-only migration: concurrent edits and edition changes
// are skipped, and existing subtitles, inventory and deletion state are retained.
func (s *Store) FillSubtitles(ctx context.Context, updates []SubtitleUpdate) (SubtitleResult, error) {
	var out SubtitleResult
	if len(updates) > 40 {
		return out, errors.New("Send at most 40 subtitles at a time")
	}
	for _, u := range updates {
		if u.ID <= 0 || u.Revision < 1 || strings.TrimSpace(u.Subtitle) == "" || len(u.Subtitle) > 1000 {
			return out, errors.New("Invalid subtitle migration item")
		}
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return out, e
	}
	defer tx.Rollback()
	for _, u := range updates {
		r, e := tx.ExecContext(ctx, `UPDATE books SET subtitle=?,revision=revision+1 WHERE id=? AND revision=? AND COALESCE(isbn,'')=? AND source_id=? AND trim(subtitle)=''`, strings.TrimSpace(u.Subtitle), u.ID, u.Revision, u.ISBN, u.SourceID)
		if e != nil {
			return SubtitleResult{}, e
		}
		n, e := r.RowsAffected()
		if e != nil {
			return SubtitleResult{}, e
		}
		if n == 1 {
			out.Changed++
		} else {
			out.Skipped++
		}
	}
	if e = tx.Commit(); e != nil {
		return SubtitleResult{}, e
	}
	return out, nil
}
