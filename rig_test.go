package kinetograph_test

import (
	"errors"
	"fmt"
	"math"
	"slices"
	"sync"
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

// errNoPose is what tableTrack returns for a time its table does not hold.
var errNoPose = errors.New("no pose at this time")

// tableTrack is a TransformTrack that returns the transform its table holds
// for t exactly, fail[t] for a time in fail, and errNoPose for any other time.
// It records every time it is asked.
type tableTrack struct {
	poses map[time.Duration]r3.Transform
	fail  map[time.Duration]error

	mu    sync.Mutex
	asked []time.Duration
}

func (tr *tableTrack) At(t time.Duration) (r3.Transform, error) {
	tr.mu.Lock()
	tr.asked = append(tr.asked, t)
	tr.mu.Unlock()
	if err, ok := tr.fail[t]; ok {
		return r3.Transform{}, err
	}
	p, ok := tr.poses[t]
	if !ok {
		return r3.Transform{}, fmt.Errorf("%w: %s", errNoPose, t)
	}
	return p, nil
}

func (tr *tableTrack) times() []time.Duration {
	tr.mu.Lock()
	defer tr.mu.Unlock()
	return slices.Clone(tr.asked)
}

// constTrack is a TransformTrack that returns pose at every time.
type constTrack struct{ pose r3.Transform }

func (c constTrack) At(time.Duration) (r3.Transform, error) { return c.pose, nil }

// turnAndShift is a rotation by deg degrees about axis through center,
// followed by a translation by shift.
func turnAndShift(t *testing.T, center, axis r3.Vec, deg float64, shift r3.Vec) r3.Transform {
	t.Helper()
	turn, err := r3.RotationAround(center, axis, units.Degrees(deg))
	require.NoError(t, err)
	move, err := r3.Translation(shift)
	require.NoError(t, err)
	tr, err := turn.Then(move)
	require.NoError(t, err)
	return tr
}

func TestDrivenNodeReturnsTrackTransform(t *testing.T) {
	track := &tableTrack{poses: map[time.Duration]r3.Transform{
		0:                      turnAndShift(t, r3.Vec{}, r3.Vec{Z: 1}, 30, r3.Vec{X: 5}),
		250 * time.Millisecond: turnAndShift(t, r3.Vec{X: 2}, r3.Vec{X: 1, Y: 1}, 75, r3.Vec{Y: -3, Z: 8}),
		500 * time.Millisecond: turnAndShift(t, r3.Vec{Z: -4}, r3.Vec{Y: 1}, -140, r3.Vec{X: 1, Y: 2, Z: 3}),
	}}
	rig := kinetograph.NewRig()
	n, err := rig.Root().Driven(track)
	require.NoError(t, err)

	for _, at := range []time.Duration{0, 250 * time.Millisecond, 500 * time.Millisecond} {
		want := track.poses[at]
		local, err := n.Local(at)
		require.NoError(t, err)
		require.True(t, local.Equal(want, 0), "Local(%s)", at)
		world, err := n.World(at)
		require.NoError(t, err)
		require.True(t, world.Equal(want, 0), "World(%s)", at)
	}

	// A time between two table entries is asked of the track, not blended.
	_, err = n.World(125 * time.Millisecond)
	require.ErrorIs(t, err, errNoPose)
	require.Contains(t, track.times(), 125*time.Millisecond)
}

func TestDrivenNodeComposesWithOtherJoints(t *testing.T) {
	times := []time.Duration{0, 400 * time.Millisecond, time.Second}
	poses := map[time.Duration]r3.Transform{
		times[0]: turnAndShift(t, r3.Vec{}, r3.Vec{Z: 1}, 20, r3.Vec{X: 3}),
		times[1]: turnAndShift(t, r3.Vec{Y: 1}, r3.Vec{X: 1, Z: 2}, 110, r3.Vec{Z: -6}),
		times[2]: turnAndShift(t, r3.Vec{X: -2}, r3.Vec{Y: 1}, 250, r3.Vec{X: 4, Y: 4}),
	}
	turn := mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Degrees(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Degrees(90), Ease: kinetograph.SmoothStep},
	)
	slide := mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Millimeters(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Millimeters(30)},
	)
	offset, err := r3.Translation(r3.Vec{X: 7, Z: 1})
	require.NoError(t, err)

	rig := kinetograph.NewRig()
	driven, err := rig.Root().Driven(&tableTrack{poses: poses})
	require.NoError(t, err)
	fixedChild, err := driven.Fixed(offset)
	require.NoError(t, err)
	revoluteChild, err := driven.Revolute(r3.Vec{Y: 2}, r3.Vec{X: 1}, turn)
	require.NoError(t, err)
	prismatic, err := rig.Root().Prismatic(r3.Vec{Y: 1, Z: 1}, slide)
	require.NoError(t, err)
	drivenChild, err := prismatic.Driven(&tableTrack{poses: poses})
	require.NoError(t, err)

	for _, at := range times {
		// The expectations are rebuilt from r3, the table and the channels.
		pose := poses[at]
		wantFixed, err := offset.Then(pose)
		require.NoError(t, err)
		angle, err := turn.At(at)
		require.NoError(t, err)
		rot, err := r3.RotationAround(r3.Vec{Y: 2}, r3.Vec{X: 1}, angle)
		require.NoError(t, err)
		wantRevolute, err := rot.Then(pose)
		require.NoError(t, err)
		d, err := slide.At(at)
		require.NoError(t, err)
		mm, err := d.In(units.Millimeter)
		require.NoError(t, err)
		dir, ok := r3.Vec{Y: 1, Z: 1}.Normalize()
		require.True(t, ok)
		slid, err := r3.Translation(dir.Scale(mm))
		require.NoError(t, err)
		wantDriven, err := pose.Then(slid)
		require.NoError(t, err)

		got, err := fixedChild.World(at)
		require.NoError(t, err)
		require.True(t, got.Equal(wantFixed, 1e-12), "fixed child at %s", at)
		got, err = revoluteChild.World(at)
		require.NoError(t, err)
		require.True(t, got.Equal(wantRevolute, 1e-12), "revolute child at %s", at)
		got, err = drivenChild.World(at)
		require.NoError(t, err)
		require.True(t, got.Equal(wantDriven, 1e-12), "driven child of prismatic at %s", at)
	}
}

func TestDrivenNodeErrors(t *testing.T) {
	rig := kinetograph.NewRig()
	_, err := rig.Root().Driven(nil)
	require.ErrorIs(t, err, kinetograph.ErrNilTrack)

	mirrorPlane, err := r3.NewFrame(r3.Vec{}, r3.Vec{X: 1}, r3.Vec{Y: 1})
	require.NoError(t, err)
	mirror, err := r3.Reflection(mirrorPlane)
	require.NoError(t, err)

	at := 375 * time.Millisecond
	for _, tc := range []struct {
		name string
		pose r3.Transform
		want error
	}{
		{"zero transform", r3.Transform{}, kinetograph.ErrInvalidTransform},
		{"reflection", mirror, kinetograph.ErrReflection},
	} {
		t.Run(tc.name, func(t *testing.T) {
			n, err := rig.Root().Driven(constTrack{pose: tc.pose})
			require.NoError(t, err)
			_, err = n.Local(at)
			require.ErrorIs(t, err, tc.want)
			require.ErrorContains(t, err, "375ms")
			child, err := n.Fixed(r3.Identity())
			require.NoError(t, err)
			_, err = child.World(at)
			require.ErrorIs(t, err, tc.want)
		})
	}

	t.Run("track error", func(t *testing.T) {
		n, err := rig.Root().Driven(&tableTrack{})
		require.NoError(t, err)
		_, err = n.Local(at)
		require.ErrorIs(t, err, errNoPose)
		require.ErrorContains(t, err, "driven node at 375ms")
		_, err = n.World(at)
		require.ErrorIs(t, err, errNoPose)
	})
}
