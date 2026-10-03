package render_test

import (
	"bytes"
	"context"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
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

var green = solidlens.RGB(0, 1, 0)

// tripAfter is a context that is live for its first k Err calls and
// cancelled from call k+1 on: Err returns context.Canceled and Done is closed.
// Every Err call on a successful render path is a check that stops the work,
// so tripping at each k in turn stops the render at each of its checks.
type tripAfter struct {
	context.Context //nolint:containedctx // a test context wrapper that overrides Err and Done
	k               int64
	calls           atomic.Int64
	once            sync.Once
	done            chan struct{}
}

func newTripAfter(parent context.Context, k int64) *tripAfter {
	return &tripAfter{Context: parent, k: k, done: make(chan struct{})}
}

func (c *tripAfter) Err() error {
	if c.calls.Add(1) > c.k {
		c.once.Do(func() { close(c.done) })
		return context.Canceled
	}
	return c.Context.Err()
}

func (c *tripAfter) Done() <-chan struct{} { return c.done }

func TestFrameCancelledBetweenLayers(t *testing.T) {
	// Two fading parts: three layers per frame.
	clip, style := twoBlocks(t, []string{partFront, partBack}, fade(0.4), fade(0.7))
	r := newRenderer(t, clip, style)

	count := newTripAfter(t.Context(), math.MaxInt64)
	_, err := r.Frame(count, 0)
	require.NoError(t, err)
	checks := count.calls.Load()
	require.GreaterOrEqual(t, checks, int64(4), "one check to evaluate the frame and at least one per layer")

	for k := range checks {
		ctx := newTripAfter(t.Context(), k)
		img, err := r.Frame(ctx, 0)
		require.Nil(t, img, "tripped after %d checks", k)
		require.Equal(t, context.Canceled, err, "tripped after %d checks", k)
	}
}

func TestSequenceCancelledBetweenLayers(t *testing.T) {
	front, style := twoBlocks(t, []string{partFront, partBack}, fade(0.4), fade(0.7))
	clip := newClip(t, front.Scene(), 1, time.Second) // one frame
	r := newRenderer(t, clip, style)

	count := newTripAfter(t.Context(), math.MaxInt64)
	_, err := r.Frame(count, 0)
	require.NoError(t, err)
	// Sequence checks ctx once before it claims the frame, then the frame
	// makes the same checks Frame does. Tripping at any of them leaves no
	// file.
	checks := 1 + count.calls.Load()

	for k := range checks {
		dir := t.TempDir()
		ctx := newTripAfter(t.Context(), k)
		seq, err := r.Sequence(ctx, dir)
		require.Nil(t, seq, "tripped after %d checks", k)
		require.Equal(t, context.Canceled, err, "tripped after %d checks", k)
		require.Empty(t, listDir(t, dir), "tripped after %d checks", k)
	}
}

// staticMesh is a solidlens.TriangleSource over fixed slices.
type staticMesh struct {
	vertices  []r3.Vec
	triangles [][3]int
}

func (m staticMesh) Vertices() []r3.Vec  { return m.vertices }
func (m staticMesh) Triangles() [][3]int { return m.triangles }

func TestOpaqueFrameMatchesRenderPNG(t *testing.T) {
	rig := kinetograph.NewRig()
	turn, err := rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, ramp(t, units.Degrees(0), units.Degrees(90)))
	require.NoError(t, err)
	left, err := rig.Root().Fixed(translation(t, r3.Vec{X: -15}))
	require.NoError(t, err)
	scene := newScene(t, rig,
		map[string]*kinetograph.Node{partLeft: left, partRight: turn}, []string{partLeft, partRight},
		map[string]*decad.Body{partLeft: block(t, -5), partRight: block(t, 5)}, rig.Root(), sideCamera())
	require.NoError(t, scene.AddLight(lightSun, turn, kinetograph.Light{Kind: kinetograph.PointLight, Position: r3.Vec{X: 10, Y: -40, Z: 25}}))
	clip := newClip(t, scene, 6, time.Second)

	style := baseStyle()
	style.Parts = map[string]render.Appearance{
		partLeft:  {Material: solidlens.Matte(blue), Edges: solidlens.Outline(black), Fade: fade(1)},
		partRight: {Material: solidlens.Matte(red), Edges: solidlens.Outline(black)},
	}
	style.DirectionalLights = []solidlens.DirectionalLight{{Direction: r3.Vec{X: -0.4, Y: 1, Z: -0.7}, Color: white, Intensity: 0.8}}
	style.Lights = map[string]render.LightAppearance{lightSun: {Color: white, Intensity: fade(2500)}}
	r := newRenderer(t, clip, style)

	const frame = 4
	got, err := r.Frame(t.Context(), frame)
	require.NoError(t, err)
	dir := t.TempDir()
	seq, err := r.Sequence(t.Context(), dir)
	require.NoError(t, err)
	file, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf(seq.Pattern, frame)))
	require.NoError(t, err)

	// The same scene assembled by hand and written by solidlens.RenderPNG.
	f, err := clip.Frame(t.Context(), frame)
	require.NoError(t, err)
	models := make([]solidlens.Model, len(f.Poses))
	for k, pose := range f.Poses {
		mesh, err := pose.Body.Tessellate(t.Context(), style.Chord, decad.WithVerification(decad.VerifyNone))
		require.NoError(t, err)
		vertices := make([]r3.Vec, len(mesh.Vertices()))
		for v, p := range mesh.Vertices() {
			vertices[v] = pose.Transform.Apply(p)
		}
		a := style.Parts[pose.Name]
		models[k] = solidlens.Model{Mesh: staticMesh{vertices, mesh.Triangles()}, Material: a.Material, Edges: a.Edges}
	}
	fov, err := f.Camera.FOV.In(units.Degree)
	require.NoError(t, err)
	var want bytes.Buffer
	require.NoError(t, solidlens.RenderPNG(t.Context(), &want, solidlens.Scene{
		Camera:            solidlens.Camera{Position: f.Camera.Position, Target: f.Camera.Target, Up: f.Camera.Up, FOV: fov},
		Models:            models,
		DirectionalLights: style.DirectionalLights,
		PointLights:       []solidlens.PointLight{{Position: f.Lights[0].Position, Color: white, Intensity: 2500}},
		Background:        style.Background,
	}, solidlens.Settings{Width: imgW, Height: imgH}))

	require.Equal(t, want.Bytes(), encode(t, got), "Frame")
	require.Equal(t, want.Bytes(), file, "Sequence")
}

func TestFadingParametricPartBuildsOncePerTuple(t *testing.T) {
	style := withFade(baseStyle(), fade(0.5))
	style.Default.Edges = solidlens.Outline(black)

	b := &widthBuilder{}
	r := newRenderer(t, parametricClip(t, b, steppedWidth(t)), style)
	_, err := r.Frame(t.Context(), 0)
	require.NoError(t, err)
	require.Equal(t, int32(1), b.calls.Load(), "one build for a frame of two layers")
	_, err = r.Frame(t.Context(), 5)
	require.NoError(t, err)
	require.Equal(t, int32(2), b.calls.Load(), "each Frame call builds once")

	for _, workers := range []int{1, 3} {
		t.Run(fmt.Sprintf("%d workers", workers), func(t *testing.T) {
			b := &widthBuilder{}
			r := newRenderer(t, parametricClip(t, b, steppedWidth(t)), style)
			_, err := r.Sequence(t.Context(), t.TempDir(), render.WithWorkers(workers))
			require.NoError(t, err)
			require.Equal(t, int32(2), b.calls.Load(), "one build per distinct width, not per layer")
		})
	}
}

const (
	partNear = "near"
	partMid  = "mid"
	partFar  = "far"
)

// threeBlocks is a red block "near" centred on the camera axis, a larger
// green block "mid" behind it and a still larger blue block "far" behind
// that, flat-shaded with no lights, in a 4 fps clip. The image centre sees
// all three.
func threeBlocks(t *testing.T, near, mid, far *kinetograph.Channel) (*kinetograph.Clip, render.Style) {
	t.Helper()
	rig := kinetograph.NewRig()
	scene := newScene(t, rig,
		map[string]*kinetograph.Node{partNear: rig.Root(), partMid: rig.Root(), partFar: rig.Root()},
		[]string{partFar, partNear, partMid},
		map[string]*decad.Body{
			partNear: block(t, -5),
			partMid:  decadtest.NewBlock(t, decad.New(), -10, 15, 10, 25, units.Millimeters(20)),
			partFar:  decadtest.NewBlock(t, decad.New(), -15, 35, 15, 45, units.Millimeters(30)),
		}, rig.Root(), sideCamera())
	style := baseStyle()
	style.Parts = map[string]render.Appearance{
		partNear: {Material: flat(red), Fade: near},
		partMid:  {Material: flat(green), Fade: mid},
		partFar:  {Material: flat(blue), Fade: far},
	}
	return newClip(t, scene, 4, time.Second), style
}

// layered is the composite at a pixel where each surface, listed near to far
// with its fade, hides the ones behind it and differs from them:
// R = f · c + (1 − f) · R_behind from the farthest surface in, over white,
// rounded half up once. A fade of 0 leaves R alone and a fade of 1 replaces
// it, so the formula also covers hidden and opaque parts.
func layered(colors [][4]uint8, fades []float64) [4]uint8 {
	acc := [4]float64{255, 255, 255, 255}
	for j, color := range slices.Backward(colors) {
		for c := range acc {
			acc[c] = float64(fades[j]*float64(color[c])) + float64((1-fades[j])*acc[c])
		}
	}
	var out [4]uint8
	for c, v := range acc {
		out[c] = uint8(math.Floor(v + 0.5))
	}
	return out
}

func TestThreeFadingParts(t *testing.T) {
	steps := []float64{0, 0.001, 0.999, 1}
	fixed := []float64{0.3, 0.5, 0.7} // near, mid, far
	colors := [][4]uint8{{255, 0, 0, 255}, {0, 255, 0, 255}, {0, 0, 255, 255}}
	for varied, name := range []string{partNear, partMid, partFar} {
		t.Run(name, func(t *testing.T) {
			channels := []*kinetograph.Channel{fade(fixed[0]), fade(fixed[1]), fade(fixed[2])}
			channels[varied] = fadeSteps(t, steps...)
			clip, style := threeBlocks(t, channels[0], channels[1], channels[2])
			r := newRenderer(t, clip, style)

			var px [4][4]uint8
			for i, f := range steps {
				img, err := r.Frame(t.Context(), i)
				require.NoError(t, err)
				px[i] = centre(img)
				fades := append([]float64(nil), fixed...)
				fades[varied] = f
				require.Equal(t, layered(colors, fades), px[i], "%s at %v", name, f)
			}
			within1(t, px[0], px[1], name+" at 0 and 0.001")
			within1(t, px[3], px[2], name+" at 1 and 0.999")
		})
	}
}

func TestEqualDepthKeysFollowPartsOrder(t *testing.T) {
	// Both blocks' bounds are centred on (0, 0, 5), so their depth keys are
	// equal. "deep" reaches y = -8, in front of "wide" at the image centre.
	const deep, wide = "deep", "wide"
	render2 := func(order []string) [4]uint8 {
		rig := kinetograph.NewRig()
		scene := newScene(t, rig,
			map[string]*kinetograph.Node{deep: rig.Root(), wide: rig.Root()}, order,
			map[string]*decad.Body{
				deep: decadtest.NewBlock(t, decad.New(), -2, -8, 2, 8, units.Millimeters(10)),
				wide: decadtest.NewBlock(t, decad.New(), -8, -2, 8, 2, units.Millimeters(10)),
			}, rig.Root(), sideCamera())
		style := baseStyle()
		style.Parts = map[string]render.Appearance{
			deep: {Material: flat(red), Fade: fade(0.5)},
			wide: {Material: flat(blue), Fade: fade(0.5)},
		}
		img, err := newRenderer(t, newClip(t, scene, 24, time.Second), style).Frame(t.Context(), 0)
		require.NoError(t, err)
		return centre(img)
	}

	// deep first: deep is layered nearest, and S_2 shows wide alone, so the
	// blue mixes in: 0.5 · red + 0.5 · (0.5 · blue + 0.5 · white).
	require.Equal(t, [4]uint8{191, 64, 128, 255}, render2([]string{deep, wide}))
	// wide first: wide is layered nearest, but at the centre S_1 and S_2 both
	// show deep, so wide adds nothing: 0.5 · red + 0.5 · white.
	require.Equal(t, [4]uint8{255, 128, 128, 255}, render2([]string{wide, deep}))
}

func TestHiddenPartBesideOpaquePart(t *testing.T) {
	rig := kinetograph.NewRig()
	left, err := rig.Root().Fixed(translation(t, r3.Vec{X: -12}))
	require.NoError(t, err)
	right, err := rig.Root().Fixed(translation(t, r3.Vec{X: 12}))
	require.NoError(t, err)
	edged := render.Appearance{Material: solidlens.Matte(red), Edges: solidlens.Outline(black)}
	light := []solidlens.DirectionalLight{{Direction: r3.Vec{X: -0.4, Y: 1, Z: -0.7}, Color: white, Intensity: 1}}

	both := newScene(t, rig,
		map[string]*kinetograph.Node{partLeft: left, partRight: right}, []string{partLeft, partRight},
		map[string]*decad.Body{partLeft: block(t, -5), partRight: block(t, -5)}, rig.Root(), sideCamera())
	style := baseStyle()
	style.DirectionalLights = light
	hidden := edged
	hidden.Fade = fade(0)
	style.Parts = map[string]render.Appearance{partLeft: hidden, partRight: edged}
	got, err := newRenderer(t, newClip(t, both, 24, time.Second), style).Frame(t.Context(), 0)
	require.NoError(t, err)

	alone := newScene(t, rig,
		map[string]*kinetograph.Node{partRight: right}, []string{partRight},
		map[string]*decad.Body{partRight: block(t, -5)}, rig.Root(), sideCamera())
	style = baseStyle()
	style.DirectionalLights = light
	style.Parts = map[string]render.Appearance{partRight: edged}
	want, err := newRenderer(t, newClip(t, alone, 24, time.Second), style).Frame(t.Context(), 0)
	require.NoError(t, err)

	require.Equal(t, encode(t, want), encode(t, got))
	_, _, n := centroid(got, white)
	require.Greater(t, n, 40, "the opaque part is drawn")
}
