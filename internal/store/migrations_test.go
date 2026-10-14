package store

import (
	"bytes"
	"context"
	"database/sql"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func legacyFixture(t *testing.T, path string) *sql.DB {
	t.Helper()
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	db.SetMaxOpenConns(1)
	if _, e = db.Exec(schema); e != nil {
		t.Fatal(e)
	}
	if _, e = db.Exec("PRAGMA user_version=1"); e != nil {
		t.Fatal(e)
	}
	return db
}
func mustExecMigration(t *testing.T, db *sql.DB, q string) {
	t.Helper()
	if _, e := db.Exec(q); e != nil {
		t.Fatal(e)
	}
}
func TestLegacyCoreUpgradePreservesDataAndRestarts(t *testing.T) {
	path := filepath.Join(t.TempDir(), "legacy.sqlite")
	db := legacyFixture(t, path)
	for _, q := range []string{`INSERT INTO books(id,isbn,title,authors) VALUES(7,'9780140328721','Existing book','["Existing author"]')`, `INSERT INTO bookcases(id,name) VALUES(5,'Study')`, `INSERT INTO shelves(id,bookcase_id,name) VALUES(8,5,'One')`, `INSERT INTO copies(id,book_id,shelf_id,label) VALUES(11,7,8,'Original copy')`, `INSERT INTO borrowers(id,name) VALUES(2,'Ada')`, `INSERT INTO loans(id,copy_id,borrower_id) VALUES(13,11,2)`, `INSERT INTO cover_assets(id,book_id,local_path,source_kind,checksum) VALUES(99,7,'covers/existing.jpg','upload','original-checksum')`, `UPDATE books SET selected_cover_asset_id=99 WHERE id=7`, `CREATE TABLE external_extension(note TEXT)`, `INSERT INTO external_extension VALUES('keep me')`} {
		mustExecMigration(t, db, q)
	}
	db.Close()
	for i := 0; i < 2; i++ {
		s, e := Open(path)
		if e != nil {
			t.Fatal(e)
		}
		book, e := s.Book(context.Background(), 7)
		if e != nil || book.Title != "Existing book" || book.Count != 1 || book.Available != 0 || book.Copies[0].ID != 11 || book.Copies[0].Shelf != "Study / One" {
			t.Fatalf("Legacy data changed %+v %v", book, e)
		}
		sh, e := s.Shelves(context.Background())
		if e != nil || sh[0].ID != 8 || sh[0].Revision != 1 {
			t.Fatalf("Location upgrade %+v %v", sh, e)
		}
		var cover int
		var note string
		if e = s.DB.QueryRow("SELECT selected_cover_asset_id FROM books WHERE id=7").Scan(&cover); e != nil || cover != 99 {
			t.Fatal("Cover association changed")
		}
		s.DB.QueryRow("SELECT note FROM external_extension").Scan(&note)
		if note != "keep me" {
			t.Fatal("Extra table overwritten")
		}
		var version, enabled int
		s.DB.QueryRow("PRAGMA user_version").Scan(&version)
		s.DB.QueryRow("PRAGMA foreign_keys").Scan(&enabled)
		if version != 1 || enabled != 1 {
			t.Fatal("Version/enforcement wrong")
		}
		if _, e = s.DB.Exec("INSERT INTO copies(book_id) VALUES(999)"); e == nil {
			t.Fatal("Foreign keys not enforced")
		}
		s.DB.Close()
	}
}
func TestNewerVersionRefusalDoesNotModifyDatabase(t *testing.T) {
	path := filepath.Join(t.TempDir(), "future.sqlite")
	db := legacyFixture(t, path)
	mustExecMigration(t, db, "PRAGMA user_version=2")
	db.Close()
	before, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if s, e := Open(path); e == nil {
		s.DB.Close()
		t.Fatal("Future version accepted")
	} else if !strings.Contains(e.Error(), "newer") {
		t.Fatal(e)
	}
	after, e := os.ReadFile(path)
	if e != nil {
		t.Fatal(e)
	}
	if !bytes.Equal(before, after) {
		t.Fatal("Refusal modified future database bytes")
	}
}
func TestAtomicAdditiveUpgradeRollsBackOnIncompatibleFeature(t *testing.T) {
	path := filepath.Join(t.TempDir(), "broken-feature.sqlite")
	db := legacyFixture(t, path)
	mustExecMigration(t, db, "CREATE TABLE location_versions(kind TEXT,id INTEGER,sentinel TEXT)")
	mustExecMigration(t, db, "INSERT INTO location_versions VALUES('shelf',1,'preserved')")
	db.Close()
	if s, e := Open(path); e == nil {
		s.DB.Close()
		t.Fatal("Broken feature accepted")
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var count int
	db.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE type='table' AND name='book_discovery'").Scan(&count)
	if count != 0 {
		t.Fatal("Discovery upgrade committed before failing location upgrade")
	}
	var sentinel string
	db.QueryRow("SELECT sentinel FROM location_versions").Scan(&sentinel)
	if sentinel != "preserved" {
		t.Fatal("Incompatible table overwritten")
	}
	var journal string
	db.QueryRow("PRAGMA journal_mode").Scan(&journal)
	if journal != "delete" {
		t.Fatal("Failed upgrade modified journal mode")
	}
}
func TestForeignKeyViolationsRejectBeforeUpgrade(t *testing.T) {
	path := filepath.Join(t.TempDir(), "invalid-data.sqlite")
	db := legacyFixture(t, path)
	mustExecMigration(t, db, "INSERT INTO copies(id,book_id) VALUES(1,999)")
	db.Close()
	if s, e := Open(path); e == nil {
		s.DB.Close()
		t.Fatal("Foreign key violation accepted")
	} else if !strings.Contains(e.Error(), "foreign key violation") {
		t.Fatal(e)
	}
	db, e := sql.Open("sqlite", path)
	if e != nil {
		t.Fatal(e)
	}
	defer db.Close()
	var count int
	db.QueryRow("SELECT COUNT(*) FROM sqlite_schema WHERE name IN('book_discovery','location_versions')").Scan(&count)
	if count != 0 {
		t.Fatal("Feature upgrade ran before invalid data rejection")
	}
}
func TestUnversionedAndMalformedCoreRefusal(t *testing.T) {
	for _, fixture := range []struct{ name, sql string }{{"unversioned", "CREATE TABLE unrelated(value TEXT);INSERT INTO unrelated VALUES('preserve')"}, {"missing-columns", "CREATE TABLE books(id INTEGER PRIMARY KEY,title TEXT);PRAGMA user_version=1"}, {"missing-active-index", schema + ";DROP INDEX one_active_loan;PRAGMA user_version=1"}} {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "unknown.sqlite")
			db, e := sql.Open("sqlite", path)
			if e != nil {
				t.Fatal(e)
			}
			mustExecMigration(t, db, fixture.sql)
			db.Close()
			before, _ := os.ReadFile(path)
			if s, e := Open(path); e == nil {
				s.DB.Close()
				t.Fatal("Incompatible database accepted")
			}
			after, _ := os.ReadFile(path)
			if !bytes.Equal(before, after) {
				t.Fatal("Refusal modified database")
			}
		})
	}
}
func TestFreshInitializationRestart(t *testing.T) {
	path := filepath.Join(t.TempDir(), "fresh.sqlite")
	for i := 0; i < 2; i++ {
		s, e := Open(path)
		if e != nil {
			t.Fatal(e)
		}
		if e = validateCore(context.Background(), s.DB); e != nil {
			t.Fatal(e)
		}
		if e = validateFeatures(context.Background(), s.DB); e != nil {
			t.Fatal(e)
		}
		if e = checkForeignKeys(context.Background(), s.DB); e != nil {
			t.Fatal(e)
		}
		s.DB.Close()
	}
}

func TestDeclaredConstraintsValidatedBeforeMutation(t *testing.T) {
	fixtures := []struct{ name, ddl string }{
		{"partial-isbn", strings.Replace(schema, "isbn TEXT UNIQUE", "isbn TEXT", 1) + `;CREATE UNIQUE INDEX fake_isbn_unique ON books(isbn) WHERE title='special';PRAGMA user_version=1`},
		{"partial-shelf", strings.Replace(schema, ", UNIQUE(bookcase_id,name)", "", 1) + `;CREATE UNIQUE INDEX fake_shelf_unique ON shelves(bookcase_id,name) WHERE name='special';PRAGMA user_version=1`},
		{"missing-copy-fk", strings.Replace(schema, "book_id INTEGER NOT NULL REFERENCES books(id)", "book_id INTEGER NOT NULL", 1) + `;PRAGMA user_version=1`},
		{"missing-copy-shelf-fk", strings.Replace(schema, "shelf_id INTEGER REFERENCES shelves(id)", "shelf_id INTEGER", 1) + `;PRAGMA user_version=1`},
		{"missing-cover-forward-fk", strings.Replace(schema, "selected_cover_asset_id INTEGER REFERENCES cover_assets(id)", "selected_cover_asset_id INTEGER", 1) + `;PRAGMA user_version=1`},
		{"missing-book-pk", strings.Replace(schema, "CREATE TABLE books(id INTEGER PRIMARY KEY", "CREATE TABLE books(id INTEGER", 1) + `;PRAGMA user_version=1`},
	}
	for _, fixture := range fixtures {
		t.Run(fixture.name, func(t *testing.T) {
			path := filepath.Join(t.TempDir(), "incompatible.sqlite")
			db, e := sql.Open("sqlite", path)
			if e != nil {
				t.Fatal(e)
			}
			mustExecMigration(t, db, fixture.ddl)
			db.Close()
			before, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			if s, e := Open(path); e == nil {
				s.DB.Close()
				t.Fatal("Missing or partial declared constraint accepted")
			}
			after, e := os.ReadFile(path)
			if e != nil {
				t.Fatal(e)
			}
			if !bytes.Equal(before, after) {
				t.Fatal("Constraint refusal changed database bytes")
			}
		})
	}
}
