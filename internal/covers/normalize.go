package covers

import (
	"bytes"
	"encoding/binary"
	"errors"
	"golang.org/x/image/draw"
	"image"
	"image/color"
	"image/jpeg"
)

// exifOrientation only reads the bounded TIFF orientation entry; no camera
// information, location, or other metadata is retained in normalized output.
func exifOrientation(data []byte) int {
	if len(data) < 4 || data[0] != 0xff || data[1] != 0xd8 {
		return 1
	}
	for offset := 2; offset+4 <= len(data); {
		if data[offset] != 0xff {
			return 1
		}
		marker := data[offset+1]
		offset += 2
		if marker == 0xda || marker == 0xd9 {
			return 1
		}
		if marker == 0xff {
			offset--
			continue
		}
		if marker == 0x01 || (marker >= 0xd0 && marker <= 0xd7) {
			continue
		}
		length := int(binary.BigEndian.Uint16(data[offset : offset+2]))
		if length < 2 || length > len(data)-offset {
			return 1
		}
		block := data[offset+2 : offset+length]
		offset += length
		if marker != 0xe1 || len(block) < 14 || !bytes.Equal(block[:6], []byte("Exif\x00\x00")) {
			continue
		}
		tiff := block[6:]
		var order binary.ByteOrder
		switch string(tiff[:2]) {
		case "II":
			order = binary.LittleEndian
		case "MM":
			order = binary.BigEndian
		default:
			continue
		}
		if order.Uint16(tiff[2:4]) != 42 {
			continue
		}
		start := uint64(order.Uint32(tiff[4:8]))
		if start+2 > uint64(len(tiff)) {
			continue
		}
		count := uint64(order.Uint16(tiff[start : start+2]))
		if count > 4096 || start+2+count*12 > uint64(len(tiff)) {
			continue
		}
		for i := uint64(0); i < count; i++ {
			entry := tiff[start+2+i*12 : start+2+(i+1)*12]
			if order.Uint16(entry[:2]) == 0x112 && order.Uint16(entry[2:4]) == 3 && order.Uint32(entry[4:8]) == 1 {
				v := int(order.Uint16(entry[8:10]))
				if v >= 1 && v <= 8 {
					return v
				}
			}
		}
	}
	return 1
}
func orient(src image.Image, orientation int) image.Image {
	if orientation == 1 {
		return src
	}
	bounds := src.Bounds()
	width, height := bounds.Dx(), bounds.Dy()
	outWidth, outHeight := width, height
	if orientation >= 5 {
		outWidth, outHeight = height, width
	}
	dst := image.NewRGBA(image.Rect(0, 0, outWidth, outHeight))
	for y := 0; y < height; y++ {
		for x := 0; x < width; x++ {
			dx, dy := x, y
			switch orientation {
			case 2:
				dx = width - 1 - x
			case 3:
				dx, dy = width-1-x, height-1-y
			case 4:
				dy = height - 1 - y
			case 5:
				dx, dy = y, x
			case 6:
				dx, dy = height-1-y, x
			case 7:
				dx, dy = height-1-y, width-1-x
			case 8:
				dx, dy = y, width-1-x
			}
			dst.Set(dx, dy, src.At(bounds.Min.X+x, bounds.Min.Y+y))
		}
	}
	return dst
}
func fit(src image.Image, max int) image.Image {
	b := src.Bounds()
	width, height := b.Dx(), b.Dy()
	if width <= max && height <= max {
		return src
	}
	if width >= height {
		height = height * max / width
		width = max
	} else {
		width = width * max / height
		height = max
	}
	if width < 1 {
		width = 1
	}
	if height < 1 {
		height = 1
	}
	dst := image.NewRGBA(image.Rect(0, 0, width, height))
	draw.CatmullRom.Scale(dst, dst.Bounds(), src, b, draw.Src, nil)
	return dst
}
func normalizeUpload(data []byte) ([]byte, error) {
	if _, e := validate(data); e != nil {
		return nil, e
	}
	src, _, e := image.Decode(bytes.NewReader(data))
	if e != nil {
		return nil, e
	}
	src = fit(orient(src, exifOrientation(data)), 1600)
	opaque := image.NewRGBA(image.Rect(0, 0, src.Bounds().Dx(), src.Bounds().Dy()))
	draw.Draw(opaque, opaque.Bounds(), &image.Uniform{C: color.White}, image.Point{}, draw.Src)
	draw.Draw(opaque, opaque.Bounds(), src, src.Bounds().Min, draw.Over)
	var out bytes.Buffer
	if e = jpeg.Encode(&out, opaque, &jpeg.Options{Quality: 90}); e != nil {
		return nil, e
	}
	if out.Len() > MaxImageBytes {
		return nil, errors.New("Normalized image is too large")
	}
	return out.Bytes(), nil
}
