package main

import (
	"maps"
	"slices"
	"testing"
	"time"

	"github.com/lestrrat-3d/decad"
	"github.com/lestrrat-3d/units"
	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

// stillBuild is act A at the global time at, with the flange replaced by the
// still plate, restyled by restyle when it is non-nil, rendered at the clip's
// full size.
func stillBuild(t *testing.T, plate *decad.Body, at time.Duration, restyle func(*render.Style)) []byte {
	t.Helper()
	ctx := t.Context()
	ch, err := landingScript().Channels(0)
	require.NoError(t, err)
	take, err := assembleBuild(ctx, ch, plate)
	require.NoError(t, err)
	if restyle != nil {
		restyle(&take.Style)
	}
	return renderTake(t, take, 13*time.Second, at)
}

// renderTake renders take's frame at local time at, at 1280×720 and 30 fps,
// in a clip length long.
func renderTake(t *testing.T, take *Take, length, at time.Duration) []byte {
	t.Helper()
	const fps = 30
	ctx := t.Context()
	clip, err := kinetograph.NewClip(take.Scene, fps, length)
	require.NoError(t, err)
	style := take.Style
	style.Width, style.Height = testWidth, testHeight
	r, err := render.New(ctx, clip, style)
	require.NoError(t, err)
	i := int(at * fps / time.Second)
	require.Equal(t, at, clip.FrameTime(i))
	img, err := r.Frame(ctx, i)
	require.NoError(t, err)
	return img.Pix
}

const testWidth, testHeight = 1280, 720

// drilledPlate is the 16 mm flange with depths, by drill name, and every
// other hole through.
func drilledPlate(t *testing.T, depths map[string]float64) *decad.Body {
	t.Helper()
	all := make(map[string]float64, len(drills))
	for _, d := range drills {
		all[d.name] = plateThickness + holeClearance
	}
	maps.Copy(all, depths)
	plate, err := flangeBody(t.Context(), flangeShape{height: plateThickness, depths: all})
	require.NoError(t, err)
	return plate
}

// toolFades sets every drill tool's Fade to fade.
func toolFades(fade *kinetograph.Channel) func(*render.Style) {
	return func(s *render.Style) {
		parts := maps.Clone(s.Parts)
		for _, d := range drills {
			look := parts["tool."+d.name]
			look.Fade = fade
			parts["tool."+d.name] = look
		}
		s.Parts = parts
	}
}

// TestToolsAreSeeThrough renders act A at 3.9 s, when every tool stands in
// its through hole, three ways: as the clip draws it, with the tools opaque,
// and with the tools hidden. Wherever the opaque tools change a pixel by
// more than 32 levels, the clip's pixel lies part of the way from the
// hidden-tool pixel to the opaque-tool pixel, and the median of that part is
// toolFade: the tools show the plate behind them rather than a hole in the
// frame. The median, not the mean, because where two tools overlap on screen
// their fades compound.
func TestToolsAreSeeThrough(t *testing.T) {
	at := ms(3900)
	plate := drilledPlate(t, nil)
	clip := stillBuild(t, plate, at, nil)
	opaque := stillBuild(t, plate, at, toolFades(nil))
	hidden := stillBuild(t, plate, at, toolFades(kinetograph.Constant(units.Scalar(0))))

	var opacities []float64
	for p := 0; p < len(clip); p += 4 {
		// c is the channel the tool changes most at this pixel.
		c, most := 0, 0
		for k := range 3 {
			if d := abs(int(opaque[p+k]) - int(hidden[p+k])); d > most {
				c, most = k, d
			}
		}
		if most <= 32 {
			continue
		}
		opacities = append(opacities, float64(int(clip[p+c])-int(hidden[p+c]))/float64(int(opaque[p+c])-int(hidden[p+c])))
	}
	slices.Sort(opacities)
	median := opacities[len(opacities)/2]
	t.Logf("%d tool pixels, median opacity %.4f", len(opacities), median)
	require.Greater(t, len(opacities), testWidth*testHeight/100, "the tools cover at least 1 %% of the frame")
	require.InDelta(t, toolFade, median, 0.01)
}

// TestHolesShowThroughTools renders act A at 3.9 s with the drilled plate
// and, for each hole in turn, with that hole at its shallowest (0.5 mm blind)
// instead of through. Seen through its tool, the hole's depth must change
// the frame: at least 0.2 % of the pixels differ by more than 8 levels in a
// channel. The left bolt hole, which the bore's tool partly covers on screen,
// changes the fewest, about 0.28 %.
func TestHolesShowThroughTools(t *testing.T) {
	at := ms(3900)
	drilled := stillBuild(t, drilledPlate(t, nil), at, nil)
	for _, d := range drills {
		t.Run(d.name, func(t *testing.T) {
			blind := stillBuild(t, drilledPlate(t, map[string]float64{d.name: minHoleDepth}), at, nil)
			require.Len(t, blind, len(drilled))
			differ := 0
			for p := 0; p < len(drilled); p += 4 {
				if maxChannelDelta(drilled[p:p+4], blind[p:p+4]) > 8 {
					differ++
				}
			}
			t.Logf("%d of %d pixels differ by more than 8 levels", differ, testWidth*testHeight)
			require.GreaterOrEqual(t, differ, testWidth*testHeight/500)
		})
	}
}

// TestHolesDeepenOverFrames reads the flange's parameters at every frame of
// act A at 30 fps, as its Builder reads them. Each hole must be blind for at
// least five frames, so it deepens visibly inside its tool, and no frame may
// hold two blind holes, which decad refuses to cut.
func TestHolesDeepenOverFrames(t *testing.T) {
	ch, err := landingScript().Channels(0)
	require.NoError(t, err)
	tracks := map[string]string{"height": "flange.height", "fillet": "flange.fillet", "chamfer": "flange.chamfer"}
	for _, d := range drills {
		tracks[d.name] = "hole." + d.name
	}
	blindFrames := map[string]int{}
	for i := range 13 * 30 {
		at := time.Duration(i) * time.Second / 30
		params := kinetograph.Params{}
		for param, track := range tracks {
			c, err := ch.Get(track)
			require.NoError(t, err)
			params[param], err = c.At(at)
			require.NoError(t, err)
		}
		shape, err := shapeFromParams(params)
		require.NoError(t, err)
		blind, _ := splitHoles(shape)
		require.LessOrEqual(t, len(blind), 1, "blind holes at %s", at)
		for _, d := range blind {
			blindFrames[d.name]++
		}
	}
	for _, d := range drills {
		require.GreaterOrEqual(t, blindFrames[d.name], 5, "frames with hole %s blind", d.name)
	}
}

// lampSweep renders take at each local time in at twice, as styled and with
// the lamp named lamp at intensity 0, and returns, per time, how many pixels
// the lamp brightens by more than 8 levels in a channel and the mean x of
// those pixels, each weighted by how much the lamp brightens it.
func lampSweep(t *testing.T, take func() *Take, lamp string, length time.Duration, at ...time.Duration) ([]int, []float64) {
	t.Helper()
	counts := make([]int, len(at))
	xs := make([]float64, len(at))
	for j, a := range at {
		lit := renderTake(t, take(), length, a)
		dark := take()
		lights := maps.Clone(dark.Style.Lights)
		look := lights[lamp]
		look.Intensity = kinetograph.Constant(units.Scalar(0))
		lights[lamp] = look
		dark.Style.Lights = lights
		unlit := renderTake(t, dark, length, a)
		var sumX, sumW float64
		for p := 0; p < len(lit); p += 4 {
			gain := maxChannelDelta(lit[p:p+4], unlit[p:p+4])
			if gain <= 8 {
				continue
			}
			counts[j]++
			sumX += float64(gain * ((p / 4) % testWidth))
			sumW += float64(gain)
		}
		if counts[j] > 0 {
			xs[j] = sumX / sumW
		}
		t.Logf("at %s: %d pixels lit, mean x %.1f", a, counts[j], xs[j])
	}
	return counts, xs
}

// TestFilletLampSweeps checks the A3 light: it brightens part of the plate
// while it is up, and the lit pixels move left to right across the frame as
// it turns past the round nearest the camera. The plate's front face is lit
// for most of the turn and holds the centre of the lit pixels back, so they
// move about 60 px from 7.0 to 7.8 s.
func TestFilletLampSweeps(t *testing.T) {
	ctx := t.Context()
	ch, err := landingScript().Channels(0)
	require.NoError(t, err)
	take := func() *Take {
		tk, err := buildTake(ctx, ch)
		require.NoError(t, err)
		return tk
	}
	counts, xs := lampSweep(t, take, filletLamp, 13*time.Second, ms(6700), ms(7000), ms(7800))
	require.Zero(t, counts[0], "the lamp is dark before 6.8 s")
	require.Greater(t, counts[1], testWidth*testHeight/200)
	require.Greater(t, counts[2], testWidth*testHeight/200)
	require.Greater(t, xs[2]-xs[1], 40.0, "the lit pixels move right")
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
