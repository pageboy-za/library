package store

import (
	"context"
	"database/sql"
	"fmt"
)

type CoverAsset struct {
	ID              int64  `json:"id"`
	BookID          int64  `json:"book_id"`
	LocalPath       string `json:"-"`
	URL             string `json:"url"`
	SourceKind      string `json:"source_kind"`
	SourceCoverID   *int64 `json:"source_cover_id"`
	SourceEditionID string `json:"source_edition_id"`
	Checksum        string `json:"checksum"`
	Selected        bool   `json:"selected"`
	ThumbnailURL    string `json:"thumbnail_url"`
	Width           int    `json:"width,omitempty"`
	Height          int    `json:"height,omitempty"`
	ThumbnailWidth  int    `json:"thumbnail_width,omitempty"`
	ThumbnailHeight int    `json:"thumbnail_height,omitempty"`
}

func (s *Store) Covers(ctx context.Context, bookID int64) ([]CoverAsset, error) {
	rows, e := s.DB.QueryContext(ctx, `SELECT a.id,a.book_id,a.local_path,a.source_kind,a.source_cover_id,coalesce(a.source_edition_id,''),a.checksum,coalesce(b.selected_cover_asset_id=a.id,0) FROM cover_assets a JOIN books b ON b.id=a.book_id WHERE a.book_id=? ORDER BY a.id`, bookID)
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	out := []CoverAsset{}
	for rows.Next() {
		var a CoverAsset
		if e = rows.Scan(&a.ID, &a.BookID, &a.LocalPath, &a.SourceKind, &a.SourceCoverID, &a.SourceEditionID, &a.Checksum, &a.Selected); e != nil {
			return nil, e
		}
		a.URL = "/covers/" + a.LocalPath
		a.ThumbnailURL = coverThumbnailURL(a.BookID, a.ID)
		out = append(out, a)
	}
	return out, rows.Err()
}

// SaveCover associates a validated immutable file and optionally selects it. A
// background fetch only fills a missing selection and never replaces user choice.
func (s *Store) SaveCover(ctx context.Context, a CoverAsset, selectCover, onlyMissing bool) (CoverAsset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, e := s.DB.BeginTx(ctx, nil)
	if e != nil {
		return a, e
	}
	defer tx.Rollback()
	_, e = tx.ExecContext(ctx, `INSERT INTO cover_assets(book_id,local_path,source_kind,source_cover_id,source_edition_id,checksum) VALUES(?,?,?,?,?,?) ON CONFLICT(book_id,checksum) DO NOTHING`, a.BookID, a.LocalPath, a.SourceKind, a.SourceCoverID, a.SourceEditionID, a.Checksum)
	if e != nil {
		return a, e
	}
	if e = tx.QueryRowContext(ctx, `SELECT id FROM cover_assets WHERE book_id=? AND checksum=?`, a.BookID, a.Checksum).Scan(&a.ID); e != nil {
		return a, e
	}
	if selectCover {
		q := `UPDATE books SET selected_cover_asset_id=?,revision=revision+1 WHERE id=?`
		if onlyMissing {
			q += ` AND selected_cover_asset_id IS NULL`
		}
		if _, e = tx.ExecContext(ctx, q, a.ID, a.BookID); e != nil {
			return a, e
		}
	}
	e = tx.Commit()
	a.URL = "/covers/" + a.LocalPath
	a.ThumbnailURL = coverThumbnailURL(a.BookID, a.ID)
	return a, e
}
func coverThumbnailURL(bookID, assetID int64) string {
	return fmt.Sprintf("/api/books/%d/covers/%d/thumbnail?v=1", bookID, assetID)
}
func (s *Store) SelectedCoverThumbnailURL(ctx context.Context, bookID int64) (string, error) {
	var id int64
	e := s.DB.QueryRowContext(ctx, `SELECT a.id FROM books b JOIN cover_assets a ON a.id=b.selected_cover_asset_id WHERE b.id=? AND a.book_id=b.id`, bookID).Scan(&id)
	if e == sql.ErrNoRows {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	return coverThumbnailURL(bookID, id), nil
}

func (s *Store) SelectCover(ctx context.Context, bookID, assetID int64) error {
	result, e := s.DB.ExecContext(ctx, `UPDATE books SET selected_cover_asset_id=?,revision=revision+1 WHERE id=? AND EXISTS(SELECT 1 FROM cover_assets WHERE id=? AND book_id=?)`, assetID, bookID, assetID, bookID)
	if e != nil {
		return e
	}
	n, e := result.RowsAffected()
	if e == nil && n == 0 {
		return ErrNotFound
	}
	return e
}
func (s *Store) SelectedCoverURL(ctx context.Context, bookID int64) (string, error) {
	var path string
	e := s.DB.QueryRowContext(ctx, `SELECT a.local_path FROM books b JOIN cover_assets a ON a.id=b.selected_cover_asset_id WHERE b.id=? AND a.book_id=b.id`, bookID).Scan(&path)
	if e == sql.ErrNoRows {
		return "", nil
	}
	if e != nil {
		return "", e
	}
	return "/covers/" + path, nil
}
