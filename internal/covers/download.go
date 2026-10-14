package covers

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"home-library/internal/store"
	"io"
	"os"
	"path/filepath"
	"regexp"
)

var storedCoverName = regexp.MustCompile(`^[0-9a-f]{64}\.(jpg|png|webp)$`)

// Download renders a fresh sanitized image in memory, including provider
// images. It never changes the stored asset, selection or physical copies.
func (s *Service) coverAsset(ctx context.Context, bookID, assetID int64) (store.CoverAsset, error) {
	if _, e := s.Store.Book(ctx, bookID); e != nil {
		return store.CoverAsset{}, e
	}
	assets, e := s.Store.Covers(ctx, bookID)
	if e != nil {
		return store.CoverAsset{}, e
	}
	for _, a := range assets {
		if a.ID == assetID {
			return a, nil
		}
	}
	return store.CoverAsset{}, store.ErrNotFound
}
func (s *Service) readOriginal(ctx context.Context, a store.CoverAsset) ([]byte, error) {
	if !storedCoverName.MatchString(a.LocalPath) {
		return nil, errors.New("Stored cover is unavailable")
	}
	path := filepath.Join(s.Directory, a.LocalPath)
	info, e := os.Lstat(path)
	if e != nil || !info.Mode().IsRegular() {
		return nil, errors.New("Stored cover is unavailable")
	}
	f, e := os.Open(path)
	if e != nil {
		return nil, errors.New("Stored cover is unavailable")
	}
	defer f.Close()
	data, e := io.ReadAll(io.LimitReader(f, MaxImageBytes+1))
	if e != nil {
		return nil, errors.New("Could not read the cover image")
	}
	hash := sha256.Sum256(data)
	if hex.EncodeToString(hash[:]) != a.Checksum {
		return nil, errors.New("Stored cover failed its integrity check")
	}
	if e = ctx.Err(); e != nil {
		return nil, e
	}
	return data, nil
}
func (s *Service) Download(ctx context.Context, bookID, assetID int64) ([]byte, error) {
	a, e := s.coverAsset(ctx, bookID, assetID)
	if e != nil {
		return nil, e
	}
	data, e := s.readOriginal(ctx, a)
	if e != nil {
		return nil, e
	}
	return normalizeUpload(data)
}
