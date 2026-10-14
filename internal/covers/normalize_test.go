package covers

import (
	"bytes"
	"context"
	"encoding/binary"
	"image"
	"image/color"
	"image/jpeg"
	"os"
	"path/filepath"
	"testing"
)

func cameraJPEG(t *testing.T, orientation uint16, width, height int) []byte {
	t.Helper()
	var encoded bytes.Buffer
	jpeg.Encode(&encoded, image.NewRGBA(image.Rect(0, 0, width, height)), nil)
	tiff := make([]byte, 26)
	copy(tiff, "II")
	binary.LittleEndian.PutUint16(tiff[2:], 42)
	binary.LittleEndian.PutUint32(tiff[4:], 8)
	binary.LittleEndian.PutUint16(tiff[8:], 1)
	binary.LittleEndian.PutUint16(tiff[10:], 0x112)
	binary.LittleEndian.PutUint16(tiff[12:], 3)
	binary.LittleEndian.PutUint32(tiff[14:], 1)
	binary.LittleEndian.PutUint16(tiff[18:], orientation)
	body := append([]byte("Exif\x00\x00"), tiff...)
	body = append(body, []byte("private GPS metadata")...)
	marker := []byte{0xff, 0xe1, 0, 0}
	binary.BigEndian.PutUint16(marker[2:], uint16(len(body)+2))
	data := append([]byte{}, encoded.Bytes()[:2]...)
	data = append(data, marker...)
	data = append(data, body...)
	data = append(data, encoded.Bytes()[2:]...)
	return data
}
func TestCameraOrientationAndMetadataRemoval(t *testing.T) {
	input := cameraJPEG(t, 6, 4, 8)
	if exifOrientation(input) != 6 {
		t.Fatal("orientation not parsed")
	}
	output, e := normalizeUpload(input)
	if e != nil {
		t.Fatal(e)
	}
	cfg, format, e := image.DecodeConfig(bytes.NewReader(output))
	if e != nil || format != "jpeg" || cfg.Width != 8 || cfg.Height != 4 {
		t.Fatal(cfg, format, e)
	}
	if bytes.Contains(output, []byte("Exif")) || bytes.Contains(output, []byte("GPS")) {
		t.Fatal("camera metadata retained")
	}
	large, e := normalizeUpload(cameraJPEG(t, 1, 2000, 1000))
	if e != nil {
		t.Fatal(e)
	}
	cfg, _, e = image.DecodeConfig(bytes.NewReader(large))
	if e != nil || cfg.Width != 1600 || cfg.Height != 800 {
		t.Fatal(cfg, e)
	}
}
func TestRejectedUploadPreservesSelectedCover(t *testing.T) {
	s, id := setup(t)
	ctx := context.Background()
	asset, e := s.Upload(ctx, id, testImage(color.Black))
	if e != nil {
		t.Fatal(e)
	}
	if _, e = s.Upload(ctx, id, []byte("corrupt image")); e == nil {
		t.Fatal("corrupt upload accepted")
	}
	selected, _ := s.Store.SelectedCoverURL(ctx, id)
	if selected != asset.URL {
		t.Fatal("rejected upload replaced selection")
	}
	data, e := os.ReadFile(filepath.Join(s.Directory, asset.LocalPath))
	if e != nil {
		t.Fatal(e)
	}
	_, format, e := image.DecodeConfig(bytes.NewReader(data))
	if e != nil || format != "jpeg" {
		t.Fatal("upload not normalized", format, e)
	}
}
func TestAllOrientationTransforms(t *testing.T) {
	src := image.NewRGBA(image.Rect(0, 0, 3, 2))
	src.Set(0, 0, color.RGBA{R: 255, A: 255})
	positions := map[int]image.Point{1: {0, 0}, 2: {2, 0}, 3: {2, 1}, 4: {0, 1}, 5: {0, 0}, 6: {1, 0}, 7: {1, 2}, 8: {0, 2}}
	for orientation, point := range positions {
		out := orient(src, orientation)
		red, _, _, _ := out.At(point.X, point.Y).RGBA()
		if red != 65535 {
			t.Fatal("wrong transformed corner", orientation)
		}
	}
}
