package render_test

import (
	"context"
	"errors"
	"fmt"
	"image"
	"os"
	"path/filepath"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/lestrrat-3d/decad"
	"github.com/lestrrat-3d/sketch"
	"github.com/lestrrat-3d/solidlens"
	"github.com/lestrrat-3d/units"
	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

// centeredBlock extrudes a block w mm along X and 10 mm along Y, centred on
// the Z axis and 10 mm tall, into a fresh decad document. It returns errors
// rather than failing a test so a Builder can call it from any worker.
func centeredBlock(ctx context.Context, w float64) (*decad.Body, error) {
	world := sketch.NewWorld()
	s, err := world.CreateSketch(world.XY())
	if err != nil {
		return nil, err
	}
	rect := s.CreateRectangle(-w/2, -5, w/2, 5)
	s.Fix(rect.A)
	if _, err := s.Solve(ctx); err != nil {
		return nil, err
	}
	return decad.New().Extrude(s, s.Profiles()[0], decad.Distance{D: units.Millimeters(10), Dir: decad.Along})
}

// widthBuilder builds centeredBlock(params["width"]) and counts its calls. It
// returns errBuild for a width above failAbove, when failAbove is set.
type widthBuilder struct {
	calls     atomic.Int32
	failAbove units.Value
}

var errBuild = errors.New("width out of range")

func (b *widthBuilder) Build(ctx context.Context, p kinetograph.Params) (*decad.Body, error) {
	b.calls.Add(1)
	w, err := p["width"].In(units.Millimeter)
	if err != nil {
		return nil, err
	}
	if b.failAbove.Kind() == units.Length {
		limit, err := b.failAbove.In(units.Millimeter)
		if err != nil {
			return nil, err
		}
		if w > limit {
			return nil, fmt.Errorf("%w: %s", errBuild, p["width"])
		}
	}
	return centeredBlock(ctx, w)
}

// steppedWidth is 10 mm for frames 0 to 2 of a 6 fps clip and 20 mm for
// frames 3 to 5: two distinct tuples over six frames.
func steppedWidth(t *testing.T) *kinetograph.Channel {
	t.Helper()
	return channel(t,
		kinetograph.Keyframe{At: 0, Value: units.Millimeters(10)},
		kinetograph.Keyframe{At: 2 * time.Second / 6, Value: units.Millimeters(10)},
		kinetograph.Keyframe{At: 3 * time.Second / 6, Value: units.Millimeters(20)},
	)
}

// parametricClip is a 1 s, 6 fps clip of one parametric part "block" on the
// root, built by b from width, seen by the side camera.
func parametricClip(t *testing.T, b kinetograph.Builder, width *kinetograph.Channel) *kinetograph.Clip {
	t.Helper()
	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	require.NoError(t, scene.AddParametric("block", rig.Root(), b, map[string]*kinetograph.Channel{"width": width}))
	require.NoError(t, scene.SetCamera(rig.Root(), sideCamera()))
	return newClip(t, scene, 6, time.Second)
}

// blobWidth is the number of pixel columns that hold a pixel differing from bg.
func blobWidth(img *image.RGBA, bg solidlens.Color) int {
	want := bg.NRGBA()
	b := img.Bounds()
	lo, hi := b.Max.X, b.Min.X-1
	for py := b.Min.Y; py < b.Max.Y; py++ {
		for px := b.Min.X; px < b.Max.X; px++ {
			c := img.RGBAAt(px, py)
			if c.R == want.R && c.G == want.G && c.B == want.B {
				continue
			}
			lo = min(lo, px)
			hi = max(hi, px)
		}
	}
	return hi - lo + 1
}

func TestFrameRebuildsParametricBody(t *testing.T) {
	b := &widthBuilder{}
	r := newRenderer(t, parametricClip(t, b, steppedWidth(t)), baseStyle())
	require.Equal(t, int32(0), b.calls.Load(), "New builds nothing")

	narrow, err := r.Frame(t.Context(), 0)
	require.NoError(t, err)
	wide, err := r.Frame(t.Context(), 5)
	require.NoError(t, err)
	require.Equal(t, int32(2), b.calls.Load(), "each Frame call builds once")

	nw, ww := blobWidth(narrow, white), blobWidth(wide, white)
	require.GreaterOrEqual(t, ww-nw, 4, "blob width %d px at 10 mm, %d px at 20 mm", nw, ww)
	cx, _, _ := centroid(wide, white)
	require.InDelta(t, imgW/2.0, cx, 1, "the block grows about the Z axis")

	// The rebuilt 20 mm body draws exactly as the same body attached rigidly.
	rigidBody, err := centeredBlock(t.Context(), 20)
	require.NoError(t, err)
	rig := kinetograph.NewRig()
	rigid := newRenderer(t, newClip(t, oneBlockScene(t, rig, rig.Root(), rigidBody), 6, time.Second), baseStyle())
	want, err := rigid.Frame(t.Context(), 5)
	require.NoError(t, err)
	require.Equal(t, encode(t, want), encode(t, wide))
}

func TestSequenceBuildsOncePerTuple(t *testing.T) {
	files := map[int][][]byte{}
	for _, workers := range []int{1, 3} {
		t.Run(fmt.Sprintf("%d workers", workers), func(t *testing.T) {
			b := &widthBuilder{}
			r := newRenderer(t, parametricClip(t, b, steppedWidth(t)), baseStyle())
			require.Equal(t, int32(0), b.calls.Load(), "New builds nothing")
			dir := t.TempDir()
			seq, err := r.Sequence(t.Context(), dir, render.WithWorkers(workers))
			require.NoError(t, err)
			require.Equal(t, 6, seq.Frames)
			require.Equal(t, int32(2), b.calls.Load(), "one Build per distinct width")

			for i := range seq.Frames {
				got, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf(seq.Pattern, i)))
				require.NoError(t, err)
				files[workers] = append(files[workers], got)
			}
			// Frames 0 to 2 share one body and 3 to 5 another; the halves differ.
			require.Equal(t, files[workers][0], files[workers][2])
			require.Equal(t, files[workers][3], files[workers][5])
			require.NotEqual(t, files[workers][0], files[workers][3])
		})
	}
	require.Len(t, files[1], 6)
	require.Equal(t, files[1], files[3], "the worker count does not reach the bytes")
}

func TestSequenceReportsBuildFailure(t *testing.T) {
	// The width passes 15 mm first at frame 3 (20 mm), and stays there.
	for _, workers := range []int{1, 3} {
		t.Run(fmt.Sprintf("%d workers", workers), func(t *testing.T) {
			b := &widthBuilder{failAbove: units.Millimeters(15)}
			clip := parametricClip(t, b, steppedWidth(t))
			r := newRenderer(t, clip, baseStyle())
			dir := t.TempDir()
			seq, err := r.Sequence(t.Context(), dir, render.WithWorkers(workers))
			require.Nil(t, seq)

			var fe *render.FrameError
			require.ErrorAs(t, err, &fe)
			require.Equal(t, 3, fe.Index)
			require.Equal(t, clip.FrameTime(3), fe.Time)
			require.ErrorIs(t, err, errBuild)
			require.ErrorContains(t, err, `"block"`)

			names := listDir(t, dir)
			for i := range 3 {
				require.Contains(t, names, fmt.Sprintf("frame_%06d.png", i))
			}
			require.NotContains(t, names, "frame_000003.png")
			require.Len(t, names, 3, "files: %v", names)
			for _, n := range names {
				require.False(t, strings.HasSuffix(n, ".tmp"), "leftover temporary file %s", n)
			}
		})
	}
}

func TestFrameReportsBuildFailureAtFrameZero(t *testing.T) {
	b := &widthBuilder{failAbove: units.Millimeters(5)}
	clip := parametricClip(t, b, steppedWidth(t))
	r := newRenderer(t, clip, baseStyle()) // New calls no Builder, so it cannot fail here
	_, err := r.Frame(t.Context(), 0)
	var fe *render.FrameError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, 0, fe.Index)
	require.Equal(t, time.Duration(0), fe.Time)
	require.ErrorIs(t, err, errBuild)
}

func TestStyleNamesParametricPart(t *testing.T) {
	style := baseStyle()
	style.Parts = map[string]render.Appearance{"block": {Material: flat(blue)}}
	r := newRenderer(t, parametricClip(t, &widthBuilder{}, steppedWidth(t)), style)
	img, err := r.Frame(t.Context(), 4)
	require.NoError(t, err)
	require.Greater(t, count(img, blue), 40)
	require.Equal(t, 0, count(img, red), "the default appearance is not used")
}
