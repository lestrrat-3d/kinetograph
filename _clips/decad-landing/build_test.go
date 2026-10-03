package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

// TestToolsHideTheHoles renders act A at 3.9 s, when every tool has landed in
// its hole, with the drilled plate and, for each hole in turn, with that hole
// at its shallowest (0.5 mm blind) instead of through. A landed tool must hide
// its hole: at most 0.1 % of the pixels may differ by more than 2 levels in a
// channel. A failure means the tools' 0.3 mm radius margin must grow.
//
// Two things in solidlens set the slack. It shades each triangle flat, so
// under the point light a top face triangulated differently around a hole
// shifts large triangles by 1 or 2 levels. It interpolates depth linearly in
// screen space, so where a tool's wall meets the plate's top face the drawn
// boundary moves by a pixel or two with that triangulation. For the same
// reason the comparison is not against the undrilled blank: the blank's top
// face is two triangles across, and its depth error shows a crescent of each
// tool's wall below the face, which no margin removes.
func TestToolsHideTheHoles(t *testing.T) {
	ctx := t.Context()
	const fps, width, height = 30, 1280, 720
	at := ms(3900)

	frame := func(t *testing.T, depths map[string]float64) []byte {
		t.Helper()
		plate, err := flangeBody(ctx, flangeShape{height: plateThickness, depths: depths})
		require.NoError(t, err)
		ch, err := landingScript().Channels(0)
		require.NoError(t, err)
		take, err := assembleBuild(ctx, ch, plate)
		require.NoError(t, err)
		clip, err := kinetograph.NewClip(take.Scene, fps, 13*time.Second)
		require.NoError(t, err)
		style := take.Style
		style.Width, style.Height = width, height
		r, err := render.New(ctx, clip, style)
		require.NoError(t, err)
		i := int(at * fps / time.Second)
		require.Equal(t, at, clip.FrameTime(i))
		img, err := r.Frame(ctx, i)
		require.NoError(t, err)
		return img.Pix
	}
	throughAll := func() map[string]float64 {
		depths := make(map[string]float64, len(drills))
		for _, d := range drills {
			depths[d.name] = plateThickness + holeClearance
		}
		return depths
	}
	drilled := frame(t, throughAll())

	for _, d := range drills {
		t.Run(d.name, func(t *testing.T) {
			depths := throughAll()
			depths[d.name] = minHoleDepth
			blind := frame(t, depths)
			require.Len(t, blind, len(drilled))
			differ, shaded := 0, 0
			for p := 0; p < len(drilled); p += 4 {
				switch maxChannelDelta(drilled[p:p+4], blind[p:p+4]) {
				case 0:
				case 1, 2:
					shaded++
				default:
					differ++
				}
			}
			t.Logf("%d of %d pixels differ by more than 2 levels, %d by 1 or 2", differ, width*height, shaded)
			require.LessOrEqual(t, differ, width*height/1000)
		})
	}
}

// maxChannelDelta is the largest difference between two pixels' bytes.
func maxChannelDelta(a, b []byte) int {
	most := 0
	for i := range a {
		most = max(most, abs(int(a[i])-int(b[i])))
	}
	return most
}

func abs(n int) int {
	if n < 0 {
		return -n
	}
	return n
}

// TestFlangeBuilderStages evaluates the parametric flange before and after
// its height ramp and at the end of the act, and checks each body's measured
// height.
func TestFlangeBuilderStages(t *testing.T) {
	ctx := t.Context()
	ch, err := landingScript().Channels(0)
	require.NoError(t, err)
	take, err := buildTake(ctx, ch)
	require.NoError(t, err)
	cache := kinetograph.NewBuildCache()
	for _, c := range []struct {
		at     time.Duration
		height float64
	}{
		{0, 2},
		{ms(1500), plateThickness},
		{ms(13000) - time.Nanosecond, plateThickness},
	} {
		f, err := take.Scene.AtCached(ctx, c.at, cache)
		require.NoError(t, err)
		require.Equal(t, "flange", f.Poses[0].Name)
		box, err := f.Poses[0].Body.Bounds()
		require.NoError(t, err)
		require.InDelta(t, c.height, box.Max.Z-box.Min.Z, 1e-6, "height at %s", c.at)
	}
}
