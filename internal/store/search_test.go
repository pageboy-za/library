package store

import (
	"context"
	"encoding/json"
	"fmt"
	"math"
	"path/filepath"
	"strings"
	"testing"
)

func TestSQLSearchUnicodeLiteralFiltersAndPagination(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	ctx := context.Background()
	s.AddShelf(ctx, "Study", "One")
	s.AddShelf(ctx, "Study", "Two")
	shelves, _ := s.Shelves(ctx)
	titles := []string{"Ｆｕｌｌｗｉｄｔｈ café", "100% _ literal", "Zulu", "A book"}
	for i, title := range titles {
		b, e := s.Save(ctx, Input{Title: title, Authors: []string{"First Author", "Second Author"}, RequestKey: fmt.Sprintf("search-book-%d", i), ShelfID: &shelves[0].ID})
		if e != nil {
			t.Fatal(e)
		}
		if i == 0 {
			if _, e = s.AddCopy(ctx, b.ID, &shelves[1].ID, "available-copy"); e != nil {
				t.Fatal(e)
			}
			if _, e = s.DB.Exec("INSERT INTO borrowers(id,name) VALUES(1,'Reader')"); e != nil {
				t.Fatal(e)
			}
			if _, e = s.DB.Exec("INSERT INTO loans(copy_id,borrower_id) VALUES(?,1)", b.Copies[0].ID); e != nil {
				t.Fatal(e)
			}
		}
		if i == 2 {
			s.DB.Exec("UPDATE copies SET archived_at=CURRENT_TIMESTAMP WHERE book_id=?", b.ID)
		}
	}
	cases := []struct {
		q            string
		shelf        int64
		availability string
		total        int
	}{{"fullwidth CAFÉ", 0, "", 1}, {"author second", 0, "", 4}, {"% _", 0, "", 1}, {"' OR 1=1 --", 0, "", 0}, {"", 0, "available", 3}, {"", shelves[0].ID, "available", 3}, {"", shelves[1].ID, "available", 1}, {"Zulu", shelves[0].ID, "", 0}, {"", 0, "", 4}}
	for _, c := range cases {
		books, total, e := s.List(ctx, c.q, c.shelf, c.availability, 1)
		if e != nil || total != c.total || len(books) != c.total {
			t.Fatalf("query %+v total=%d books=%d error=%v", c, total, len(books), e)
		}
	}
	for _, page := range []int{math.MaxInt, 2, 999} {
		books, total, e := s.List(ctx, "", 0, "", page)
		if e != nil || total != 4 || len(books) != 0 {
			t.Fatalf("page %d %v %d %v", page, books, total, e)
		}
	}
}
func seedSearchScale(t testing.TB, s *Store, count int) {
	t.Helper()
	tx, e := s.DB.Begin()
	if e != nil {
		t.Fatal(e)
	}
	defer tx.Rollback()
	for i := 0; i < count; i++ {
		authors, _ := json.Marshal([]string{"Writer", "Jane Café"})
		r, e := tx.Exec("INSERT INTO books(title,authors) VALUES(?,?)", fmt.Sprintf("Book %05d", i), string(authors))
		if e != nil {
			t.Fatal(e)
		}
		id, _ := r.LastInsertId()
		if _, e = tx.Exec("INSERT INTO copies(book_id) VALUES(?)", id); e != nil {
			t.Fatal(e)
		}
	}
	if e = tx.Commit(); e != nil {
		t.Fatal(e)
	}
}
func TestSQLSearchScalePageBoundary(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	seedSearchScale(t, s, 1001)
	ctx := context.Background()
	books, total, e := s.List(ctx, "", 0, "", 42)
	if e != nil || total != 1001 || len(books) != 17 || books[0].Title != "Book 00984" || books[16].Title != "Book 01000" {
		t.Fatalf("last page len=%d total=%d error=%v", len(books), total, e)
	}
	books, total, e = s.List(ctx, "jane café", 0, "available", 2)
	if e != nil || total != 1001 || len(books) != 24 || books[0].Title != "Book 00024" {
		t.Fatalf("filtered page len=%d total=%d error=%v", len(books), total, e)
	}
	var plan string
	rows, e := s.DB.QueryContext(ctx, "EXPLAIN QUERY PLAN SELECT id FROM books b WHERE EXISTS(SELECT 1 FROM copies c WHERE c.book_id=b.id AND c.archived_at IS NULL)")
	if e != nil {
		t.Fatal(e)
	}
	for rows.Next() {
		var id, parent, unused int
		var detail string
		rows.Scan(&id, &parent, &unused, &detail)
		plan += detail + "\n"
	}
	rows.Close()
	if !strings.Contains(plan, "copy_book") {
		t.Fatalf("missing copy index in query plan: %s", plan)
	}
}
func BenchmarkSQLCataloguePage(b *testing.B) {
	s, e := Open(filepath.Join(b.TempDir(), "db"))
	if e != nil {
		b.Fatal(e)
	}
	defer s.DB.Close()
	seedSearchScale(b, s, 5000)
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		books, total, e := s.List(context.Background(), "jane café", 0, "available", 20)
		if e != nil || total != 5000 || len(books) != 24 {
			b.Fatal(e, total, len(books))
		}
	}
}

func TestTrigramBackfillTriggersAndLiteralEquivalence(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	// Start without the derived index to exercise backfill of existing books.
	for _, statement := range []string{"DROP TRIGGER book_search_ai", "DROP TRIGGER book_search_au", "DROP TRIGGER book_search_ad", "DROP TABLE book_search"} {
		if _, e = s.DB.Exec(statement); e != nil {
			t.Fatal(e)
		}
	}
	ctx := context.Background()
	titles := []string{"Ｆｕｌｌｗｉｄｔｈ CAFÉ", "Quoted \"NEAR\" title", "literal %_ [*] ?", "東京書店について", "before\x00after", "éclair e\u0301clair", "emoji 📚📗📘 books", "abc AND NOT OR xyz", "abc  def", "repeat ababab", "long string ending"}
	for i, title := range titles {
		if _, e = s.Save(ctx, Input{Title: title, Authors: []string{"First Author", "Second Author"}, RequestKey: fmt.Sprintf("trigram-book-%d", i)}); e != nil {
			t.Fatal(e)
		}
	}
	if e = EnsureSearchSchema(s); e != nil {
		t.Fatal(e)
	}
	var indexed int
	if e = s.DB.QueryRow("SELECT count(*) FROM book_search").Scan(&indexed); e != nil || indexed != len(titles) {
		t.Fatalf("index rows %d %v", indexed, e)
	}
	queries := []string{"fullwidth CAFÉ", `"NEAR"`, `%_ [*]`, `[*]`, `?`, `東京書`, `📚📗📘`, `abc AND NOT`, `abc  def`, `ababab`, `after`, `before`, `e\u0301clair`, "e\u0301clair", `long string ending`, `author second`, `a`, `ab`, "\x00", "before\x00after", ` OR `, `"`, strings.Repeat("x", 33000)}
	all, _, e := s.List(ctx, "", 0, "", 1)
	if e != nil {
		t.Fatal(e)
	}
	for _, q := range queries {
		want := []int64{}
		for _, b := range all {
			if strings.Contains(fold(b.Title+" "+strings.Join(b.Authors, " ")+" "+b.ISBN), fold(q)) {
				want = append(want, b.ID)
			}
		}
		got, total, e := s.List(ctx, q, 0, "", 1)
		if e != nil || total != len(want) || len(got) != len(want) {
			t.Fatalf("literal %q expected %v got %d/%d err %v", q, want, len(got), total, e)
		}
		for i := range got {
			if got[i].ID != want[i] {
				t.Fatalf("order %q", q)
			}
		}
	}
	inserted, e := s.Save(ctx, Input{Title: "Inserted after backfill", Authors: []string{"New author"}, RequestKey: "insert-after-index"})
	if e != nil {
		t.Fatal(e)
	}
	got, total, e := s.List(ctx, "inserted after", 0, "", 1)
	if e != nil || total != 1 || len(got) != 1 || got[0].ID != inserted.ID {
		t.Fatal("insert trigger", got, total, e)
	}
	if _, e = s.DB.Exec("DELETE FROM copies WHERE book_id=?", inserted.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("DELETE FROM books WHERE id=?", inserted.ID); e != nil {
		t.Fatal(e)
	}
	changed := all[0]
	if _, e = s.DB.Exec("UPDATE books SET title=?,authors=?,isbn=? WHERE id=?", "New distinct marker", `["Updated Writer"]`, "9791234567899", changed.ID); e != nil {
		t.Fatal(e)
	}
	for _, q := range []string{"distinct marker", "updated writer", "9791234567899"} {
		got, total, e := s.List(ctx, q, 0, "", 1)
		if e != nil || total != 1 || got[0].ID != changed.ID {
			t.Fatal(q, got, total, e)
		}
	}
	if _, e = s.DB.Exec("DELETE FROM copies WHERE book_id=?", changed.ID); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("DELETE FROM books WHERE id=?", changed.ID); e != nil {
		t.Fatal(e)
	}
	var count int
	s.DB.QueryRow("SELECT count(*) FROM book_search WHERE rowid=?", changed.ID).Scan(&count)
	if count != 0 {
		t.Fatal("delete trigger left row")
	}
	if _, e = s.DB.Exec("DELETE FROM book_search"); e != nil {
		t.Fatal(e)
	}
	if e = s.RebuildSearchIndex(ctx); e != nil {
		t.Fatal(e)
	}
	s.DB.QueryRow("SELECT count(*) FROM book_search").Scan(&count)
	if count != len(titles)-1 {
		t.Fatal("rebuild missing rows", count)
	}
	if _, e = s.DB.Exec("DROP TRIGGER book_search_au"); e != nil {
		t.Fatal(e)
	}
	if _, e = s.DB.Exec("UPDATE books SET title='Recovered after dropped trigger' WHERE id=?", all[1].ID); e != nil {
		t.Fatal(e)
	}
	if e = EnsureSearchSchema(s); e != nil {
		t.Fatal(e)
	}
	_, count, e = s.List(ctx, "recovered after", 0, "", 1)
	if e != nil || count != 1 {
		t.Fatal("trigger repair/backfill", count, e)
	}
	for _, q := range []string{"DROP TRIGGER book_search_ai", "DROP TRIGGER book_search_au", "DROP TRIGGER book_search_ad", "DROP TABLE book_search"} {
		if _, e = s.DB.Exec(q); e != nil {
			t.Fatal(e)
		}
	}
	got, total, e = s.List(ctx, "recovered after", 0, "", 1)
	if e != nil || total != 1 || len(got) != 1 {
		t.Fatal("no index fallback", got, total, e)
	}
}
func TestTrigramLargeQueryPlan(t *testing.T) {
	s, e := Open(filepath.Join(t.TempDir(), "db"))
	if e != nil {
		t.Fatal(e)
	}
	defer s.DB.Close()
	seedSearchScale(t, s, 5000)
	if e = EnsureSearchSchema(s); e != nil {
		t.Fatal(e)
	}
	ctx := context.Background()
	got, total, e := s.List(ctx, "Book 03999", 0, "available", 1)
	if e != nil || total != 1 || got[0].Title != "Book 03999" {
		t.Fatal(got, total, e)
	}
	rows, e := s.DB.QueryContext(ctx, `EXPLAIN QUERY PLAN SELECT b.id FROM books b WHERE b.id IN(SELECT rowid FROM book_search WHERE book_search MATCH ?) AND instr(library_search_text(CAST(b.title AS BLOB),CAST(b.authors AS BLOB),CAST(b.isbn AS BLOB)),?)>0`, `"book 03999"`, "book 03999")
	if e != nil {
		t.Fatal(e)
	}
	plan := ""
	for rows.Next() {
		var id, parent, unused int
		var detail string
		rows.Scan(&id, &parent, &unused, &detail)
		plan += detail + "\n"
	}
	rows.Close()
	if !strings.Contains(plan, "VIRTUAL TABLE INDEX") || !strings.Contains(plan, "book_search") {
		t.Fatalf("trigram index not used: %s", plan)
	}
	t.Log(plan)
}
func BenchmarkTrigramSelectiveCataloguePage(b *testing.B) {
	s, e := Open(filepath.Join(b.TempDir(), "db"))
	if e != nil {
		b.Fatal(e)
	}
	defer s.DB.Close()
	seedSearchScale(b, s, 5000)
	if e = EnsureSearchSchema(s); e != nil {
		b.Fatal(e)
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		books, total, e := s.List(context.Background(), "Book 03999", 0, "available", 1)
		if e != nil || total != 1 || len(books) != 1 {
			b.Fatal(e, total, len(books))
		}
	}
}

func BenchmarkLiteralSelectiveCataloguePage(b *testing.B) {
	s, e := Open(filepath.Join(b.TempDir(), "db"))
	if e != nil {
		b.Fatal(e)
	}
	defer s.DB.Close()
	seedSearchScale(b, s, 5000)
	for _, statement := range []string{"DROP TRIGGER book_search_ai", "DROP TRIGGER book_search_au", "DROP TRIGGER book_search_ad", "DROP TABLE book_search"} {
		if _, e = s.DB.Exec(statement); e != nil {
			b.Fatal(e)
		}
	}
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		books, total, e := s.List(context.Background(), "Book 03999", 0, "available", 1)
		if e != nil || total != 1 || len(books) != 1 {
			b.Fatal(e, total, len(books))
		}
	}
}

func TestStartupRepairsLegacyCastNormalizationTriggers(t *testing.T) {
	path := filepath.Join(t.TempDir(), "library.sqlite")
	s, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	ctx := context.Background()
	for _, name := range []string{"book_search_ai", "book_search_au", "book_search_ad"} {
		if _, err = s.DB.Exec("DROP TRIGGER " + name); err != nil {
			t.Fatal(err)
		}
	}
	for _, definition := range []string{searchInsertTrigger, searchUpdateTrigger, searchDeleteTrigger} {
		if _, err = s.DB.Exec(strings.ReplaceAll(definition, "library_search_index", "library_search_text")); err != nil {
			t.Fatal(err)
		}
	}
	book, err := s.Save(ctx, Input{Title: "before\x00after", Authors: []string{}, RequestKey: "legacy-cast-book"})
	if err != nil {
		t.Fatal(err)
	}
	if err = s.DB.Close(); err != nil {
		t.Fatal(err)
	}
	s, err = Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer s.DB.Close()
	books, total, err := s.List(ctx, "after", 0, "", 1)
	if err != nil || total != 1 || len(books) != 1 || books[0].ID != book.ID || books[0].Title != book.Title {
		t.Fatalf("Backfill: %+v %d %v", books, total, err)
	}
	tx, err := s.DB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	count, err := recognizedSearchTriggers(ctx, tx)
	tx.Rollback()
	if err != nil || count != 3 {
		t.Fatalf("Recognized triggers: %d %v", count, err)
	}
	book, err = s.Edit(ctx, book.ID, Input{Title: "before\x00later", Authors: []string{}, Revision: book.Revision})
	if err != nil {
		t.Fatal(err)
	}
	_, total, err = s.List(ctx, "later", 0, "", 1)
	if err != nil || total != 1 {
		t.Fatalf("Repaired update trigger: %d %v", total, err)
	}
	_, total, err = s.List(ctx, "after", 0, "", 1)
	if err != nil || total != 0 {
		t.Fatalf("Old document remains: %d %v", total, err)
	}
}
