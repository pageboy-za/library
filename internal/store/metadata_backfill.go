package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
)

// MetadataBackfillUpdate is an operator-reviewed, identity-bound addition. It
// contains no description, notes, borrower details or inventory mutations.
type MetadataBackfillUpdate struct {
	ID                          int64    `json:"id"`
	ISBN                        string   `json:"isbn"`
	SourceID                    string   `json:"source_id"`
	Revision                    int      `json:"revision"`
	DiscoveryRevision           int      `json:"discovery_revision"`
	BibliographyRevision        int      `json:"bibliography_revision"`
	Subjects                    []string `json:"subjects"`
	FirstPublishedYear          *int     `json:"first_published_year"`
	FirstPublishedYearSourceURL string   `json:"first_published_year_source_url"`
}
type MetadataBackfillResult struct {
	Changed         int `json:"changed"`
	SubjectsChanged int `json:"subjects_changed"`
	YearsChanged    int `json:"years_changed"`
	Skipped         int `json:"skipped"`
}

// FillMetadata validates the complete batch before writing, then fills only
// empty fields from the reviewed plan. Retrying a committed batch is a no-op.
func (s *Store) FillMetadata(ctx context.Context, updates []MetadataBackfillUpdate) (MetadataBackfillResult, error) {
	out := MetadataBackfillResult{}
	if len(updates) > 20 {
		return out, errors.New("Send at most 20 metadata updates at a time")
	}
	seen := map[int64]bool{}
	cleaned := make([]MetadataBackfillUpdate, len(updates))
	for i, u := range updates {
		if u.ID < 1 || u.Revision < 1 || u.DiscoveryRevision < 0 || u.BibliographyRevision < 0 || len(u.ISBN) > 100 || len(u.SourceID) > 200 || seen[u.ID] {
			return out, errors.New("Invalid or repeated metadata update")
		}
		seen[u.ID] = true
		if len(u.Subjects) > 100 {
			return out, errors.New("Use at most 100 subjects per book")
		}
		for _, label := range u.Subjects {
			if len(label) > 200 {
				return out, errors.New("Keep each subject under 200 characters")
			}
		}
		if u.FirstPublishedYear != nil {
			if !validFirstYear(u.FirstPublishedYear) || !bibliographySource.MatchString(u.FirstPublishedYearSourceURL) {
				return out, errors.New("Invalid first publication year or linked work source")
			}
		} else if u.FirstPublishedYearSourceURL != "" {
			return out, errors.New("A publication source requires a year")
		}
		u.Subjects = cleanLabels(u.Subjects)
		cleaned[i] = u
	}
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return out, err
	}
	defer tx.Rollback()
	for _, u := range cleaned {
		var isbn, sourceID, rawSubjects string
		var deleted *string
		var revision, discoveryRevision, bibliographyRevision int
		var year *int
		err = tx.QueryRowContext(ctx, `SELECT COALESCE(b.isbn,''),b.source_id,b.revision,b.deleted_at,COALESCE(d.revision,0),COALESCE(d.subjects,'[]'),COALESCE(m.revision,0),m.first_published_year FROM books b LEFT JOIN book_discovery d ON d.book_id=b.id LEFT JOIN book_bibliography m ON m.book_id=b.id WHERE b.id=?`, u.ID).Scan(&isbn, &sourceID, &revision, &deleted, &discoveryRevision, &rawSubjects, &bibliographyRevision, &year)
		if errors.Is(err, sql.ErrNoRows) {
			out.Skipped++
			continue
		}
		if err != nil {
			return MetadataBackfillResult{}, err
		}
		if deleted != nil || isbn != u.ISBN || sourceID != u.SourceID || revision != u.Revision || discoveryRevision != u.DiscoveryRevision || bibliographyRevision != u.BibliographyRevision {
			out.Skipped++
			continue
		}
		var subjects []string
		if err = json.Unmarshal([]byte(rawSubjects), &subjects); err != nil {
			return MetadataBackfillResult{}, err
		}
		changeSubjects := len(subjects) == 0 && len(u.Subjects) > 0
		// Any existing bibliography row with a nil year represents an explicit clear.
		changeYear := year == nil && bibliographyRevision == 0 && u.FirstPublishedYear != nil
		if !changeSubjects && !changeYear {
			out.Skipped++
			continue
		}
		if changeSubjects {
			raw, _ := json.Marshal(u.Subjects)
			_, err = tx.ExecContext(ctx, `INSERT INTO book_discovery(book_id,subjects) VALUES(?,?) ON CONFLICT(book_id) DO UPDATE SET subjects=excluded.subjects,revision=book_discovery.revision+1`, u.ID, string(raw))
			if err != nil {
				return MetadataBackfillResult{}, err
			}
			out.SubjectsChanged++
		}
		if changeYear {
			_, err = tx.ExecContext(ctx, `INSERT INTO book_bibliography(book_id,first_published_year,source_url) VALUES(?,?,?)`, u.ID, u.FirstPublishedYear, u.FirstPublishedYearSourceURL)
			if err != nil {
				return MetadataBackfillResult{}, err
			}
			out.YearsChanged++
		}
		out.Changed++
	}
	if err = tx.Commit(); err != nil {
		return MetadataBackfillResult{}, err
	}
	return out, nil
}
