package render_test

import (
	"fmt"
	"image"
	"math"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/solidlens"
	"github.com/lestrrat-3d/units"
	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

var black = solidlens.RGB(0, 0, 0)

// unlit is a red material with no ambient term: only lights make it visible.
var unlit = solidlens.Material{Color: red}

// alongY is a directional light travelling +Y: from the side camera, on -Y,
// toward the block's camera-facing face, whose normal is -Y.
var alongY = kinetograph.Light{Kind: kinetograph.DirectionalLight, Direction: r3.Vec{Y: 1}}

// litScene is a centred block on the root, the side camera, and light attached
// to node under the name "sun".
func litScene(t *testing.T, rig *kinetograph.Rig, node *kinetograph.Node, light kinetograph.Light) *kinetograph.Scene {
	t.Helper()
	scene := oneBlockScene(t, rig, rig.Root(), block(t, -5))
	require.NoError(t, scene.AddLight(lightSun, node, light))
	return scene
}

// litStyle draws parts with unlit and gives "sun" a white light at intensity.
func litStyle(intensity *kinetograph.Channel) render.Style {
	style := baseStyle()
	style.Default = render.Appearance{Material: unlit}
	style.Lights = map[string]render.LightAppearance{lightSun: {Color: white, Intensity: intensity}}
	return style
}

// centre returns the image-centre pixel as non-premultiplied bytes; every
// pixel the tests read is opaque, so they equal the stored bytes.
func centre(img *image.RGBA) [4]uint8 {
	c := img.RGBAAt(imgW/2, imgH/2)
	return [4]uint8{c.R, c.G, c.B, c.A}
}

func bytesOf(c solidlens.Color) [4]uint8 {
	n := c.NRGBA()
	return [4]uint8{n.R, n.G, n.B, n.A}
}

func TestNodeLightTurnsAway(t *testing.T) {
	rig := kinetograph.NewRig()
	turn, err := rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, ramp(t, units.Degrees(0), units.Degrees(180)))
	require.NoError(t, err)
	scene := litScene(t, rig, turn, alongY)
	r := newRenderer(t, newClip(t, scene, 1, 2*time.Second), litStyle(kinetograph.Constant(units.Scalar(0.5))))

	facing, err := r.Frame(t.Context(), 0) // 0°: the light hits the face head on
	require.NoError(t, err)
	require.Equal(t, bytesOf(solidlens.RGB(0.5, 0, 0)), centre(facing))

	away, err := r.Frame(t.Context(), 1) // 180°: the light travels -Y, away from the face
	require.NoError(t, err)
	require.Equal(t, bytesOf(black), centre(away))
}

func TestNodeLightIntensityRamp(t *testing.T) {
	rig := kinetograph.NewRig()
	turn, err := rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, kinetograph.Constant(units.Degrees(0)))
	require.NoError(t, err)
	scene := litScene(t, rig, turn, alongY)
	r := newRenderer(t, newClip(t, scene, 24, time.Second), litStyle(ramp(t, units.Scalar(0), units.Scalar(1))))

	img, err := r.Frame(t.Context(), 12) // t = 0.5 s, intensity 0.5
	require.NoError(t, err)
	require.Equal(t, bytesOf(solidlens.RGB(0.5, 0, 0)), centre(img))

	dark, err := r.Frame(t.Context(), 0) // intensity 0
	require.NoError(t, err)
	require.Equal(t, bytesOf(black), centre(dark))
}

func TestNodeLightMatchesStyleLight(t *testing.T) {
	const frame = 5
	for _, tc := range []struct {
		name  string
		light kinetograph.Light
		// fixed is the Style light at the node light's world pose.
		fixed func(style *render.Style, pose kinetograph.LightPose)
	}{
		{
			name:  "point",
			light: kinetograph.Light{Kind: kinetograph.PointLight, Position: r3.Vec{X: 10, Y: -40, Z: 25}},
			fixed: func(style *render.Style, pose kinetograph.LightPose) {
				style.PointLights = []solidlens.PointLight{{Position: pose.Position, Color: white, Intensity: 2500}}
			},
		},
		{
			name:  "directional",
			light: kinetograph.Light{Kind: kinetograph.DirectionalLight, Direction: r3.Vec{X: 1, Y: 2, Z: -1}},
			fixed: func(style *render.Style, pose kinetograph.LightPose) {
				style.DirectionalLights = []solidlens.DirectionalLight{{Direction: pose.Direction, Color: white, Intensity: 2500}}
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := kinetograph.NewRig()
			turn, err := rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, ramp(t, units.Degrees(0), units.Degrees(60)))
			require.NoError(t, err)
			clip := newClip(t, litScene(t, rig, turn, tc.light), 24, time.Second)
			nodeStyle := litStyle(kinetograph.Constant(units.Scalar(2500)))
			nodeStyle.Default.Material = solidlens.Matte(red)
			got, err := newRenderer(t, clip, nodeStyle).Frame(t.Context(), frame)
			require.NoError(t, err)

			f, err := clip.Frame(t.Context(), frame)
			require.NoError(t, err)
			plain := oneBlockScene(t, rig, rig.Root(), block(t, -5))
			fixedStyle := baseStyle()
			fixedStyle.Default.Material = solidlens.Matte(red)
			unlitImg, err := newRenderer(t, newClip(t, plain, 24, time.Second), fixedStyle).Frame(t.Context(), frame)
			require.NoError(t, err)
			tc.fixed(&fixedStyle, f.Lights[0])
			want, err := newRenderer(t, newClip(t, plain, 24, time.Second), fixedStyle).Frame(t.Context(), frame)
			require.NoError(t, err)

			require.Equal(t, encode(t, want), encode(t, got))
			require.NotEqual(t, encode(t, unlitImg), encode(t, got), "the light lights something")
		})
	}
}

func TestNodeLightAddsToStyleLight(t *testing.T) {
	rig := kinetograph.NewRig()
	scene := litScene(t, rig, rig.Root(), alongY)
	style := litStyle(kinetograph.Constant(units.Scalar(0.25)))
	style.DirectionalLights = []solidlens.DirectionalLight{{Direction: r3.Vec{Y: 1}, Color: white, Intensity: 0.25}}
	r := newRenderer(t, newClip(t, scene, 24, time.Second), style)

	img, err := r.Frame(t.Context(), 0)
	require.NoError(t, err)
	require.Equal(t, bytesOf(solidlens.RGB(0.5, 0, 0)), centre(img))
}

func TestNewRefusesBadLightStyles(t *testing.T) {
	rig := kinetograph.NewRig()
	clip := newClip(t, litScene(t, rig, rig.Root(), alongY), 24, time.Second)
	good := render.LightAppearance{Color: white, Intensity: kinetograph.Constant(units.Scalar(1))}

	for _, tc := range []struct {
		name   string
		lights map[string]render.LightAppearance
		want   error
		says   string
	}{
		{"scene light missing", nil, render.ErrStyle, `light "sun" has no Lights entry`},
		{"unknown name", map[string]render.LightAppearance{lightSun: good, "moon": good}, render.ErrStyle, `"moon"`},
		{"zero color", map[string]render.LightAppearance{lightSun: {Intensity: good.Intensity}}, render.ErrStyle, `"sun"`},
		{"nil intensity", map[string]render.LightAppearance{lightSun: {Color: white}}, kinetograph.ErrNilChannel, `"sun"`},
		{"length intensity", map[string]render.LightAppearance{
			lightSun: {Color: white, Intensity: kinetograph.Constant(units.Millimeters(1))},
		}, kinetograph.ErrKind, `"sun"`},
		{"negative keyframe", map[string]render.LightAppearance{
			lightSun: {Color: white, Intensity: ramp(t, units.Scalar(1), units.Scalar(-1))},
		}, render.ErrStyle, `"sun" Intensity keyframe 1`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			style := baseStyle()
			style.Lights = tc.lights
			r, err := render.New(t.Context(), clip, style)
			require.ErrorIs(t, err, tc.want)
			require.ErrorContains(t, err, tc.says)
			require.Nil(t, r)
		})
	}
}

func TestNewChecksLightsBeforeBuilding(t *testing.T) {
	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	b := &widthBuilder{}
	require.NoError(t, scene.AddParametric(partBlock, rig.Root(), b, map[string]*kinetograph.Channel{"width": steppedWidth(t)}))
	require.NoError(t, scene.SetCamera(rig.Root(), sideCamera()))
	require.NoError(t, scene.AddLight(lightSun, rig.Root(), alongY))

	_, err := render.New(t.Context(), newClip(t, scene, 6, time.Second), baseStyle())
	require.ErrorIs(t, err, render.ErrStyle)
	require.ErrorContains(t, err, `"sun"`)
	require.Equal(t, int32(0), b.calls.Load())
}

func TestSequenceReportsIntensityOverflow(t *testing.T) {
	// At frame 12 (u = 0.5) the intensity is 0 + MaxFloat64 · 2, which
	// overflows; every other frame evaluates.
	intensity := channel(t,
		kinetograph.Keyframe{At: 0, Value: units.Scalar(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Scalar(math.MaxFloat64), Ease: doubling{}},
	)
	rig := kinetograph.NewRig()
	clip := newClip(t, litScene(t, rig, rig.Root(), alongY), 24, time.Second)
	r := newRenderer(t, clip, litStyle(intensity))
	dir := t.TempDir()

	seq, err := r.Sequence(t.Context(), dir)
	require.Nil(t, seq)
	var fe *render.FrameError
	require.ErrorAs(t, err, &fe)
	require.Equal(t, 12, fe.Index)
	require.Equal(t, clip.FrameTime(12), fe.Time)
	require.ErrorIs(t, err, units.ErrNotFinite)
	require.ErrorContains(t, err, `light "sun" intensity`)

	want := make([]string, 12)
	for i := range want {
		want[i] = fmt.Sprintf("frame_%06d.png", i)
	}
	require.Equal(t, want, listDir(t, dir))
	_, err = os.Stat(filepath.Join(dir, "frame_000012.png"))
	require.ErrorIs(t, err, os.ErrNotExist)
}

// doubling eases to 2 at u = 0.5 and to u elsewhere.
type doubling struct{}

func (doubling) Ease(u float64) float64 {
	if u == 0.5 {
		return 2
	}
	return u
}
