package kinetograph_test

import (
	"math"
	"testing"
	"time"

	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/units"
	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph"
)

// lightScene is a scene with one block on the root and the default camera.
func lightScene(t *testing.T, rig *kinetograph.Rig) *kinetograph.Scene {
	t.Helper()
	scene := kinetograph.NewScene(rig)
	require.NoError(t, scene.AddPart("block", rig.Root(), newBlock(t)))
	require.NoError(t, scene.SetCamera(rig.Root(), defaultCamera()))
	return scene
}

func TestPointLightFollowsRevoluteNode(t *testing.T) {
	rig := kinetograph.NewRig()
	turn, err := rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, kinetograph.Constant(units.Degrees(90)))
	require.NoError(t, err)
	scene := lightScene(t, rig)
	require.NoError(t, scene.AddLight("lamp", turn, kinetograph.Light{Kind: kinetograph.PointLight, Position: r3.Vec{X: 10}}))

	f, err := scene.At(t.Context(), 0)
	require.NoError(t, err)
	require.Len(t, f.Lights, 1)
	got := f.Lights[0]
	require.Equal(t, "lamp", got.Name)
	require.Equal(t, kinetograph.PointLight, got.Kind)
	require.True(t, got.Position.Equal(r3.Vec{Y: 10}, 1e-12), "position %v", got.Position)
	require.Equal(t, r3.Vec{}, got.Direction)
}

func TestDirectionalLightIgnoresSlide(t *testing.T) {
	rig := kinetograph.NewRig()
	slide, err := rig.Root().Prismatic(r3.Vec{X: 1, Y: 2}, mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Millimeters(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Millimeters(37)},
	))
	require.NoError(t, err)
	scene := lightScene(t, rig)
	require.NoError(t, scene.AddLight("sun", slide, kinetograph.Light{Kind: kinetograph.DirectionalLight, Direction: r3.Vec{Z: 3}}))
	clip, err := kinetograph.NewClip(scene, 6, time.Second)
	require.NoError(t, err)

	for i := range clip.FrameCount() {
		f, err := clip.Frame(t.Context(), i)
		require.NoError(t, err)
		require.Equal(t, r3.Vec{Z: 1}, f.Lights[0].Direction, "frame %d", i)
		require.Equal(t, r3.Vec{}, f.Lights[0].Position, "frame %d", i)
	}
}

func TestDirectionalLightTurnsWithRevoluteNode(t *testing.T) {
	rig := kinetograph.NewRig()
	turn, err := rig.Root().Revolute(r3.Vec{X: 4, Y: -3}, r3.Vec{Z: 1}, kinetograph.Constant(units.Degrees(90)))
	require.NoError(t, err)
	scene := lightScene(t, rig)
	require.NoError(t, scene.AddLight("sun", turn, kinetograph.Light{Kind: kinetograph.DirectionalLight, Direction: r3.Vec{X: 1}}))

	f, err := scene.At(t.Context(), 0)
	require.NoError(t, err)
	require.True(t, f.Lights[0].Direction.Equal(r3.Vec{Y: 1}, 1e-12), "direction %v", f.Lights[0].Direction)
}

func TestSceneLightsInAddLightOrder(t *testing.T) {
	rig := kinetograph.NewRig()
	scene := lightScene(t, rig)
	require.Empty(t, scene.Lights())
	require.NoError(t, scene.AddLight("sun", rig.Root(), kinetograph.Light{Kind: kinetograph.DirectionalLight, Direction: r3.Vec{Z: -1}}))
	require.NoError(t, scene.AddLight("lamp", rig.Root(), kinetograph.Light{Kind: kinetograph.PointLight, Position: r3.Vec{Z: 50}}))
	require.Equal(t, []kinetograph.LightInfo{
		{Name: "sun", Kind: kinetograph.DirectionalLight},
		{Name: "lamp", Kind: kinetograph.PointLight},
	}, scene.Lights())
}

func TestFrameLightsLeavePosesAlone(t *testing.T) {
	rig := kinetograph.NewRig()
	turn, err := rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Degrees(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Degrees(90)},
	))
	require.NoError(t, err)
	body := newBlock(t)
	build := func(lights bool) *kinetograph.Scene {
		scene := kinetograph.NewScene(rig)
		require.NoError(t, scene.AddPart("block", turn, body))
		require.NoError(t, scene.SetCamera(rig.Root(), defaultCamera()))
		if lights {
			require.NoError(t, scene.AddLight("key", turn, kinetograph.Light{Kind: kinetograph.PointLight, Position: r3.Vec{X: 30}}))
			// A light may share a part's name.
			require.NoError(t, scene.AddLight("block", rig.Root(), kinetograph.Light{Kind: kinetograph.DirectionalLight, Direction: r3.Vec{Y: 1}}))
		}
		return scene
	}

	at := 300 * time.Millisecond
	with, err := build(true).At(t.Context(), at)
	require.NoError(t, err)
	without, err := build(false).At(t.Context(), at)
	require.NoError(t, err)

	require.Equal(t, []string{"key", "block"}, []string{with.Lights[0].Name, with.Lights[1].Name})
	require.Equal(t, without.Poses, with.Poses)
	require.Equal(t, without.Camera, with.Camera)
	require.Empty(t, without.Lights)
}

func TestAddLightErrors(t *testing.T) {
	rig := kinetograph.NewRig()
	other := kinetograph.NewRig()
	scene := lightScene(t, rig)
	point := kinetograph.Light{Kind: kinetograph.PointLight, Position: r3.Vec{Z: 10}}

	for _, tc := range []struct {
		name  string
		node  *kinetograph.Node
		light kinetograph.Light
		want  error
	}{
		{"kind 0", rig.Root(), kinetograph.Light{Position: r3.Vec{Z: 10}}, kinetograph.ErrInvalidLight},
		{"point light with a direction", rig.Root(),
			kinetograph.Light{Kind: kinetograph.PointLight, Direction: r3.Vec{Z: -1}}, kinetograph.ErrInvalidLight},
		{"directional light with a position", rig.Root(),
			kinetograph.Light{Kind: kinetograph.DirectionalLight, Position: r3.Vec{Z: 1}, Direction: r3.Vec{Z: -1}},
			kinetograph.ErrInvalidLight},
		{"non-finite point position", rig.Root(),
			kinetograph.Light{Kind: kinetograph.PointLight, Position: r3.Vec{X: math.Inf(1)}}, kinetograph.ErrInvalidLight},
		{"zero direction", rig.Root(), kinetograph.Light{Kind: kinetograph.DirectionalLight}, kinetograph.ErrDegenerateDirection},
		{"non-finite direction", rig.Root(),
			kinetograph.Light{Kind: kinetograph.DirectionalLight, Direction: r3.Vec{Z: math.NaN()}},
			kinetograph.ErrDegenerateDirection},
		{"foreign node", other.Root(), point, kinetograph.ErrForeignNode},
		{"nil node", nil, point, kinetograph.ErrForeignNode},
	} {
		t.Run(tc.name, func(t *testing.T) {
			require.ErrorIs(t, scene.AddLight("bad", tc.node, tc.light), tc.want)
		})
	}
	require.Empty(t, scene.Lights(), "a refused light is not attached")

	require.NoError(t, scene.AddLight("lamp", rig.Root(), point))
	err := scene.AddLight("lamp", rig.Root(), point)
	require.ErrorIs(t, err, kinetograph.ErrDuplicateName)
	require.EqualError(t, err, `kinetograph: name already used: light "lamp"`)
	require.Len(t, scene.Lights(), 1)
}

// doubling eases to 2 at u = 0.5 and to u elsewhere.
type doubling struct{}

func (doubling) Ease(u float64) float64 {
	if u == 0.5 {
		return 2
	}
	return u
}

func TestSceneAtNamesLightThatCannotBePosed(t *testing.T) {
	rig := kinetograph.NewRig()
	// At 500 ms the angle is 0 + MaxFloat64 · 2 degrees, which overflows.
	turn, err := rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Degrees(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Degrees(math.MaxFloat64), Ease: doubling{}},
	))
	require.NoError(t, err)
	scene := lightScene(t, rig)
	require.NoError(t, scene.AddLight("lamp", turn, kinetograph.Light{Kind: kinetograph.PointLight, Position: r3.Vec{X: 10}}))

	_, err = scene.At(t.Context(), 500*time.Millisecond)
	require.ErrorIs(t, err, units.ErrNotFinite)
	require.ErrorContains(t, err, `light "lamp" at 500ms`)

	_, err = scene.At(t.Context(), 250*time.Millisecond)
	require.NoError(t, err)
}
