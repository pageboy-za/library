package store

import (
	"context"
	"errors"
	"path/filepath"
	"reflect"
	"testing"
)

func TestBibliographyLegacyMigrationAndCorrections(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	db := legacyFixture(t, path)
	mustExecMigration(t, db, `INSERT INTO books(id,title,authors,publication_date) VALUES(1,'Fixture','[]','2003')`)
	mustExecMigration(t, db, `INSERT INTO copies(book_id,label) VALUES(1,'Original')`)
	db.Close()
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	ctx := context.Background()
	before, err := s.Book(ctx, 1)
	if err != nil || before.FirstPublishedYear != nil || before.BibliographyRevision != 0 || before.Count != 1 {
		t.Fatal(before, err)
	}
	year := 1978
	b, err := s.SaveBibliography(ctx, 1, Bibliography{FirstPublishedYear: &year})
	if err != nil || b.Revision != 1 || b.SourceURL != "" {
		t.Fatal(b, err)
	}
	if _, err = s.SaveBibliography(ctx, 1, Bibliography{FirstPublishedYear: &year}); !errors.Is(err, ErrConflict) {
		t.Fatal("stale write accepted", err)
	}
	bad := 0
	if _, err = s.SaveBibliography(ctx, 1, Bibliography{FirstPublishedYear: &bad, Revision: 1}); err == nil {
		t.Fatal("invalid year accepted")
	}
	got, err := s.Book(ctx, 1)
	if err != nil || got.PublicationDate != "2003" || got.Count != 1 || got.Revision != before.Revision || *got.FirstPublishedYear != year {
		t.Fatal(got, err)
	}
	cleared, err := s.SaveBibliography(ctx, 1, Bibliography{Revision: b.Revision})
	if err != nil || cleared.FirstPublishedYear != nil || cleared.Revision != 2 {
		t.Fatal(cleared, err)
	}
}
func TestCatalogueSubjectsAndStableYearSorting(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	ctx := context.Background()
	books := []Book{}
	for _, in := range []Input{{Title: "Beta", Authors: []string{"Zeta"}, PublicationDate: "June 2005", RequestKey: "sort-fixture-one"}, {Title: "Alpha", Authors: []string{"Ada"}, PublicationDate: "1991-01-01", RequestKey: "sort-fixture-two"}, {Title: "Alpha", PublicationDate: "unknown", RequestKey: "sort-fixture-three"}, {Title: "Deleted", RequestKey: "sort-fixture-four"}} {
		b, e := s.Save(ctx, in)
		if e != nil {
			t.Fatal(e)
		}
		books = append(books, b)
	}
	for i, labels := range [][]string{{"Science", "ＳＣＩＥＮＣＥ", "History"}, {"science"}, {"History"}, {"Deleted-only", "Science"}} {
		if _, err = s.SaveDiscovery(ctx, books[i].ID, Discovery{Subjects: labels}); err != nil {
			t.Fatal(err)
		}
	}
	if _, err = s.SetBookDeleted(ctx, books[3].ID, books[3].Revision, true, "delete-subject-fixture"); err != nil {
		t.Fatal(err)
	}
	for i, year := range []int{1980, 1970} {
		if _, err = s.SaveBibliography(ctx, books[i].ID, Bibliography{FirstPublishedYear: &year}); err != nil {
			t.Fatal(err)
		}
	}
	facets, err := s.BookSubjects(ctx)
	if err != nil || !reflect.DeepEqual(facets, []SubjectFacet{{Name: "History", Count: 2}, {Name: "Science", Count: 2}}) {
		t.Fatal(facets, err)
	}
	for _, tc := range []struct {
		options ListOptions
		want    []int64
	}{{ListOptions{Subject: " ＳＣＩＥＮＣＥ "}, []int64{books[1].ID, books[0].ID}}, {ListOptions{Sort: "author"}, []int64{books[1].ID, books[0].ID, books[2].ID}}, {ListOptions{Sort: "published_newest"}, []int64{books[0].ID, books[1].ID, books[2].ID}}, {ListOptions{Sort: "first_published_oldest"}, []int64{books[1].ID, books[0].ID, books[2].ID}}, {ListOptions{Sort: "first_published_newest"}, []int64{books[0].ID, books[1].ID, books[2].ID}}, {ListOptions{Subject: "x' OR 1=1 --"}, []int64{}}} {
		got, total, e := s.ListWithOptions(ctx, "", 0, "", 1, tc.options)
		ids := []int64{}
		for _, b := range got {
			ids = append(ids, b.ID)
		}
		if e != nil || total != len(tc.want) || !reflect.DeepEqual(ids, tc.want) {
			t.Fatalf("%+v: %v total%d err%v", tc.options, ids, total, e)
		}
	}
	if _, _, err = s.ListWithOptions(ctx, "", 0, "", 1, ListOptions{Sort: "title;DROP TABLE books"}); err == nil {
		t.Fatal("sort injection accepted")
	}
	got, _, err := s.ListWithOptions(ctx, "Alpha", 0, "", 1, ListOptions{})
	if err != nil || len(got) != 2 || got[0].ID != books[1].ID || got[1].ID != books[2].ID {
		t.Fatal("unstable ties", got, err)
	}
}
func TestEnrichmentTransactionProtectsEveryRevision(t *testing.T) {
	for _, race := range []string{"book", "discovery", "bibliography"} {
		t.Run(race, func(t *testing.T) {
			s, err := Open(filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.DB.Close()
			ctx := context.Background()
			book, err := s.Save(ctx, Input{Title: "Fixture", RequestKey: "enrichment-revisions"})
			if err != nil {
				t.Fatal(err)
			}
			d, _ := s.Discovery(ctx, book.ID)
			b, _ := s.Bibliography(ctx, book.ID)
			year := 1977
			b.FirstPublishedYear = &year
			b.SourceURL = "https://openlibrary.org/works/OL1W"
			d.Description = "Provider description"
			d.Subjects = []string{"Science"}
			switch race {
			case "book":
				_, err = s.Edit(ctx, book.ID, Input{Title: "Edited", Revision: book.Revision})
			case "discovery":
				_, err = s.SaveDiscovery(ctx, book.ID, Discovery{Notes: "Private concurrent note"})
			case "bibliography":
				manual := 1980
				_, err = s.SaveBibliography(ctx, book.ID, Bibliography{FirstPublishedYear: &manual})
			}
			if err != nil {
				t.Fatal(err)
			}
			if _, _, err = s.SaveEnrichment(ctx, book.ID, d, b, book.Revision, true, true); !errors.Is(err, ErrConflict) {
				t.Fatal(err)
			}
			actual, _ := s.Discovery(ctx, book.ID)
			got, _ := s.Bibliography(ctx, book.ID)
			if actual.Description != "" || len(actual.Subjects) > 0 || (race != "bibliography" && got.FirstPublishedYear != nil) {
				t.Fatal("partial provider write", actual, got)
			}
		})
	}
}

func TestIncompatibleBibliographySchemaRollsBack(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	db := legacyFixture(t, path)
	mustExecMigration(t, db, `CREATE TABLE book_bibliography(book_id INTEGER,first_published_year INTEGER,source_url TEXT,revision INTEGER)`)
	db.Close()
	if s, err := Open(path); err == nil {
		s.DB.Close()
		t.Fatal("incompatible feature table accepted")
	}
}
