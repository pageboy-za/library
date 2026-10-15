package store

import (
	"context"
	"path/filepath"
	"reflect"
	"testing"
)

func TestMetadataBackfillPreservesPrivateDataAndIsRetrySafe(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	ctx := context.Background()
	book, err := s.Save(ctx, Input{Title: "Metadata fixture", ISBN: "9780140328721", SourceID: "OL123M", PublicationDate: "2003", RequestKey: "metadata-backfill"})
	if err != nil {
		t.Fatal(err)
	}
	private, err := s.SaveDiscovery(ctx, book.ID, Discovery{Description: "Reviewed description", Tags: []string{"Private fixture tag"}, Notes: "Private fixture note"})
	if err != nil {
		t.Fatal(err)
	}
	year := 1978
	updates := []MetadataBackfillUpdate{{ID: book.ID, ISBN: book.ISBN, SourceID: book.SourceID, Revision: book.Revision, DiscoveryRevision: private.Revision, Subjects: []string{" Science ", "SCIENCE", "History", ""}, FirstPublishedYear: &year, FirstPublishedYearSourceURL: "https://openlibrary.org/works/OL99W"}}
	got, err := s.FillMetadata(ctx, updates)
	if err != nil || got != (MetadataBackfillResult{Changed: 1, SubjectsChanged: 1, YearsChanged: 1}) {
		t.Fatal(got, err)
	}
	d, err := s.Discovery(ctx, book.ID)
	if err != nil || d.Description != private.Description || d.Notes != private.Notes || !reflect.DeepEqual(d.Tags, private.Tags) || !reflect.DeepEqual(d.Subjects, []string{"Science", "History"}) {
		t.Fatal(d, err)
	}
	actual, err := s.Book(ctx, book.ID)
	if err != nil || actual.Title != book.Title || actual.ISBN != book.ISBN || actual.SourceID != book.SourceID || actual.PublicationDate != "2003" || actual.Revision != book.Revision || !reflect.DeepEqual(actual.Copies, book.Copies) || *actual.FirstPublishedYear != year || actual.FirstPublishedYearSourceURL != "https://openlibrary.org/works/OL99W" {
		t.Fatal(actual, err)
	}
	retry, err := s.FillMetadata(ctx, updates)
	if err != nil || retry != (MetadataBackfillResult{Skipped: 1}) {
		t.Fatal(retry, err)
	}
	after, _ := s.Discovery(ctx, book.ID)
	if !reflect.DeepEqual(after, d) {
		t.Fatal("retry mutated private data", after)
	}
}
func TestMetadataBackfillSkipsStaleIdentitiesDeletedAndManualClears(t *testing.T) {
	for _, race := range []string{"bookrevision", "isbn", "sourceid", "discoveryrevision", "bibliographyrevision", "deleted", "manualclear", "existingfields"} {
		t.Run(race, func(t *testing.T) {
			s, err := Open(filepath.Join(t.TempDir(), "db"))
			if err != nil {
				t.Fatal(err)
			}
			defer s.DB.Close()
			ctx := context.Background()
			b, err := s.Save(ctx, Input{Title: "Fixture", ISBN: "9780140328721", SourceID: "OL123M", RequestKey: "metadata-guards"})
			if err != nil {
				t.Fatal(err)
			}
			year := 1978
			u := MetadataBackfillUpdate{ID: b.ID, ISBN: b.ISBN, SourceID: b.SourceID, Revision: b.Revision, Subjects: []string{"Science"}, FirstPublishedYear: &year, FirstPublishedYearSourceURL: "https://openlibrary.org/works/OL99W"}
			switch race {
			case "bookrevision":
				u.Revision++
			case "isbn":
				u.ISBN = "9781857028898"
			case "sourceid":
				u.SourceID = "OL999M"
			case "discoveryrevision":
				u.DiscoveryRevision++
			case "bibliographyrevision":
				u.BibliographyRevision++
			case "deleted":
				_, err = s.SetBookDeleted(ctx, b.ID, b.Revision, true, "metadata-deleted")
			case "manualclear":
				_, err = s.SaveBibliography(ctx, b.ID, Bibliography{})
				u.BibliographyRevision = 1
			case "existingfields":
				d, e := s.SaveDiscovery(ctx, b.ID, Discovery{Subjects: []string{"Reviewed subject"}})
				err = e
				u.DiscoveryRevision = d.Revision
				manual := 1980
				_, err = s.SaveBibliography(ctx, b.ID, Bibliography{FirstPublishedYear: &manual})
				u.BibliographyRevision = 1
			}
			if err != nil {
				t.Fatal(err)
			}
			result, err := s.FillMetadata(ctx, []MetadataBackfillUpdate{u})
			if err != nil {
				t.Fatal(result, err)
			}
			if race == "manualclear" {
				if result != (MetadataBackfillResult{Changed: 1, SubjectsChanged: 1}) {
					t.Fatal(result)
				}
				meta, _ := s.Bibliography(ctx, b.ID)
				if meta.FirstPublishedYear != nil || meta.Revision != 1 {
					t.Fatal("manual clear overwritten", meta)
				}
			} else if result != (MetadataBackfillResult{Skipped: 1}) {
				t.Fatal(result)
			}
			if race != "manualclear" && race != "existingfields" {
				d, _ := s.Discovery(ctx, b.ID)
				meta, _ := s.Bibliography(ctx, b.ID)
				if len(d.Subjects) > 0 || meta.FirstPublishedYear != nil {
					t.Fatal("stale update wrote data", d, meta)
				}
			}
		})
	}
}
func TestMetadataBackfillRejectsWholeInvalidBatch(t *testing.T) {
	s, err := Open(filepath.Join(t.TempDir(), "db"))
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	ctx := context.Background()
	b, err := s.Save(ctx, Input{Title: "Fixture", RequestKey: "metadata-validation"})
	if err != nil {
		t.Fatal(err)
	}
	year := 1978
	valid := MetadataBackfillUpdate{ID: b.ID, Revision: b.Revision, Subjects: []string{"Science"}, FirstPublishedYear: &year, FirstPublishedYearSourceURL: "https://openlibrary.org/works/OL99W"}
	for _, name := range []string{"year", "source", "nullsource", "labels", "duplicate", "oversized", "revision"} {
		t.Run(name, func(t *testing.T) {
			bad := valid
			bad.ID = 999
			batch := []MetadataBackfillUpdate{valid, bad}
			switch name {
			case "year":
				n := 9999
				batch[1].FirstPublishedYear = &n
			case "source":
				batch[1].FirstPublishedYearSourceURL = "https://evil.invalid/works/OL99W"
			case "nullsource":
				batch[1].FirstPublishedYear = nil
			case "labels":
				batch[1].Subjects = make([]string, 101)
			case "duplicate":
				batch[1].ID = valid.ID
			case "oversized":
				batch = make([]MetadataBackfillUpdate, 21)
			case "revision":
				batch[1].DiscoveryRevision = -1
			}
			if result, err := s.FillMetadata(ctx, batch); err == nil || result != (MetadataBackfillResult{}) {
				t.Fatal("invalid batch accepted", result, err)
			}
			d, _ := s.Discovery(ctx, b.ID)
			meta, _ := s.Bibliography(ctx, b.ID)
			if len(d.Subjects) > 0 || meta.Revision != 0 {
				t.Fatal("partial write", d, meta)
			}
		})
	}
}
