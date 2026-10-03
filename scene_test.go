package kinetograph_test

import (
	"context"
	"testing"
	"time"

	"github.com/lestrrat-3d/decad"
	"github.com/lestrrat-3d/decad/decadtest"
	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/units"
	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph"
)

// newBlock returns a 10 mm cube spanning (-5, -5, 0) to (5, 5, 10), built by
// decad's own fixture kit.
func newBlock(tb testing.TB) *decad.Body {
	tb.Helper()
	return decadtest.NewBlock(tb, decad.New(), -5, -5, 5, 5, units.Millimeters(10))
}

func defaultCamera() kinetograph.Camera {
	return kinetograph.Camera{
		Position: r3.Vec{Y: -100},
		Target:   r3.Vec{},
		Up:       r3.Vec{Z: 1},
		FOV:      kinetograph.Constant(units.Degrees(40)),
	}
}

func TestSceneAtPosesInAddPartOrder(t *testing.T) {
	rig := kinetograph.NewRig()
	turn := mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Degrees(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Degrees(90)},
	)
	slide := mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Millimeters(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Millimeters(20)},
	)
	spin, err := rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, turn)
	require.NoError(t, err)
	shift, err := rig.Root().Prismatic(r3.Vec{X: 1}, slide)
	require.NoError(t, err)

	a, b := newBlock(t), newBlock(t)
	scene := kinetograph.NewScene(rig)
	// Added shift-first so the order cannot be a tree order.
	require.NoError(t, scene.AddPart("slider", shift, b))
	require.NoError(t, scene.AddPart("spinner", spin, a))
	require.NoError(t, scene.SetCamera(rig.Root(), defaultCamera()))

	at := 400 * time.Millisecond
	f, err := scene.At(t.Context(), at)
	require.NoError(t, err)
	require.Equal(t, -1, f.Index)
	require.Equal(t, at, f.Time)
	require.Len(t, f.Poses, 2)
	require.Equal(t, "slider", f.Poses[0].Name)
	require.Same(t, b, f.Poses[0].Body)
	require.Equal(t, "spinner", f.Poses[1].Name)
	require.Same(t, a, f.Poses[1].Body)

	wantSlide, err := shift.World(at)
	require.NoError(t, err)
	require.True(t, f.Poses[0].Transform.Equal(wantSlide, 0))
	wantSpin, err := spin.World(at)
	require.NoError(t, err)
	require.True(t, f.Poses[1].Transform.Equal(wantSpin, 0))
	// 400 ms of a 20 mm slide over one second is 8 mm.
	require.True(t, f.Poses[0].Transform.Translation().Equal(r3.Vec{X: 8}, 1e-12))
}

func TestSceneBuildErrors(t *testing.T) {
	rig := kinetograph.NewRig()
	other := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)

	require.ErrorIs(t, scene.AddPart("a", rig.Root(), nil), kinetograph.ErrNilBody)
	require.ErrorIs(t, scene.AddPart("a", other.Root(), newBlock(t)), kinetograph.ErrForeignNode)
	require.ErrorIs(t, scene.AddPart("a", nil, newBlock(t)), kinetograph.ErrForeignNode)

	require.NoError(t, scene.AddPart("a", rig.Root(), newBlock(t)))
	require.ErrorIs(t, scene.AddPart("a", rig.Root(), newBlock(t)), kinetograph.ErrDuplicateName)

	require.ErrorIs(t, scene.SetCamera(other.Root(), defaultCamera()), kinetograph.ErrForeignNode)
	noFOV := defaultCamera()
	noFOV.FOV = nil
	require.ErrorIs(t, scene.SetCamera(rig.Root(), noFOV), kinetograph.ErrNilChannel)
	wrongKind := defaultCamera()
	wrongKind.FOV = kinetograph.Constant(units.Millimeters(40))
	require.ErrorIs(t, scene.SetCamera(rig.Root(), wrongKind), kinetograph.ErrKind)
}

func TestSceneAtRefusesIncompleteScenes(t *testing.T) {
	rig := kinetograph.NewRig()

	noCamera := kinetograph.NewScene(rig)
	require.NoError(t, noCamera.AddPart("a", rig.Root(), newBlock(t)))
	_, err := noCamera.At(t.Context(), 0)
	require.ErrorIs(t, err, kinetograph.ErrNoCamera)

	noParts := kinetograph.NewScene(rig)
	require.NoError(t, noParts.SetCamera(rig.Root(), defaultCamera()))
	_, err = noParts.At(t.Context(), 0)
	require.ErrorIs(t, err, kinetograph.ErrEmptyScene)
}

func TestSceneSetCameraReplaces(t *testing.T) {
	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	require.NoError(t, scene.AddPart("a", rig.Root(), newBlock(t)))
	require.NoError(t, scene.SetCamera(rig.Root(), defaultCamera()))
	second := defaultCamera()
	second.Position = r3.Vec{X: 80}
	require.NoError(t, scene.SetCamera(rig.Root(), second))
	f, err := scene.At(t.Context(), 0)
	require.NoError(t, err)
	require.Equal(t, r3.Vec{X: 80}, f.Camera.Position)
}

func TestSceneAtContext(t *testing.T) {
	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	require.NoError(t, scene.AddPart("a", rig.Root(), newBlock(t)))
	require.NoError(t, scene.SetCamera(rig.Root(), defaultCamera()))

	//nolint:staticcheck // a nil context is the case under test
	_, err := scene.At(nil, 0)
	require.ErrorIs(t, err, kinetograph.ErrNilContext)

	cancelled, stop := context.WithCancel(t.Context())
	stop()
	_, err = scene.At(cancelled, 0)
	require.Equal(t, cancelled.Err(), err)
}
