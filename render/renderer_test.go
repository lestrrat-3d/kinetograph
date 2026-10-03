package render_test

import (
	"testing"
	"time"

	"github.com/lestrrat-3d/decad"
	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/solidlens"
	"github.com/lestrrat-3d/units"
	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

func TestFrameCentersAStaticBlock(t *testing.T) {
	rig := kinetograph.NewRig()
	// Block spans x, y in [-5, 5] and z in [0, 10]: centered on the camera axis.
	scene := oneBlockScene(t, rig, rig.Root(), block(t, -5))
	r := newRenderer(t, newClip(t, scene, 24, time.Second), baseStyle())

	img, err := r.Frame(t.Context(), 0)
	require.NoError(t, err)
	require.Equal(t, imgW, img.Bounds().Dx())
	require.Equal(t, imgH, img.Bounds().Dy())
	cx, cy, n := centroid(img, white)
	require.Greater(t, n, 80, "the block should cover a visible area")
	require.InDelta(t, imgW/2.0, cx, 1)
	require.InDelta(t, imgH/2.0, cy, 1)
}

func TestFrameFollowsPrismaticSlide(t *testing.T) {
	rig := kinetograph.NewRig()
	slide, err := rig.Root().Prismatic(r3.Vec{X: 1}, ramp(t, units.Millimeters(0), units.Millimeters(24)))
	require.NoError(t, err)
	scene := oneBlockScene(t, rig, slide, block(t, -5))
	r := newRenderer(t, newClip(t, scene, 24, time.Second), baseStyle())

	first, err := r.Frame(t.Context(), 0)
	require.NoError(t, err)
	later, err := r.Frame(t.Context(), 12) // 12 mm along +X
	require.NoError(t, err)
	x0, y0, _ := centroid(first, white)
	x1, y1, _ := centroid(later, white)
	require.GreaterOrEqual(t, x1-x0, 2.0, "centroid x moved %.2f px", x1-x0)
	require.InDelta(t, y0, y1, 1)
}

func TestFrameMirrorsAfterHalfTurn(t *testing.T) {
	rig := kinetograph.NewRig()
	// The block sits 10 to 20 mm off the Z axis; half a turn about that axis
	// puts it on the other side, mirrored in X about the image centre.
	turn, err := rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, ramp(t, units.Degrees(0), units.Degrees(180)))
	require.NoError(t, err)
	scene := oneBlockScene(t, rig, turn, block(t, 10))
	r := newRenderer(t, newClip(t, scene, 1, 2*time.Second), baseStyle()) // frames at 0 s and 1 s

	a, err := r.Frame(t.Context(), 0)
	require.NoError(t, err)
	b, err := r.Frame(t.Context(), 1)
	require.NoError(t, err)
	ax, ay, _ := centroid(a, white)
	bx, by, _ := centroid(b, white)
	require.Greater(t, ax, imgW/2.0+3, "the block starts right of centre")
	require.InDelta(t, imgW-ax, bx, 1)
	require.InDelta(t, ay, by, 1)
}

func TestFrameDrawsBothSidesOfASheet(t *testing.T) {
	rig := kinetograph.NewRig()
	// Seen from above and in front, the near wall shows its outer (front) side
	// and the far wall, through the open top, its inner (back) side.
	cam := kinetograph.Camera{
		Position: r3.Vec{Y: -45, Z: 55},
		Target:   r3.Vec{Z: 5},
		Up:       r3.Vec{Z: 1},
		FOV:      kinetograph.Constant(units.Degrees(40)),
	}
	scene := newScene(t, rig,
		map[string]*kinetograph.Node{"tube": rig.Root()}, []string{"tube"},
		map[string]*decad.Body{"tube": openTube(t)}, rig.Root(), cam)
	back := flat(blue)
	style := baseStyle()
	style.Default = render.Appearance{Material: flat(red), Back: &back}
	r := newRenderer(t, newClip(t, scene, 24, time.Second), style)

	img, err := r.Frame(t.Context(), 0)
	require.NoError(t, err)
	require.Greater(t, count(img, red), 20, "front color never drawn")
	require.Greater(t, count(img, blue), 20, "back color never drawn")

	// With no Back, both sides take the front material.
	plain := baseStyle()
	r = newRenderer(t, newClip(t, scene, 24, time.Second), plain)
	img, err = r.Frame(t.Context(), 0)
	require.NoError(t, err)
	require.Greater(t, count(img, red), 40)
	require.Equal(t, 0, count(img, blue))
}

func TestPartAppearanceOverridesDefault(t *testing.T) {
	rig := kinetograph.NewRig()
	left, err := rig.Root().Fixed(translation(t, r3.Vec{X: -20}))
	require.NoError(t, err)
	right, err := rig.Root().Fixed(translation(t, r3.Vec{X: 20}))
	require.NoError(t, err)
	scene := newScene(t, rig,
		map[string]*kinetograph.Node{partLeft: left, partRight: right}, []string{partLeft, partRight},
		map[string]*decad.Body{partLeft: block(t, -5), partRight: block(t, -5)}, rig.Root(), sideCamera())
	style := baseStyle()
	style.Parts = map[string]render.Appearance{partRight: {Material: flat(blue)}}
	r := newRenderer(t, newClip(t, scene, 24, time.Second), style)

	img, err := r.Frame(t.Context(), 0)
	require.NoError(t, err)
	// Left of centre is the default red block; right of centre the blue one.
	for _, tc := range []struct {
		x    int
		want solidlens.Color
	}{{imgW/2 - 22, red}, {imgW/2 + 22, blue}} {
		got := img.RGBAAt(tc.x, imgH/2)
		w := tc.want.NRGBA()
		require.Equal(t, [3]uint8{w.R, w.G, w.B}, [3]uint8{got.R, got.G, got.B}, "pixel at x=%d", tc.x)
	}
}

func TestFrameMatchesDecadPosedReference(t *testing.T) {
	rig := kinetograph.NewRig()
	center := r3.Vec{X: 2, Y: 1}
	turn, err := rig.Root().Revolute(center, r3.Vec{X: 1, Y: 2, Z: 3}, ramp(t, units.Degrees(0), units.Degrees(100)))
	require.NoError(t, err)
	body := block(t, 8)
	scene := oneBlockScene(t, rig, turn, body)
	clip := newClip(t, scene, 24, time.Second)
	style := baseStyle()
	r := newRenderer(t, clip, style)

	const frame = 17
	got, err := r.Frame(t.Context(), frame)
	require.NoError(t, err)

	// The reference moves the decad body itself, re-tessellates it and hands
	// it to solidlens directly: the same motion by the other route.
	f, err := clip.Frame(t.Context(), frame)
	require.NoError(t, err)
	placed, err := body.PlacedCopy(t.Context(), f.Poses[0].Transform)
	require.NoError(t, err)
	mesh, err := placed.Tessellate(t.Context(), style.Chord, decad.WithVerification(decad.VerifyNone))
	require.NoError(t, err)
	fov, err := f.Camera.FOV.In(units.Degree)
	require.NoError(t, err)
	want, err := solidlens.Render(t.Context(), solidlens.Scene{
		Camera:     solidlens.Camera{Position: f.Camera.Position, Target: f.Camera.Target, Up: f.Camera.Up, FOV: fov},
		Models:     []solidlens.Model{{Mesh: mesh, Material: style.Default.Material}},
		Background: style.Background,
	}, solidlens.Settings{Width: imgW, Height: imgH})
	require.NoError(t, err)

	gx, gy, gn := centroid(got, white)
	wx, wy, wn := centroid(want, white)
	require.Greater(t, wn, 80)
	require.Greater(t, gn, 80)
	require.InDelta(t, wx, gx, 0.5)
	require.InDelta(t, wy, gy, 0.5)

	same := 0
	for i := 0; i < len(got.Pix); i += 4 {
		if got.Pix[i] == want.Pix[i] && got.Pix[i+1] == want.Pix[i+1] && got.Pix[i+2] == want.Pix[i+2] {
			same++
		}
	}
	require.GreaterOrEqual(t, float64(same)/float64(imgW*imgH), 0.995, "%d of %d pixels agree", same, imgW*imgH)
}

func TestFrameIsDeterministic(t *testing.T) {
	rig := kinetograph.NewRig()
	turn, err := rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, ramp(t, units.Degrees(0), units.Degrees(90)))
	require.NoError(t, err)
	scene := oneBlockScene(t, rig, turn, block(t, 4))
	style := baseStyle()
	style.Default.Edges = solidlens.Outline(solidlens.RGB(0, 0, 0))
	r := newRenderer(t, newClip(t, scene, 24, time.Second), style)

	a, err := r.Frame(t.Context(), 7)
	require.NoError(t, err)
	b, err := r.Frame(t.Context(), 7)
	require.NoError(t, err)
	require.Equal(t, encode(t, a), encode(t, b))

	// A second renderer tessellates again and must still agree.
	r2 := newRenderer(t, newClip(t, scene, 24, time.Second), style)
	c, err := r2.Frame(t.Context(), 7)
	require.NoError(t, err)
	require.Equal(t, encode(t, a), encode(t, c))

	other, err := r.Frame(t.Context(), 8)
	require.NoError(t, err)
	require.NotEqual(t, encode(t, a), encode(t, other), "adjacent frames should differ")
}

func TestFrameRefusals(t *testing.T) {
	rig := kinetograph.NewRig()
	scene := oneBlockScene(t, rig, rig.Root(), block(t, -5))
	r := newRenderer(t, newClip(t, scene, 24, time.Second), baseStyle())

	for _, i := range []int{-1, 24} {
		_, err := r.Frame(t.Context(), i)
		require.ErrorIs(t, err, kinetograph.ErrFrameRange)
	}
	//nolint:staticcheck // a nil context is the case under test
	_, err := r.Frame(nil, 0)
	require.ErrorIs(t, err, kinetograph.ErrNilContext)

	cancelled, stop := contextCancelled(t)
	defer stop()
	_, err = r.Frame(cancelled, 0)
	require.Equal(t, cancelled.Err(), err, "a cancelled context is returned unwrapped")
}

func TestFrameErrorNamesTheFrame(t *testing.T) {
	r := failingRenderer(t)
	_, err := r.Frame(t.Context(), 4)
	var fe *render.FrameError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, 4, fe.Index)
	require.Equal(t, time.Duration(4)*time.Second/24, fe.Time)
	require.Error(t, fe.Unwrap())

	_, err = r.Frame(t.Context(), 3)
	require.NoError(t, err)
}
