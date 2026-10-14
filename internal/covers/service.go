// Package covers caches covers only for books in the user's local catalogue.
package covers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	_ "golang.org/x/image/webp"
	"home-library/internal/store"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"io"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const MaxImageBytes = 8 << 20

var editionID = regexp.MustCompile(`^OL[0-9]+M$`)
var isbnID = regexp.MustCompile(`^[0-9]{13}$`)

// Covers API redirects images into Internet Archive's cover-only ZIP storage.
var archiveCoverPath = regexp.MustCompile(`^/download/(?:[sml]_covers_[0-9]+|olcovers[0-9]+)/[A-Za-z0-9_-]+\.zip/[0-9]+-[SML]\.(?:jpg|png|webp)$`)
var archiveCoverHost = regexp.MustCompile(`^ia[0-9]+\.(?:us|eu)\.archive\.org$`)
var archiveCoverZIP = regexp.MustCompile(`^/[0-9]+/items/(?:[sml]_covers_[0-9]+|olcovers[0-9]+)/[A-Za-z0-9_-]+\.zip$`)
var archiveCoverFile = regexp.MustCompile(`^[0-9]+-[SML]\.(?:jpg|png|webp)$`)

func trustedArchive(u *url.URL) bool {
	if u.Hostname() == "archive.org" {
		return archiveCoverPath.MatchString(u.Path)
	}
	if !archiveCoverHost.MatchString(u.Hostname()) || u.Path != "/view_archive.php" {
		return false
	}
	q := u.Query()
	return len(q) == 2 && len(q["archive"]) == 1 && len(q["file"]) == 1 && archiveCoverZIP.MatchString(q.Get("archive")) && archiveCoverFile.MatchString(q.Get("file"))
}

type Service struct {
	Store     *store.Store
	Directory string
	HTTP      *http.Client
	mu        sync.Mutex
	variantMu sync.Mutex
	last      time.Time
}

func New(s *store.Store, directory string) *Service {
	return &Service{Store: s, Directory: directory, HTTP: &http.Client{Timeout: 15 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) > 3 {
			return errors.New("Too many cover redirects")
		}
		return allowed(r.URL)
	}}}
}
func allowed(u *url.URL) error {
	if u.Scheme != "https" || u.User != nil || u.Port() != "" || (u.Hostname() != "openlibrary.org" && u.Hostname() != "covers.openlibrary.org" && !trustedArchive(u)) {
		return errors.New("Cover source is not allowed")
	}
	return nil
}
func (s *Service) get(ctx context.Context, address string, max int64) ([]byte, error) {
	u, e := url.Parse(address)
	if e != nil {
		return nil, e
	}
	if e = allowed(u); e != nil {
		return nil, e
	}
	if delay := time.Second - time.Since(s.last); delay > 0 {
		select {
		case <-time.After(delay):
		case <-ctx.Done():
			return nil, ctx.Err()
		}
	}
	s.last = time.Now()
	r, e := http.NewRequestWithContext(ctx, "GET", address, nil)
	if e != nil {
		return nil, e
	}
	r.Header.Set("User-Agent", "HomeLibrary/0.1 (personal local catalogue)")
	resp, e := s.HTTP.Do(r)
	if e != nil {
		return nil, e
	}
	defer resp.Body.Close()
	if resp.StatusCode == 404 {
		return nil, store.ErrNotFound
	}
	if resp.StatusCode != 200 {
		return nil, fmt.Errorf("Open Library returned %d; try again later", resp.StatusCode)
	}
	b, e := io.ReadAll(io.LimitReader(resp.Body, max+1))
	if e == nil && int64(len(b)) > max {
		e = errors.New("Cover source response too large")
	}
	return b, e
}
func validate(data []byte) (string, error) {
	if len(data) == 0 || len(data) > MaxImageBytes {
		return "", errors.New("Choose an image up to 8 MB")
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil {
		return "", errors.New("Choose a valid JPEG, PNG or WebP image")
	}
	if format != "jpeg" && format != "png" && format != "webp" {
		return "", errors.New("Choose a JPEG, PNG or WebP image")
	}
	if cfg.Width < 2 || cfg.Height < 2 || cfg.Width > 6000 || cfg.Height > 6000 || int64(cfg.Width)*int64(cfg.Height) > 16000000 {
		return "", errors.New("Cover dimensions must be between 2 and 6000 pixels, with at most 16 million pixels")
	}
	if _, _, e = image.Decode(bytes.NewReader(data)); e != nil {
		return "", errors.New("Image is incomplete or corrupt")
	}
	if format == "jpeg" {
		format = "jpg"
	}
	return format, nil
}
func (s *Service) save(ctx context.Context, a store.CoverAsset, data []byte, selectCover, onlyMissing bool) (store.CoverAsset, error) {
	ext, e := validate(data)
	if e != nil {
		return a, e
	}
	sum := sha256.Sum256(data)
	a.Checksum = hex.EncodeToString(sum[:])
	a.LocalPath = a.Checksum + "." + ext
	if e = os.MkdirAll(s.Directory, 0700); e != nil {
		return a, e
	}
	dest := filepath.Join(s.Directory, a.LocalPath)
	// Content hashes make assets immutable and safe for long-lived static pages.
	if _, e = os.Stat(dest); os.IsNotExist(e) {
		f, e := os.CreateTemp(s.Directory, ".cover-*")
		if e != nil {
			return a, e
		}
		tmp := f.Name()
		defer os.Remove(tmp)
		if _, e = f.Write(data); e == nil {
			e = f.Sync()
		}
		ce := f.Close()
		if e == nil {
			e = ce
		}
		if e != nil {
			return a, e
		}
		if e = os.Rename(tmp, dest); e != nil {
			return a, e
		}
	}
	return s.Store.SaveCover(ctx, a, selectCover, onlyMissing)
}
func (s *Service) Upload(ctx context.Context, id int64, data []byte) (store.CoverAsset, error) {
	if _, e := s.Store.Book(ctx, id); e != nil {
		return store.CoverAsset{}, e
	}
	data, e := normalizeUpload(data)
	if e != nil {
		return store.CoverAsset{}, e
	}
	return s.save(ctx, store.CoverAsset{BookID: id, SourceKind: "upload"}, data, true, false)
}

// Fetch limits alternatives to cover IDs attached to the exact edition. It
// deliberately does not substitute another edition merely sharing a title.
func (s *Service) Fetch(ctx context.Context, id int64) ([]store.CoverAsset, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	book, e := s.Store.Book(ctx, id)
	if e != nil {
		return nil, e
	}
	existing, e := s.Store.Covers(ctx, id)
	if e != nil {
		return nil, e
	}
	source := book.SourceID
	address := ""
	if editionID.MatchString(source) {
		address = "https://openlibrary.org/books/" + source + ".json"
	} else if isbnID.MatchString(book.ISBN) {
		address = "https://openlibrary.org/isbn/" + book.ISBN + ".json"
	} else {
		return existing, errors.New("This book needs an ISBN or Open Library edition to find covers")
	}
	data, e := s.get(ctx, address, 1<<20)
	if e != nil && !errors.Is(e, store.ErrNotFound) {
		return existing, e
	}
	var edition struct {
		Key    string  `json:"key"`
		Covers []int64 `json:"covers"`
	}
	if data != nil {
		if e = json.Unmarshal(data, &edition); e != nil {
			return existing, e
		}
	}
	if strings.HasPrefix(edition.Key, "/books/") {
		source = strings.TrimPrefix(edition.Key, "/books/")
	}
	if data != nil && !editionID.MatchString(source) {
		return existing, errors.New("Open Library returned an invalid edition")
	}
	known := map[int64]bool{}
	for _, a := range existing {
		if a.SourceCoverID != nil {
			known[*a.SourceCoverID] = true
		}
	}
	downloaded := 0
	var lastError error
	for _, coverID := range edition.Covers {
		if coverID <= 0 || known[coverID] {
			continue
		}
		if downloaded >= 6 {
			break
		}
		downloaded++
		data, e = s.get(ctx, fmt.Sprintf("https://covers.openlibrary.org/b/id/%d-L.jpg?default=false", coverID), MaxImageBytes)
		if e != nil {
			lastError = e
			continue
		}
		_, e = s.save(ctx, store.CoverAsset{BookID: id, SourceKind: "openlibrary", SourceCoverID: &coverID, SourceEditionID: source}, data, true, true)
		if e != nil {
			lastError = e
		}
	}
	// Some editions have no covers array but do have an ISBN cover mapping.
	if (len(edition.Covers) == 0 || downloaded > 0) && len(existing) == 0 && isbnID.MatchString(book.ISBN) {
		cached, ce := s.Store.Covers(ctx, id)
		if ce != nil {
			return nil, ce
		}
		if len(cached) == 0 {
			data, e = s.get(ctx, "https://covers.openlibrary.org/b/isbn/"+book.ISBN+"-L.jpg?default=false", MaxImageBytes)
			if e == nil {
				_, e = s.save(ctx, store.CoverAsset{BookID: id, SourceKind: "openlibrary-isbn", SourceEditionID: source}, data, true, true)
			}
			lastError = e
		}
	}
	result, e := s.Store.Covers(ctx, id)
	if e != nil {
		return nil, e
	}
	if len(result) == 0 {
		if lastError != nil {
			return result, lastError
		}
		return result, errors.New("No cover found for this edition. Upload your own image")
	}
	return result, nil
}
