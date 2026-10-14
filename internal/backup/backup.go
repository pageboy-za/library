// Package backup creates verified, self-contained snapshots of library data.
package backup

import (
	"context"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	_ "modernc.org/sqlite"
	"net/url"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"time"
)

type File struct {
	Path   string `json:"path"`
	SHA256 string `json:"sha256"`
	Bytes  int64  `json:"bytes"`
}
type Manifest struct {
	Format        int    `json:"format"`
	CreatedAt     string `json:"created_at"`
	SchemaVersion int    `json:"schema_version"`
	Files         []File `json:"files"`
}
type Export struct {
	Format        int                         `json:"format"`
	SchemaVersion int                         `json:"schema_version"`
	Tables        map[string][]map[string]any `json:"tables"`
}

func openRead(path string, immutable bool) (*sql.DB, error) {
	abs, e := filepath.Abs(path)
	if e != nil {
		return nil, e
	}
	if _, e = os.Stat(abs); e != nil {
		return nil, e
	}
	u := url.URL{Scheme: "file", Path: abs}
	q := u.Query()
	q.Set("mode", "ro")
	if immutable {
		q.Set("immutable", "1")
	}
	u.RawQuery = q.Encode()
	db, e := sql.Open("sqlite", u.String())
	if e != nil {
		return nil, e
	}
	db.SetMaxOpenConns(1)
	if _, e = db.Exec("PRAGMA busy_timeout=5000"); e != nil {
		db.Close()
		return nil, e
	}
	return db, nil
}
func safePath(path string) bool {
	return path != "" && !filepath.IsAbs(path) && filepath.ToSlash(filepath.Clean(path)) == path && path != "." && path != ".." && !strings.HasPrefix(path, "../") && !strings.Contains(path, "\\")
}
func noSymlinks(root, relative string) error {
	current := root
	for _, part := range strings.Split(relative, "/") {
		current = filepath.Join(current, part)
		info, err := os.Lstat(current)
		if err != nil {
			return err
		}
		if info.Mode()&os.ModeSymlink != 0 {
			return errors.New("Backup paths may not contain symlinks")
		}
	}
	return nil
}
func digest(path string) (File, error) {
	f, e := os.Open(path)
	if e != nil {
		return File{}, e
	}
	defer f.Close()
	info, e := f.Stat()
	if e != nil {
		return File{}, e
	}
	if !info.Mode().IsRegular() {
		return File{}, errors.New("Backup assets must be regular files")
	}
	h := sha256.New()
	n, e := io.Copy(h, f)
	return File{SHA256: hex.EncodeToString(h.Sum(nil)), Bytes: n}, e
}
func copyFile(source, dest string) error {
	info, e := os.Lstat(source)
	if e != nil {
		return e
	}
	if !info.Mode().IsRegular() {
		return errors.New("Backup source contains a nonregular file")
	}
	in, e := os.Open(source)
	if e != nil {
		return e
	}
	defer in.Close()
	if e = os.MkdirAll(filepath.Dir(dest), 0700); e != nil {
		return e
	}
	out, e := os.OpenFile(dest, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0600)
	if e != nil {
		return e
	}
	_, e = io.Copy(out, in)
	if e == nil {
		e = out.Sync()
	}
	closeErr := out.Close()
	if e == nil {
		e = closeErr
	}
	return e
}
func writeJSON(path string, value any) error {
	data, e := json.MarshalIndent(value, "", "  ")
	if e != nil {
		return e
	}
	return os.WriteFile(path, append(data, '\n'), 0600)
}
func coverFiles(ctx context.Context, db *sql.DB) (map[string]string, error) {
	rows, e := db.QueryContext(ctx, "SELECT local_path,checksum FROM cover_assets")
	if e != nil {
		return nil, e
	}
	defer rows.Close()
	files := map[string]string{}
	for rows.Next() {
		var path, checksum string
		if e = rows.Scan(&path, &checksum); e != nil {
			return nil, e
		}
		if !safePath(path) {
			return nil, errors.New("Unsafe cover path in database")
		}
		if prev, ok := files[path]; ok && prev != checksum {
			return nil, errors.New("Conflicting cover checksum")
		}
		files[path] = checksum
	}
	return files, rows.Err()
}
func exported(ctx context.Context, db *sql.DB) (Export, error) {
	result := Export{Format: 1, Tables: map[string][]map[string]any{}}
	if e := db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&result.SchemaVersion); e != nil {
		return result, e
	}
	rows, e := db.QueryContext(ctx, "SELECT name FROM sqlite_schema WHERE type='table' AND name NOT LIKE 'sqlite_%' ORDER BY name")
	if e != nil {
		return result, e
	}
	names := []string{}
	for rows.Next() {
		var name string
		if e = rows.Scan(&name); e != nil {
			rows.Close()
			return result, e
		}
		names = append(names, name)
	}
	e = rows.Err()
	rows.Close()
	if e != nil {
		return result, e
	}
	for _, name := range names {
		// Search documents and SQLite FTS shadow tables are rebuildable indexes.
		if name == "mutation_requests" || name == "book_search" || strings.HasPrefix(name, "book_search_") {
			continue
		}
		quoted := `"` + strings.ReplaceAll(name, `"`, `""`) + `"`
		r, e := db.QueryContext(ctx, "SELECT * FROM "+quoted)
		if e != nil {
			return result, e
		}
		cols, e := r.Columns()
		if e != nil {
			r.Close()
			return result, e
		}
		records := []map[string]any{}
		for r.Next() {
			values := make([]any, len(cols))
			ptrs := make([]any, len(cols))
			for i := range values {
				ptrs[i] = &values[i]
			}
			if e = r.Scan(ptrs...); e != nil {
				r.Close()
				return result, e
			}
			record := map[string]any{}
			for i, col := range cols {
				v := values[i]
				if b, ok := v.([]byte); ok {
					v = string(b)
				}
				if text, ok := v.(string); ok && (col == "authors" || col == "subjects" || col == "tags") {
					var parsed any
					if json.Unmarshal([]byte(text), &parsed) == nil {
						v = parsed
					}
				}
				record[col] = v
			}
			records = append(records, record)
		}
		e = r.Err()
		r.Close()
		if e != nil {
			return result, e
		}
		result.Tables[name] = records
	}
	return result, nil
}

// Create reserves a new directory and writes the manifest last. SQLite's
// VACUUM INTO takes a transactionally consistent snapshot including live WAL.
func Create(ctx context.Context, sourceData, dest string) (err error) {
	if e := os.Mkdir(dest, 0700); e != nil {
		return fmt.Errorf("Backup destination must be new: %w", e)
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dest)
		}
	}()
	db, e := openRead(filepath.Join(sourceData, "library.sqlite"), false)
	if e != nil {
		return e
	}
	defer db.Close()
	if _, e = db.ExecContext(ctx, "PRAGMA synchronous=FULL"); e != nil {
		return e
	}
	snapshot := filepath.Join(dest, "library.sqlite")
	if _, e = db.ExecContext(ctx, "VACUUM INTO ?", snapshot); e != nil {
		return e
	}
	snap, e := openRead(snapshot, true)
	if e != nil {
		return e
	}
	defer snap.Close()
	data, e := exported(ctx, snap)
	if e != nil {
		return e
	}
	if e = writeJSON(filepath.Join(dest, "catalogue.json"), data); e != nil {
		return e
	}
	covers, e := coverFiles(ctx, snap)
	if e != nil {
		return e
	}
	paths := []string{"library.sqlite", "catalogue.json"}
	for path, checksum := range covers {
		rel := "covers/" + path
		if e = copyFile(filepath.Join(sourceData, "covers", filepath.FromSlash(path)), filepath.Join(dest, filepath.FromSlash(rel))); e != nil {
			return e
		}
		file, e := digest(filepath.Join(dest, filepath.FromSlash(rel)))
		if e != nil {
			return e
		}
		if file.SHA256 != checksum {
			return fmt.Errorf("Cover checksum mismatch: %s", path)
		}
		paths = append(paths, rel)
	}
	sort.Strings(paths)
	manifest := Manifest{Format: 1, CreatedAt: time.Now().UTC().Format(time.RFC3339), SchemaVersion: data.SchemaVersion, Files: []File{}}
	for _, path := range paths {
		f, e := digest(filepath.Join(dest, filepath.FromSlash(path)))
		if e != nil {
			return e
		}
		f.Path = path
		manifest.Files = append(manifest.Files, f)
	}
	if e = writeJSON(filepath.Join(dest, "manifest.json"), manifest); e != nil {
		return e
	}
	return Verify(ctx, dest)
}

func Verify(ctx context.Context, directory string) error {
	raw, e := os.ReadFile(filepath.Join(directory, "manifest.json"))
	if e != nil {
		return e
	}
	var manifest Manifest
	if e = json.Unmarshal(raw, &manifest); e != nil {
		return e
	}
	if manifest.Format != 1 {
		return errors.New("Unsupported backup format")
	}
	present := map[string]bool{}
	for _, f := range manifest.Files {
		if !safePath(f.Path) || present[f.Path] {
			return errors.New("Unsafe or duplicate backup path")
		}
		present[f.Path] = true
		if e = noSymlinks(directory, f.Path); e != nil {
			return e
		}
		path := filepath.Join(directory, filepath.FromSlash(f.Path))
		info, e := os.Lstat(path)
		if e != nil {
			return e
		}
		if !info.Mode().IsRegular() {
			return errors.New("Backup contains nonregular files")
		}
		actual, e := digest(path)
		if e != nil {
			return e
		}
		if actual.SHA256 != f.SHA256 || actual.Bytes != f.Bytes {
			return fmt.Errorf("Backup file checksum mismatch: %s", f.Path)
		}
	}
	if !present["library.sqlite"] || !present["catalogue.json"] {
		return errors.New("Backup is missing its database or export")
	}
	db, e := openRead(filepath.Join(directory, "library.sqlite"), true)
	if e != nil {
		return e
	}
	defer db.Close()
	var check string
	if e = db.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&check); e != nil {
		return e
	}
	if check != "ok" {
		return fmt.Errorf("Database integrity check failed: %s", check)
	}
	if e = verifySnapshotForeignKeys(ctx, db); e != nil {
		return e
	}
	var version int
	if e = db.QueryRowContext(ctx, "PRAGMA user_version").Scan(&version); e != nil {
		return e
	}
	if version != manifest.SchemaVersion {
		return errors.New("Backup schema version mismatch")
	}
	covers, e := coverFiles(ctx, db)
	if e != nil {
		return e
	}
	for path, checksum := range covers {
		rel := "covers/" + path
		if !present[rel] {
			return fmt.Errorf("Missing cover: %s", path)
		}
		f, e := digest(filepath.Join(directory, filepath.FromSlash(rel)))
		if e != nil {
			return e
		}
		if f.SHA256 != checksum {
			return fmt.Errorf("Database cover checksum mismatch: %s", path)
		}
	}
	return nil
}

// Restore only creates a new data directory; active data can never be overwritten.
func Restore(ctx context.Context, source, dest string) (err error) {
	if err = Verify(ctx, source); err != nil {
		return err
	}
	if err = os.Mkdir(dest, 0700); err != nil {
		return fmt.Errorf("Restore destination must be new: %w", err)
	}
	defer func() {
		if err != nil {
			os.RemoveAll(dest)
		}
	}()
	raw, err := os.ReadFile(filepath.Join(source, "manifest.json"))
	if err != nil {
		return err
	}
	var manifest Manifest
	if err = json.Unmarshal(raw, &manifest); err != nil {
		return err
	}
	for _, f := range manifest.Files {
		if f.Path == "catalogue.json" {
			continue
		}
		if err = copyFile(filepath.Join(source, filepath.FromSlash(f.Path)), filepath.Join(dest, filepath.FromSlash(f.Path))); err != nil {
			return err
		}
		actual, e := digest(filepath.Join(dest, filepath.FromSlash(f.Path)))
		if e != nil {
			return e
		}
		if actual.SHA256 != f.SHA256 || actual.Bytes != f.Bytes {
			return fmt.Errorf("Backup changed during restore: %s", f.Path)
		}
	}
	return nil
}

// SQLite quick_check validates storage/index integrity but not referential
// relationships. Verify those too so a successful snapshot can start after restore.
func verifySnapshotForeignKeys(ctx context.Context, db *sql.DB) error {
	rows, e := db.QueryContext(ctx, "PRAGMA foreign_key_check")
	if e != nil {
		return e
	}
	defer rows.Close()
	if rows.Next() {
		var table, parent string
		var row sql.NullInt64
		var constraint int
		if e = rows.Scan(&table, &row, &parent, &constraint); e != nil {
			return e
		}
		return fmt.Errorf("Backup database has a foreign key violation in %s referencing %s", table, parent)
	}
	return rows.Err()
}
