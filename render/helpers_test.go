package render_test

import (
	"bytes"
	"image"
	"image/png"
	"testing"
	"time"

	"github.com/lestrrat-3d/decad"
	"github.com/lestrrat-3d/decad/decadtest"
	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/solidlens"
	"github.com/lestrrat-3d/units"
	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

const (
	imgW = 64
	imgH = 48
)

// Part and light names the tests attach.
const (
	partBlock = "block"
	partLeft  = "left"
	partRight = "right"
	partFront = "front"
	partBack  = "back"
	lightSun  = "sun"
)

var (
	white = solidlens.RGB(1, 1, 1)
	red   = solidlens.RGB(1, 0, 0)
	blue  = solidlens.RGB(0, 0, 1)
)

// flat is a material that shows its own color at full strength whatever the
// lights, so a pixel's color names the material that drew it.
func flat(c solidlens.Color) solidlens.Material {
	return solidlens.Material{Color: c, Ambient: 1}
}

func baseStyle() render.Style {
	return render.Style{
		Width:      imgW,
		Height:     imgH,
		Chord:      units.Millimeters(0.5),
		Background: white,
		Default:    render.Appearance{Material: flat(red)},
	}
}

// block is a 10 mm cube spanning (x, -5, 0) to (x+10, 5, 10).
func block(tb testing.TB, x float64) *decad.Body {
	tb.Helper()
	return decadtest.NewBlock(tb, decad.New(), x, -5, x+10, 5, units.Millimeters(10))
}

// openTube is a sheet body: the four 10 mm walls of a block spanning (-5, -5, 0)
// to (5, 5, 10), with no caps. Its outer side is the front side.
func openTube(tb testing.TB) *decad.Body {
	tb.Helper()
	s := decadtest.NewSketch(tb)
	rect := s.CreateRectangle(-5, -5, 5, 5)
	s.Fix(rect.A)
	p := decadtest.SolveRegion(tb, s)
	body, err := decad.New().Extrude(s, p, decad.Distance{D: units.Millimeters(10), Dir: decad.Along}, decad.WithSurfaceResult())
	require.NoError(tb, err)
	return body
}

// sideCamera looks at the point (0, 0, 5) from 60 mm out on -Y.
func sideCamera() kinetograph.Camera {
	return kinetograph.Camera{
		Position: r3.Vec{Y: -60, Z: 5},
		Target:   r3.Vec{Z: 5},
		Up:       r3.Vec{Z: 1},
		FOV:      kinetograph.Constant(units.Degrees(40)),
	}
}

func channel(tb testing.TB, keys ...kinetograph.Keyframe) *kinetograph.Channel {
	tb.Helper()
	c, err := kinetograph.NewChannel(keys...)
	require.NoError(tb, err)
	return c
}

// ramp is a channel that goes from a to b over one second.
func ramp(tb testing.TB, a, b units.Value) *kinetograph.Channel {
	tb.Helper()
	return channel(tb, kinetograph.Keyframe{At: 0, Value: a}, kinetograph.Keyframe{At: time.Second, Value: b})
}

func newScene(tb testing.TB, rig *kinetograph.Rig, parts map[string]*kinetograph.Node, order []string,
	bodies map[string]*decad.Body, camNode *kinetograph.Node, cam kinetograph.Camera) *kinetograph.Scene {
	tb.Helper()
	scene := kinetograph.NewScene(rig)
	for _, name := range order {
		require.NoError(tb, scene.AddPart(name, parts[name], bodies[name]))
	}
	require.NoError(tb, scene.SetCamera(camNode, cam))
	return scene
}

// oneBlockScene attaches one block to node and the side camera to the root.
func oneBlockScene(tb testing.TB, rig *kinetograph.Rig, node *kinetograph.Node, body *decad.Body) *kinetograph.Scene {
	tb.Helper()
	return newScene(tb, rig,
		map[string]*kinetograph.Node{partBlock: node}, []string{partBlock},
		map[string]*decad.Body{partBlock: body}, rig.Root(), sideCamera())
}

func newClip(tb testing.TB, scene *kinetograph.Scene, fps int, d time.Duration) *kinetograph.Clip {
	tb.Helper()
	clip, err := kinetograph.NewClip(scene, fps, d)
	require.NoError(tb, err)
	return clip
}

func newRenderer(tb testing.TB, clip *kinetograph.Clip, style render.Style) *render.Renderer {
	tb.Helper()
	r, err := render.New(tb.Context(), clip, style)
	require.NoError(tb, err)
	return r
}

// centroid returns the mean position, in pixel coordinates, of the pixels that
// differ from bg, and how many there are.
func centroid(img *image.RGBA, bg solidlens.Color) (x, y float64, n int) {
	want := bg.NRGBA()
	var sx, sy float64
	b := img.Bounds()
	for py := b.Min.Y; py < b.Max.Y; py++ {
		for px := b.Min.X; px < b.Max.X; px++ {
			c := img.RGBAAt(px, py)
			if c.R == want.R && c.G == want.G && c.B == want.B {
				continue
			}
			sx += float64(px) + 0.5
			sy += float64(py) + 0.5
			n++
		}
	}
	if n == 0 {
		return 0, 0, 0
	}
	return sx / float64(n), sy / float64(n), n
}

// count returns how many pixels of img are exactly c.
func count(img *image.RGBA, c solidlens.Color) int {
	want := c.NRGBA()
	n := 0
	b := img.Bounds()
	for py := b.Min.Y; py < b.Max.Y; py++ {
		for px := b.Min.X; px < b.Max.X; px++ {
			p := img.RGBAAt(px, py)
			if p.R == want.R && p.G == want.G && p.B == want.B {
				n++
			}
		}
	}
	return n
}

func encode(tb testing.TB, img image.Image) []byte {
	tb.Helper()
	var buf bytes.Buffer
	require.NoError(tb, png.Encode(&buf, img))
	return buf.Bytes()
}
