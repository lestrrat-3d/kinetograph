package render_test

import (
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
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

func fade(f float64) *kinetograph.Channel { return kinetograph.Constant(units.Scalar(f)) }

// fadeSteps is a Dimensionless channel that is values[i] at frame i of a 4 fps
// clip.
func fadeSteps(t *testing.T, values ...float64) *kinetograph.Channel {
	t.Helper()
	keys := make([]kinetograph.Keyframe, len(values))
	for i, v := range values {
		keys[i] = kinetograph.Keyframe{At: time.Duration(i) * time.Second / 4, Value: units.Scalar(v)}
	}
	return channel(t, keys...)
}

// mixed is round(b + f · (a − b)) for every byte of a pixel where a and b
// differ, and b elsewhere: the one-fading-part formula, written out from the
// design rather than taken from the renderer.
func mixed(a, b *image.RGBA, f float64) []uint8 {
	out := make([]uint8, len(b.Pix))
	for p := 0; p < len(b.Pix); p += 4 {
		same := a.Pix[p] == b.Pix[p] && a.Pix[p+1] == b.Pix[p+1] && a.Pix[p+2] == b.Pix[p+2] && a.Pix[p+3] == b.Pix[p+3]
		for c := p; c < p+4; c++ {
			if same {
				out[c] = b.Pix[c]
				continue
			}
			x := float64(b.Pix[c]) + f*(float64(a.Pix[c])-float64(b.Pix[c]))
			out[c] = uint8(math.Floor(x + 0.5))
		}
	}
	return out
}

// spinningEdgedClip is a block turning about Z, lit and outlined, so its
// pixels hold several shades and antialiased edge lines.
func spinningEdgedClip(t *testing.T) (*kinetograph.Clip, render.Style) {
	t.Helper()
	rig := kinetograph.NewRig()
	turn, err := rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, ramp(t, units.Degrees(0), units.Degrees(90)))
	require.NoError(t, err)
	scene := oneBlockScene(t, rig, turn, block(t, -5))
	style := baseStyle()
	style.Default = render.Appearance{
		Material: solidlens.Matte(red),
		Edges:    solidlens.Outline(black),
	}
	style.DirectionalLights = []solidlens.DirectionalLight{{Direction: r3.Vec{X: -0.4, Y: 1, Z: -0.7}, Color: white, Intensity: 1}}
	return newClip(t, scene, 6, time.Second), style
}

// withFade returns style with f as the default fade.
func withFade(style render.Style, f *kinetograph.Channel) render.Style {
	style.Default.Fade = f
	return style
}

func TestFadeZeroHidesThePart(t *testing.T) {
	clip, style := spinningEdgedClip(t)
	img, err := newRenderer(t, clip, withFade(style, fade(0))).Frame(t.Context(), 2)
	require.NoError(t, err)
	bg := white.NRGBA()
	for p := 0; p < len(img.Pix); p += 4 {
		require.Equal(t, []uint8{bg.R, bg.G, bg.B, bg.A}, img.Pix[p:p+4], "pixel %d", p/4)
	}
}

func TestFadeOneDrawsAsNoFade(t *testing.T) {
	clip, style := spinningEdgedClip(t)
	plain, err := newRenderer(t, clip, style).Frame(t.Context(), 2)
	require.NoError(t, err)
	opaque, err := newRenderer(t, clip, withFade(style, fade(1))).Frame(t.Context(), 2)
	require.NoError(t, err)
	require.Equal(t, encode(t, plain), encode(t, opaque))
}

func TestFadeHalfMixesOverBackground(t *testing.T) {
	clip, style := spinningEdgedClip(t)
	a, err := newRenderer(t, clip, style).Frame(t.Context(), 3)
	require.NoError(t, err)
	b, err := newRenderer(t, clip, withFade(style, fade(0))).Frame(t.Context(), 3)
	require.NoError(t, err)
	got, err := newRenderer(t, clip, withFade(style, fade(0.5))).Frame(t.Context(), 3)
	require.NoError(t, err)
	require.Equal(t, mixed(a, b, 0.5), got.Pix)
	require.NotEqual(t, a.Pix, got.Pix)
}

// twoBlocks is a 10 mm red block "front" centred on the camera axis and a
// larger blue block "back" behind it, both flat-shaded, seen by the side
// camera with no lights. add attaches them in the order given.
func twoBlocks(t *testing.T, order []string, frontFade, backFade *kinetograph.Channel) (*kinetograph.Clip, render.Style) {
	t.Helper()
	rig := kinetograph.NewRig()
	bodies := map[string]*decad.Body{
		partFront: block(t, -5),
		partBack:  decadtest.NewBlock(t, decad.New(), -15, 20, 15, 40, units.Millimeters(30)),
	}
	scene := newScene(t, rig,
		map[string]*kinetograph.Node{partFront: rig.Root(), partBack: rig.Root()}, order,
		bodies, rig.Root(), sideCamera())
	style := baseStyle()
	style.Parts = map[string]render.Appearance{
		partFront: {Material: flat(red), Fade: frontFade},
		partBack:  {Material: flat(blue), Fade: backFade},
	}
	return newClip(t, scene, 4, time.Second), style
}

func TestFadeShowsOpaquePartBehind(t *testing.T) {
	clip, style := twoBlocks(t, []string{partFront, partBack}, fade(0.5), nil)
	img, err := newRenderer(t, clip, style).Frame(t.Context(), 0)
	require.NoError(t, err)
	require.Equal(t, [4]uint8{128, 0, 128, 255}, centre(img), "the blue part shows through")
	// Beside the front block, only the blue block is drawn.
	c := img.RGBAAt(imgW/2+8, imgH/2)
	require.Equal(t, [4]uint8{0, 0, 255, 255}, [4]uint8{c.R, c.G, c.B, c.A})
}

func TestFadeSideBySide(t *testing.T) {
	rig := kinetograph.NewRig()
	left, err := rig.Root().Fixed(translation(t, r3.Vec{X: -15}))
	require.NoError(t, err)
	right, err := rig.Root().Fixed(translation(t, r3.Vec{X: 15}))
	require.NoError(t, err)
	scene := newScene(t, rig,
		map[string]*kinetograph.Node{partLeft: left, partRight: right}, []string{partLeft, partRight},
		map[string]*decad.Body{partLeft: block(t, -5), partRight: block(t, -5)}, rig.Root(), sideCamera())
	clip := newClip(t, scene, 24, time.Second)

	style := baseStyle()
	style.Parts = map[string]render.Appearance{
		partLeft:  {Material: flat(red)},
		partRight: {Material: flat(blue)},
	}
	opaque, err := newRenderer(t, clip, style).Frame(t.Context(), 0)
	require.NoError(t, err)
	style.Parts = map[string]render.Appearance{
		partLeft:  {Material: flat(red), Fade: fade(0.25)},
		partRight: {Material: flat(blue), Fade: fade(0.75)},
	}
	got, err := newRenderer(t, clip, style).Frame(t.Context(), 0)
	require.NoError(t, err)

	// round(b + f · (a − b)) per channel, with b the white background.
	want := map[[4]uint8][4]uint8{
		{255, 0, 0, 255}: {255, 191, 191, 255}, // red at 0.25: 255 − 0.25 · 255 = 191.25
		{0, 0, 255, 255}: {64, 64, 255, 255},   // blue at 0.75: 255 − 0.75 · 255 = 63.75
	}
	seen := map[[4]uint8]int{}
	for p := 0; p < len(opaque.Pix); p += 4 {
		var a, g [4]uint8
		copy(a[:], opaque.Pix[p:p+4])
		copy(g[:], got.Pix[p:p+4])
		w, interior := want[a]
		if !interior {
			continue
		}
		seen[a]++
		require.Equal(t, w, g, "pixel %d", p/4)
	}
	require.Greater(t, seen[[4]uint8{255, 0, 0, 255}], 40)
	require.Greater(t, seen[[4]uint8{0, 0, 255, 255}], 40)
}

// within1 requires every byte of a and b to differ by at most 1.
func within1(t *testing.T, a, b [4]uint8, msg string) {
	t.Helper()
	for c := range a {
		require.LessOrEqual(t, math.Abs(float64(a[c])-float64(b[c])), 1.0, "%s: %v against %v", msg, a, b)
	}
}

func TestFadeIsContinuousInFartherPart(t *testing.T) {
	clip, style := twoBlocks(t, []string{partFront, partBack}, fade(0.5), fadeSteps(t, 0, 0.001, 0.999, 1))
	r := newRenderer(t, clip, style)
	var px [4][4]uint8
	for i := range px {
		img, err := r.Frame(t.Context(), i)
		require.NoError(t, err)
		px[i] = centre(img)
	}
	within1(t, px[0], px[1], "blue at 0 and 0.001")
	within1(t, px[3], px[2], "blue at 1 and 0.999")
	require.Equal(t, [4]uint8{255, 128, 128, 255}, px[0], "red at 0.5 over white")
	require.Equal(t, [4]uint8{128, 0, 128, 255}, px[3], "red at 0.5 over blue")
}

func TestFadeIsContinuousInNearerPart(t *testing.T) {
	clip, style := twoBlocks(t, []string{partFront, partBack}, fadeSteps(t, 0, 0.001, 0.999, 1), fade(0.5))
	r := newRenderer(t, clip, style)
	var px [4][4]uint8
	for i := range px {
		img, err := r.Frame(t.Context(), i)
		require.NoError(t, err)
		px[i] = centre(img)
	}
	within1(t, px[0], px[1], "red at 0 and 0.001")
	within1(t, px[3], px[2], "red at 1 and 0.999")
	require.Equal(t, [4]uint8{128, 128, 255, 255}, px[0], "blue at 0.5 over white")
	require.Equal(t, [4]uint8{255, 0, 0, 255}, px[3], "opaque red in front")
}

func TestFadeOrderDoesNotDependOnAddOrder(t *testing.T) {
	nearFirst, style := twoBlocks(t, []string{partFront, partBack}, fade(0.4), fade(0.7))
	farFirst, _ := twoBlocks(t, []string{partBack, partFront}, fade(0.4), fade(0.7))
	a, err := newRenderer(t, nearFirst, style).Frame(t.Context(), 0)
	require.NoError(t, err)
	b, err := newRenderer(t, farFirst, style).Frame(t.Context(), 0)
	require.NoError(t, err)
	require.Equal(t, encode(t, a), encode(t, b))
}

// overshoot eases to 1.5 at u = 0.5 and to u elsewhere.
type overshoot struct{}

func (overshoot) Ease(u float64) float64 {
	if u == 0.5 {
		return 1.5
	}
	return u
}

func TestFadeIsClampedAfterEvaluation(t *testing.T) {
	clip, style := spinningEdgedClip(t)
	clip = newClip(t, clip.Scene(), 24, time.Second)
	f := channel(t,
		kinetograph.Keyframe{At: 0, Value: units.Scalar(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Scalar(1), Ease: overshoot{}},
	)
	got, err := newRenderer(t, clip, withFade(style, f)).Frame(t.Context(), 12) // u = 0.5: 1.5, clamped to 1
	require.NoError(t, err)
	want, err := newRenderer(t, clip, style).Frame(t.Context(), 12)
	require.NoError(t, err)
	require.Equal(t, encode(t, want), encode(t, got))
}

func TestNewRefusesBadFades(t *testing.T) {
	rig := kinetograph.NewRig()
	clip := newClip(t, oneBlockScene(t, rig, rig.Root(), block(t, -5)), 24, time.Second)

	angle := baseStyle()
	angle.Default.Fade = kinetograph.Constant(units.Degrees(1))
	defaultHigh := baseStyle()
	defaultHigh.Default.Fade = ramp(t, units.Scalar(0), units.Scalar(1.5))
	// Every part has a Parts entry, and Default.Fade is still checked.
	defaultHigh.Parts = map[string]render.Appearance{partBlock: {Material: flat(red)}}
	partLow := baseStyle()
	partLow.Parts = map[string]render.Appearance{partBlock: {Material: flat(red), Fade: ramp(t, units.Scalar(-0.1), units.Scalar(1))}}

	for _, tc := range []struct {
		name  string
		style render.Style
		want  error
		says  string
	}{
		{"angle fade", angle, kinetograph.ErrKind, "Default Fade"},
		{"default above 1", defaultHigh, render.ErrStyle, "Default Fade keyframe 1"},
		{"part below 0", partLow, render.ErrStyle, `part "block" Fade keyframe 0`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := render.New(t.Context(), clip, tc.style)
			require.ErrorIs(t, err, tc.want)
			require.ErrorContains(t, err, tc.says)
			require.Nil(t, r)
		})
	}
}

func TestFadeIsDeterministic(t *testing.T) {
	clip, style := twoBlocks(t, []string{partFront, partBack}, fade(0.5), fadeSteps(t, 0.2, 0.4, 0.6, 0.8))
	clip = newClip(t, clip.Scene(), 6, time.Second)
	r := newRenderer(t, clip, style)

	a, err := r.Frame(t.Context(), 3)
	require.NoError(t, err)
	b, err := r.Frame(t.Context(), 3)
	require.NoError(t, err)
	require.Equal(t, encode(t, a), encode(t, b))

	files := map[int][][]byte{}
	for _, workers := range []int{1, 3} {
		dir := t.TempDir()
		seq, err := r.Sequence(t.Context(), dir, render.WithWorkers(workers))
		require.NoError(t, err)
		require.Equal(t, 6, seq.Frames)
		for i := range seq.Frames {
			got, err := os.ReadFile(filepath.Join(dir, fmt.Sprintf(seq.Pattern, i)))
			require.NoError(t, err)
			files[workers] = append(files[workers], got)
		}
	}
	require.Equal(t, files[1], files[3])
	require.Equal(t, encode(t, a), files[1][3], "Sequence writes the image Frame returns")
}

func TestFadeOnParametricPart(t *testing.T) {
	clip := parametricClip(t, &widthBuilder{}, steppedWidth(t))
	style := baseStyle()
	style.Default.Edges = solidlens.Outline(black)
	a, err := newRenderer(t, clip, style).Frame(t.Context(), 4)
	require.NoError(t, err)
	b, err := newRenderer(t, clip, withFade(style, fade(0))).Frame(t.Context(), 4)
	require.NoError(t, err)
	got, err := newRenderer(t, clip, withFade(style, fade(0.5))).Frame(t.Context(), 4)
	require.NoError(t, err)
	require.Equal(t, mixed(a, b, 0.5), got.Pix)
}

func TestHiddenParametricPartStillBuilds(t *testing.T) {
	// Fade is 1 up to frame 2 and 0 from frame 3, where the width first
	// passes 15 mm and Build fails.
	b := &widthBuilder{failAbove: units.Millimeters(15)}
	clip := parametricClip(t, b, steppedWidth(t))
	style := withFade(baseStyle(), channel(t,
		kinetograph.Keyframe{At: 0, Value: units.Scalar(1)},
		kinetograph.Keyframe{At: 2 * time.Second / 6, Value: units.Scalar(1)},
		kinetograph.Keyframe{At: 3 * time.Second / 6, Value: units.Scalar(0)},
	))
	_, err := newRenderer(t, clip, style).Frame(t.Context(), 3)
	var fe *render.FrameError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, 3, fe.Index)
	require.Equal(t, clip.FrameTime(3), fe.Time)
	require.ErrorIs(t, err, errBuild)
}
