package covers

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"golang.org/x/image/draw"
	"home-library/internal/store"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
)

type Thumbnail struct {
	Data   []byte
	ETag   string
	Width  int
	Height int
}
type variantInfo struct {
	Version          int    `json:"version"`
	OriginalChecksum string `json:"original_checksum"`
	SHA256           string `json:"sha256"`
	Width            int    `json:"width"`
	Height           int    `json:"height"`
}

func thumbnailSize(width, height int) (int, int) {
	if width > 320 || height > 320 {
		if width >= height {
			height = height * 320 / width
			width = 320
		} else {
			width = width * 320 / height
			height = 320
		}
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	return width, height
}
func visualDimensions(data []byte) (int, int, error) {
	cfg, format, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil || format != "jpeg" && format != "png" && format != "webp" {
		return 0, 0, errors.New("Stored cover is not a supported image")
	}
	if cfg.Width < 2 || cfg.Height < 2 || cfg.Width > 6000 || cfg.Height > 6000 || int64(cfg.Width)*int64(cfg.Height) > 16000000 {
		return 0, 0, errors.New("Stored cover dimensions are unsupported")
	}
	width, height := cfg.Width, cfg.Height
	if exifOrientation(data) >= 5 {
		width, height = height, width
	}
	return width, height, nil
}

// Assets enriches response metadata without storing any new database columns.
func (s *Service) Assets(ctx context.Context, id int64) ([]store.CoverAsset, error) {
	assets, e := s.Store.Covers(ctx, id)
	if e != nil {
		return nil, e
	}
	for i := range assets {
		data, e := s.readOriginal(ctx, assets[i])
		if e != nil {
			continue
		}
		width, height, e := visualDimensions(data)
		if e != nil {
			continue
		}
		assets[i].Width, assets[i].Height = width, height
		assets[i].ThumbnailWidth, assets[i].ThumbnailHeight = thumbnailSize(width, height)
	}
	return assets, nil
}
func writeVariant(directory, name string, data []byte) error {
	f, e := os.CreateTemp(directory, ".variant-*")
	if e != nil {
		return e
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
		return e
	}
	return os.Rename(tmp, filepath.Join(directory, name))
}

// Thumbnail is derived solely from the immutable original. Sidecar checksums
// detect damaged cache files; missing variants regenerate after a restore.
func (s *Service) Thumbnail(ctx context.Context, bookID, assetID int64) (Thumbnail, error) {
	s.variantMu.Lock()
	defer s.variantMu.Unlock()
	a, e := s.coverAsset(ctx, bookID, assetID)
	if e != nil {
		return Thumbnail{}, e
	}
	original, e := s.readOriginal(ctx, a)
	if e != nil {
		return Thumbnail{}, e
	}
	width, height, e := visualDimensions(original)
	if e != nil {
		return Thumbnail{}, e
	}
	width, height = thumbnailSize(width, height)
	directory := filepath.Join(s.Directory, "derived")
	name := a.Checksum + "-320-v1"
	imagePath := filepath.Join(directory, name+".jpg")
	infoPath := filepath.Join(directory, name+".json")
	if info, e := os.Lstat(directory); e == nil && (!info.IsDir() || info.Mode()&os.ModeSymlink != 0) {
		return Thumbnail{}, errors.New("Thumbnail cache is unavailable")
	}
	// Every original is checked even when its cached derivative is present.
	if imageInfo, e := os.Lstat(imagePath); e == nil && imageInfo.Mode().IsRegular() && imageInfo.Size() <= MaxImageBytes {
		cached, e := os.ReadFile(imagePath)
		if e == nil && len(cached) <= MaxImageBytes {
			infoStat, statErr := os.Lstat(infoPath)
			if statErr != nil || !infoStat.Mode().IsRegular() || infoStat.Size() > 4096 {
				goto rebuild
			}
			infoRaw, e := os.ReadFile(infoPath)
			if e == nil {
				var info variantInfo
				hash := sha256.Sum256(cached)
				if json.Unmarshal(infoRaw, &info) == nil && info.Version == 1 && info.OriginalChecksum == a.Checksum && info.Width == width && info.Height == height && info.SHA256 == hex.EncodeToString(hash[:]) {
					return Thumbnail{Data: cached, ETag: info.SHA256, Width: width, Height: height}, nil
				}
			}
		}
	}
rebuild:
	if _, e := validate(original); e != nil {
		return Thumbnail{}, e
	}
	src, _, e := image.Decode(bytes.NewReader(original))
	if e != nil {
		return Thumbnail{}, errors.New("Stored cover is incomplete")
	}
	src = fit(orient(src, exifOrientation(original)), 320)
	opaque := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.Draw(opaque, opaque.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(opaque, opaque.Bounds(), src, src.Bounds().Min, draw.Over)
	var out bytes.Buffer
	if e = jpeg.Encode(&out, opaque, &jpeg.Options{Quality: 85}); e != nil {
		return Thumbnail{}, e
	}
	hash := sha256.Sum256(out.Bytes())
	checksum := hex.EncodeToString(hash[:])
	info := variantInfo{Version: 1, OriginalChecksum: a.Checksum, SHA256: checksum, Width: width, Height: height}
	raw, e := json.Marshal(info)
	if e != nil {
		return Thumbnail{}, e
	}
	if e = os.MkdirAll(directory, 0700); e != nil {
		return Thumbnail{}, errors.New("Thumbnail cache is unavailable")
	}
	if e = writeVariant(directory, name+".jpg", out.Bytes()); e != nil {
		return Thumbnail{}, errors.New("Could not cache the thumbnail")
	}
	if e = writeVariant(directory, name+".json", raw); e != nil {
		return Thumbnail{}, errors.New("Could not cache thumbnail details")
	}
	return Thumbnail{Data: out.Bytes(), ETag: checksum, Width: width, Height: height}, nil
}
