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

func TestRevoluteQuarterTurn(t *testing.T) {
	rig := kinetograph.NewRig()
	n, err := rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, kinetograph.Constant(units.Degrees(90)))
	require.NoError(t, err)
	w, err := n.World(0)
	require.NoError(t, err)
	got := w.Apply(r3.Vec{X: 1})
	require.True(t, got.Equal(r3.Vec{Y: 1}, 1e-12), "got %v", got)
}

func TestRevoluteAboutOffCenterAxis(t *testing.T) {
	rig := kinetograph.NewRig()
	// Z through (10, 0, 0): the point (11, 0, 0) swings to (10, 1, 0).
	n, err := rig.Root().Revolute(r3.Vec{X: 10}, r3.Vec{Z: 5}, kinetograph.Constant(units.Degrees(90)))
	require.NoError(t, err)
	w, err := n.World(0)
	require.NoError(t, err)
	got := w.Apply(r3.Vec{X: 11})
	require.True(t, got.Equal(r3.Vec{X: 10, Y: 1}, 1e-12), "got %v", got)
}

func TestPrismaticSlide(t *testing.T) {
	rig := kinetograph.NewRig()
	n, err := rig.Root().Prismatic(r3.Vec{Z: 2}, kinetograph.Constant(units.Millimeters(5)))
	require.NoError(t, err)
	local, err := n.Local(0)
	require.NoError(t, err)
	require.Equal(t, r3.Vec{Z: 5}, local.Translation())
}

func TestPrismaticConvertsLengthUnits(t *testing.T) {
	rig := kinetograph.NewRig()
	n, err := rig.Root().Prismatic(r3.Vec{X: 1}, kinetograph.Constant(units.Meters(0.5)))
	require.NoError(t, err)
	local, err := n.Local(0)
	require.NoError(t, err)
	require.True(t, local.Translation().Equal(r3.Vec{X: 500}, 1e-9), "got %v", local.Translation())
}

func TestWorldComposesChildThenParent(t *testing.T) {
	rig := kinetograph.NewRig()
	slide := mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Millimeters(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Millimeters(40)},
	)
	turn := mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Degrees(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Degrees(120), Ease: kinetograph.SmoothStep},
	)
	parent, err := rig.Root().Prismatic(r3.Vec{X: 1, Y: 1}, slide)
	require.NoError(t, err)
	child, err := parent.Revolute(r3.Vec{Y: 3}, r3.Vec{X: 1, Z: 1}, turn)
	require.NoError(t, err)

	for _, at := range []time.Duration{0, 300 * time.Millisecond, 700 * time.Millisecond, time.Second} {
		// The expectation is rebuilt from r3 and the channel values alone.
		d, err := slide.At(at)
		require.NoError(t, err)
		mm, err := d.In(units.Millimeter)
		require.NoError(t, err)
		dir, ok := r3.Vec{X: 1, Y: 1}.Normalize()
		require.True(t, ok)
		parentWorld, err := r3.Translation(dir.Scale(mm))
		require.NoError(t, err)
		angle, err := turn.At(at)
		require.NoError(t, err)
		childLocal, err := r3.RotationAround(r3.Vec{Y: 3}, r3.Vec{X: 1, Z: 1}, angle)
		require.NoError(t, err)
		want, err := childLocal.Then(parentWorld)
		require.NoError(t, err)

		got, err := child.World(at)
		require.NoError(t, err)
		require.True(t, got.Equal(want, 1e-12), "World(%s)", at)
	}
}

func TestRootWorldIsIdentity(t *testing.T) {
	rig := kinetograph.NewRig()
	w, err := rig.Root().World(time.Hour)
	require.NoError(t, err)
	require.True(t, w.Equal(r3.Identity(), 0))
}

func TestFixedAppliesConstantTransform(t *testing.T) {
	rig := kinetograph.NewRig()
	tr, err := r3.Translation(r3.Vec{X: 1, Y: 2, Z: 3})
	require.NoError(t, err)
	n, err := rig.Root().Fixed(tr)
	require.NoError(t, err)
	w, err := n.World(5 * time.Second)
	require.NoError(t, err)
	require.True(t, w.Equal(tr, 0))
}

func TestFixedErrors(t *testing.T) {
	rig := kinetograph.NewRig()
	mirrorPlane, err := r3.NewFrame(r3.Vec{}, r3.Vec{X: 1}, r3.Vec{Y: 1})
	require.NoError(t, err)
	mirror, err := r3.Reflection(mirrorPlane)
	require.NoError(t, err)

	_, err = rig.Root().Fixed(mirror)
	require.ErrorIs(t, err, kinetograph.ErrReflection)

	_, err = rig.Root().Fixed(r3.Transform{})
	require.ErrorIs(t, err, kinetograph.ErrInvalidTransform)
}

func TestJointConstructorErrors(t *testing.T) {
	rig := kinetograph.NewRig()
	angle := kinetograph.Constant(units.Degrees(1))
	length := kinetograph.Constant(units.Millimeters(1))

	_, err := rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, length)
	require.ErrorIs(t, err, kinetograph.ErrKind)
	_, err = rig.Root().Prismatic(r3.Vec{X: 1}, angle)
	require.ErrorIs(t, err, kinetograph.ErrKind)

	_, err = rig.Root().Revolute(r3.Vec{}, r3.Vec{}, angle)
	require.ErrorIs(t, err, r3.ErrDegenerateAxis)
	_, err = rig.Root().Revolute(r3.Vec{}, r3.Vec{X: math.NaN()}, angle)
	require.ErrorIs(t, err, r3.ErrDegenerateAxis)
	_, err = rig.Root().Prismatic(r3.Vec{}, length)
	require.ErrorIs(t, err, kinetograph.ErrDegenerateDirection)
	_, err = rig.Root().Prismatic(r3.Vec{Y: math.Inf(1)}, length)
	require.ErrorIs(t, err, kinetograph.ErrDegenerateDirection)

	_, err = rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, nil)
	require.ErrorIs(t, err, kinetograph.ErrNilChannel)
	_, err = rig.Root().Prismatic(r3.Vec{X: 1}, nil)
	require.ErrorIs(t, err, kinetograph.ErrNilChannel)
}

func TestWorldSurfacesChannelOverflow(t *testing.T) {
	rig := kinetograph.NewRig()
	huge := mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Millimeters(-1.7e308)},
		kinetograph.Keyframe{At: time.Second, Value: units.Millimeters(1.7e308)},
	)
	n, err := rig.Root().Prismatic(r3.Vec{X: 1}, huge)
	require.NoError(t, err)
	_, err = n.World(500 * time.Millisecond)
	require.ErrorIs(t, err, units.ErrNotFinite)
}
