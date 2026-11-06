package bmwapi

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"sync"
)

// The simulated vehicle's picture: a saloon seen from the side, drawn in code (no
// vendor's render in the repository), on a transparent ground as BMW's renders are, at
// the width the client keeps (640 px).
const (
	pictureW, pictureH = 640, 280
	// samples per pixel and axis: 4×4 points smooth the edges.
	samples = 4
)

// shape is a region of the picture.
type shape interface{ contains(x, y float64) bool }

type polygon [][2]float64

// contains tests a point by the even-odd rule.
func (p polygon) contains(x, y float64) bool {
	in := false
	for i, j := 0, len(p)-1; i < len(p); j, i = i, i+1 {
		xi, yi, xj, yj := p[i][0], p[i][1], p[j][0], p[j][1]
		if (yi > y) != (yj > y) && x < (xj-xi)*(y-yi)/(yj-yi)+xi {
			in = !in
		}
	}
	return in
}

type ellipse struct{ cx, cy, rx, ry float64 }

func (e ellipse) contains(x, y float64) bool {
	dx, dy := (x-e.cx)/e.rx, (y-e.cy)/e.ry
	return dx*dx+dy*dy <= 1
}

// wheel is a disc of radius r on the axle of the wheel at cx.
func wheel(cx, r float64) ellipse { return ellipse{cx, wheelY, r, r} }

// wheelY is the wheels' axle height.
const wheelY = 210

// layer is a shape painted in a colour, over the layers before it.
type layer struct {
	shape
	c color.NRGBA
}

// layers paint the car, its front to the left.
func layers() []layer {
	body := color.NRGBA{0x8a, 0x90, 0x96, 0xff} // a grey, as Brooklyn Grey
	sill := color.NRGBA{0x6e, 0x74, 0x7a, 0xff}
	glass := color.NRGBA{0x2b, 0x33, 0x3c, 0xff}
	tyre := color.NRGBA{0x1a, 0x1c, 0x1f, 0xff}
	rim := color.NRGBA{0xc3, 0xc8, 0xcd, 0xff}
	var ls []layer
	add := func(s shape, c color.NRGBA) { ls = append(ls, layer{s, c}) }
	add(ellipse{322, 258, 290, 11}, color.NRGBA{0, 0, 0, 0x40}) // the shadow on the ground
	add(polygon{
		{42, 222},
		{28, 204},
		{26, 180},
		{36, 162},
		{72, 150},
		{150, 140},
		{205, 132},
		{262, 92},
		{300, 80},
		{400, 78},
		{450, 86},
		{520, 124},
		{578, 134},
		{606, 148},
		{614, 172},
		{612, 206},
		{598, 222},
	}, body)
	add(polygon{{60, 206}, {590, 206}, {586, 222}, {64, 222}}, sill)
	add(polygon{{218, 128}, {270, 92}, {336, 86}, {336, 128}}, glass)
	add(polygon{{346, 128}, {346, 86}, {398, 85}, {442, 92}, {500, 126}}, glass)
	add(polygon{{30, 172}, {38, 162}, {84, 154}, {80, 166}}, color.NRGBA{0xe8, 0xee, 0xf4, 0xff})     // a headlight
	add(polygon{{584, 140}, {606, 150}, {609, 162}, {584, 152}}, color.NRGBA{0xb3, 0x1b, 0x1b, 0xff}) // a rear light
	for _, cx := range []float64{148, 494} {
		add(wheel(cx, 58), color.NRGBA{0x24, 0x27, 0x2b, 0xff}) // the wheel arch
		add(wheel(cx, 50), tyre)
		add(wheel(cx, 33), rim)
		add(wheel(cx, 27), color.NRGBA{0x8e, 0x94, 0x9a, 0xff})
		add(wheel(cx, 7), rim)
	}
	return ls
}

// picture is the simulated vehicle's picture, a PNG drawn once.
var picture = sync.OnceValue(func() []byte {
	ls := layers()
	img := image.NewNRGBA(image.Rect(0, 0, pictureW, pictureH))
	for y := range pictureH {
		for x := range pictureW {
			// Each sample is painted layer over layer, then the samples are averaged.
			var r, g, b, a float64
			for sy := range samples {
				for sx := range samples {
					px, py := float64(x)+(float64(sx)+0.5)/samples, float64(y)+(float64(sy)+0.5)/samples
					var cr, cg, cb, ca float64 // premultiplied
					for _, l := range ls {
						if !l.contains(px, py) {
							continue
						}
						la := float64(l.c.A) / 255
						cr = float64(l.c.R)*la + cr*(1-la)
						cg = float64(l.c.G)*la + cg*(1-la)
						cb = float64(l.c.B)*la + cb*(1-la)
						ca = 255*la + ca*(1-la)
					}
					r, g, b, a = r+cr, g+cg, b+cb, a+ca
				}
			}
			n := float64(samples * samples)
			if a == 0 {
				continue
			}
			img.SetNRGBA(x, y, color.NRGBA{
				R: uint8(r / a * 255), G: uint8(g / a * 255), B: uint8(b / a * 255), A: uint8(a / n), //nolint:gosec // within 0-255
			})
		}
	}
	var buf bytes.Buffer
	if err := (&png.Encoder{CompressionLevel: png.BestCompression}).Encode(&buf, img); err != nil {
		panic(err)
	}
	return buf.Bytes()
})

// image answers the vehicle's picture, a PNG (the OpenAPI's */*).
func (h *Handler) image(w http.ResponseWriter, r *http.Request) {
	if h.carOf(w, r) == nil {
		return
	}
	w.Header().Set("Content-Type", "image/png")
	_, _ = w.Write(picture())
}
