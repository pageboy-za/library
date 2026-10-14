package store

import (
	"context"
	"errors"
)

type Preferences struct {
	DefaultShelfID *int64 `json:"default_shelf_id"`
	Revision       int    `json:"revision"`
}

func (s *Store) Preferences(ctx context.Context) (Preferences, error) {
	var p Preferences
	err := s.DB.QueryRowContext(ctx, "SELECT default_shelf_id,revision FROM library_preferences WHERE id=1").Scan(&p.DefaultShelfID, &p.Revision)
	return p, err
}

func (s *Store) SavePreferences(ctx context.Context, id *int64, revision int) (Preferences, error) {
	if revision <= 0 || (id != nil && *id <= 0) {
		return Preferences{}, errors.New("Choose an existing shelf")
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Preferences{}, err
	}
	defer tx.Rollback()
	if id != nil {
		var count int
		if err = tx.QueryRowContext(ctx, "SELECT COUNT(*) FROM shelves WHERE id=?", *id).Scan(&count); err != nil {
			return Preferences{}, err
		}
		if count != 1 {
			return Preferences{}, errors.New("This shelf is no longer available")
		}
	}
	result, err := tx.ExecContext(ctx, "UPDATE library_preferences SET default_shelf_id=?,revision=revision+1 WHERE id=1 AND revision=?", id, revision)
	if err != nil {
		return Preferences{}, err
	}
	changed, err := result.RowsAffected()
	if err != nil {
		return Preferences{}, err
	}
	if changed != 1 {
		return Preferences{}, ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return Preferences{}, err
	}
	return Preferences{DefaultShelfID: id, Revision: revision + 1}, nil
}
