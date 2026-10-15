package store

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"regexp"
	"sort"
	"strings"
	"time"
)

// Bibliography describes the work, independently of the owned edition's date.
var bibliographySource = regexp.MustCompile(`^https://openlibrary.org/works/OL[0-9]+W$`)

type Bibliography struct {
	BookID             int64  `json:"book_id"`
	FirstPublishedYear *int   `json:"first_published_year"`
	SourceURL          string `json:"first_published_year_source_url"`
	Revision           int    `json:"revision"`
}

func ensureBibliographySchema(ctx context.Context, tx *sql.Tx) error {
	_, err := tx.ExecContext(ctx, `CREATE TABLE IF NOT EXISTS book_bibliography(book_id INTEGER PRIMARY KEY REFERENCES books(id) ON DELETE CASCADE,first_published_year INTEGER CHECK(first_published_year BETWEEN 1 AND 9999),source_url TEXT NOT NULL DEFAULT '',revision INTEGER NOT NULL DEFAULT 1)`)
	if err != nil {
		return err
	}
	var keys, linked int
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_table_info('book_bibliography') WHERE pk>0 AND name='book_id' AND pk=1`).Scan(&keys); err != nil {
		return err
	}
	if err = tx.QueryRowContext(ctx, `SELECT count(*) FROM pragma_foreign_key_list('book_bibliography') WHERE "from"='book_id' AND "table"='books' AND "to"='id' AND on_delete='CASCADE'`).Scan(&linked); err != nil {
		return err
	}
	if keys != 1 || linked != 1 {
		return errors.New("Incompatible bibliography schema: missing book key or reference")
	}
	return nil
}
func (s *Store) Bibliography(ctx context.Context, id int64) (Bibliography, error) {
	b := Bibliography{BookID: id}
	err := s.DB.QueryRowContext(ctx, `SELECT first_published_year,source_url,revision FROM book_bibliography WHERE book_id=?`, id).Scan(&b.FirstPublishedYear, &b.SourceURL, &b.Revision)
	// Read-only MCP clients can still open older catalogues; only the web app migrates.
	if err != nil && strings.Contains(err.Error(), "no such table: book_bibliography") {
		return b, nil
	}
	if errors.Is(err, sql.ErrNoRows) {
		var found int
		err = s.DB.QueryRowContext(ctx, `SELECT 1 FROM books WHERE id=?`, id).Scan(&found)
		if errors.Is(err, sql.ErrNoRows) {
			err = ErrNotFound
		}
	}
	return b, err
}
func validFirstYear(year *int) bool {
	return year == nil || (*year >= 1 && *year <= time.Now().Year()+1)
}
func (s *Store) SaveBibliography(ctx context.Context, id int64, b Bibliography) (Bibliography, error) {
	if !validFirstYear(b.FirstPublishedYear) {
		return Bibliography{}, errors.New("First publication year must be between 1 and next year")
	}
	// Manual correction removes provider provenance, including an explicit clear.
	b.SourceURL = ""

	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Bibliography{}, err
	}
	defer tx.Rollback()
	var deleted *string
	if err = tx.QueryRowContext(ctx, `SELECT deleted_at FROM books WHERE id=?`, id).Scan(&deleted); errors.Is(err, sql.ErrNoRows) {
		return Bibliography{}, ErrNotFound
	}
	if err != nil {
		return Bibliography{}, err
	}
	if deleted != nil {
		return Bibliography{}, ErrNotFound
	}

	var result sql.Result
	if b.Revision == 0 {
		result, err = tx.ExecContext(ctx, `INSERT INTO book_bibliography(book_id,first_published_year,source_url) VALUES(?,?,?) ON CONFLICT(book_id) DO NOTHING`, id, b.FirstPublishedYear, b.SourceURL)
	} else {
		result, err = tx.ExecContext(ctx, `UPDATE book_bibliography SET first_published_year=?,source_url=?,revision=revision+1 WHERE book_id=? AND revision=?`, b.FirstPublishedYear, b.SourceURL, id, b.Revision)
	}
	if err != nil {
		return Bibliography{}, err
	}
	n, _ := result.RowsAffected()
	if n != 1 {
		return Bibliography{}, ErrConflict
	}
	if err = tx.Commit(); err != nil {
		return Bibliography{}, err
	}
	return s.Bibliography(ctx, id)
}

type SubjectFacet struct {
	Name  string `json:"name"`
	Count int    `json:"count"`
}

func (s *Store) BookSubjects(ctx context.Context) ([]SubjectFacet, error) {
	rows, err := s.DB.QueryContext(ctx, `SELECT b.id,j.value FROM books b JOIN book_discovery d ON d.book_id=b.id JOIN json_each(d.subjects) j WHERE b.deleted_at IS NULL ORDER BY b.id,j.key`)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	facets := map[string]*SubjectFacet{}
	seen := map[int64]map[string]bool{}
	for rows.Next() {
		var id int64
		var name string
		if err = rows.Scan(&id, &name); err != nil {
			return nil, err
		}
		name = strings.TrimSpace(name)
		key := fold(name)
		if key == "" {
			continue
		}
		if seen[id] == nil {
			seen[id] = map[string]bool{}
		}
		if seen[id][key] {
			continue
		}
		seen[id][key] = true
		if facets[key] == nil {
			facets[key] = &SubjectFacet{Name: name}
		}
		facets[key].Count++
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	out := []SubjectFacet{}
	for _, f := range facets {
		out = append(out, *f)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count != out[j].Count {
			return out[i].Count > out[j].Count
		}
		return fold(out[i].Name) < fold(out[j].Name)
	})
	return out, nil
}

// SaveEnrichment commits provider additions together, checking every revision
// captured before the lookup. Local descriptions, notes and subjects stay intact.
func (s *Store) SaveEnrichment(ctx context.Context, id int64, d Discovery, b Bibliography, bookRevision int, discoveryChanged, yearChanged bool) (Discovery, Bibliography, error) {
	tx, err := s.DB.BeginTx(ctx, nil)
	if err != nil {
		return Discovery{}, Bibliography{}, err
	}
	defer tx.Rollback()
	var revision, discoveryRevision, bibliographyRevision int
	var deleted *string
	var existingYear *int
	err = tx.QueryRowContext(ctx, `SELECT b.revision,b.deleted_at,COALESCE(d.revision,0),COALESCE(m.revision,0),m.first_published_year FROM books b LEFT JOIN book_discovery d ON d.book_id=b.id LEFT JOIN book_bibliography m ON m.book_id=b.id WHERE b.id=?`, id).Scan(&revision, &deleted, &discoveryRevision, &bibliographyRevision, &existingYear)
	if errors.Is(err, sql.ErrNoRows) || deleted != nil {
		return Discovery{}, Bibliography{}, ErrNotFound
	}
	if err != nil {
		return Discovery{}, Bibliography{}, err
	}
	if revision != bookRevision || discoveryRevision != d.Revision || bibliographyRevision != b.Revision || (yearChanged && (existingYear != nil || bibliographyRevision > 0)) {
		return Discovery{}, Bibliography{}, ErrConflict
	}
	if discoveryChanged {
		subjects, _ := json.Marshal(cleanLabels(d.Subjects))
		tags, _ := json.Marshal(cleanLabels(d.Tags))
		_, err = tx.ExecContext(ctx, `INSERT INTO book_discovery(book_id,description,subjects,tags,notes) VALUES(?,?,?,?,?) ON CONFLICT(book_id) DO UPDATE SET description=excluded.description,subjects=excluded.subjects,tags=excluded.tags,notes=excluded.notes,revision=book_discovery.revision+1`, id, d.Description, string(subjects), string(tags), d.Notes)
		if err != nil {
			return Discovery{}, Bibliography{}, err
		}
	}
	if yearChanged {
		if b.FirstPublishedYear == nil || !validFirstYear(b.FirstPublishedYear) || !bibliographySource.MatchString(b.SourceURL) {
			return Discovery{}, Bibliography{}, errors.New("Invalid first publication year")
		}
		_, err = tx.ExecContext(ctx, `INSERT INTO book_bibliography(book_id,first_published_year,source_url) VALUES(?,?,?) ON CONFLICT(book_id) DO UPDATE SET first_published_year=excluded.first_published_year,source_url=excluded.source_url,revision=book_bibliography.revision+1`, id, b.FirstPublishedYear, b.SourceURL)
		if err != nil {
			return Discovery{}, Bibliography{}, err
		}
	}
	if err = tx.Commit(); err != nil {
		return Discovery{}, Bibliography{}, err
	}
	d, err = s.Discovery(ctx, id)
	if err != nil {
		return Discovery{}, Bibliography{}, err
	}
	b, err = s.Bibliography(ctx, id)
	return d, b, err
}
