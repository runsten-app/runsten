package bmw

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/jpeg"
	"image/png"
)

// PictureWidth is the widest a picture is kept: the width the relay asks of Volvo's CDN,
// enough for a thumbnail and a tile.
const PictureWidth = 640

// fit scales a PNG or a JPEG wider than PictureWidth down to it, each pixel the average of
// the source pixels it covers, and encodes it again in its format. A WebP, which the
// standard library does not read, and a picture narrow enough are kept as they are.
func fit(data []byte, mediaType string) ([]byte, error) {
	if mediaType != "image/png" && mediaType != "image/jpeg" {
		return data, nil
	}
	src, _, err := image.Decode(bytes.NewReader(data))
	if err != nil {
		return nil, fmt.Errorf("decode: %w", err)
	}
	b := src.Bounds()
	if b.Dx() <= PictureWidth {
		return data, nil
	}
	h := max(1, b.Dy()*PictureWidth/b.Dx())
	dst := shrink(src, PictureWidth, h)
	var out bytes.Buffer
	if mediaType == "image/png" {
		err = (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&out, dst)
	} else {
		err = jpeg.Encode(&out, dst, &jpeg.Options{Quality: 85})
	}
	if err != nil {
		return nil, fmt.Errorf("encode: %w", err)
	}
	return out.Bytes(), nil
}

// shrink scales src down to w×h by averaging, in premultiplied colours, the source pixels
// each destination pixel covers: the transparent ground of a render stays clean.
func shrink(src image.Image, w, h int) *image.RGBA64 {
	b := src.Bounds()
	dst := image.NewRGBA64(image.Rect(0, 0, w, h))
	for y := range h {
		y0, y1 := b.Min.Y+y*b.Dy()/h, b.Min.Y+(y+1)*b.Dy()/h
		y1 = max(y1, y0+1)
		for x := range w {
			x0, x1 := b.Min.X+x*b.Dx()/w, b.Min.X+(x+1)*b.Dx()/w
			x1 = max(x1, x0+1)
			var r, g, bl, a, n uint64
			for sy := y0; sy < y1; sy++ {
				for sx := x0; sx < x1; sx++ {
					pr, pg, pb, pa := src.At(sx, sy).RGBA()
					r, g, bl, a, n = r+uint64(pr), g+uint64(pg), bl+uint64(pb), a+uint64(pa), n+1
				}
			}
			dst.SetRGBA64(x, y, color.RGBA64{R: uint16(r / n), G: uint16(g / n), B: uint16(bl / n), A: uint16(a / n)}) //nolint:gosec // averages of 16-bit values
		}
	}
	return dst
}
