package kinetograph_test

import (
	"context"
	"errors"
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
	err := scene.AddPart("a", rig.Root(), newBlock(t))
	require.ErrorIs(t, err, kinetograph.ErrDuplicateName)
	require.EqualError(t, err, `kinetograph: name already used: part "a"`)

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

// errTimelineStopped is the sentinel a failing test track returns.
var errTimelineStopped = errors.New("timeline stopped")

// drivenClipTrack holds a distinct pose for each frame time of a 6-frame clip
// at 24 fps, and fails with errTimelineStopped at fail.
func drivenClipTrack(t *testing.T, fail ...int) *tableTrack {
	t.Helper()
	track := &tableTrack{poses: map[time.Duration]r3.Transform{}, fail: map[time.Duration]error{}}
	for i := range 6 {
		at := time.Duration(i) * time.Second / 24
		track.poses[at] = turnAndShift(t, r3.Vec{X: 1}, r3.Vec{X: 1, Y: 2, Z: 3}, 17*float64(i), r3.Vec{X: float64(i), Z: -2 * float64(i)})
	}
	for _, i := range fail {
		track.fail[time.Duration(i)*time.Second/24] = errTimelineStopped
	}
	return track
}

func TestDrivenPartPosesAtFrameTimes(t *testing.T) {
	track := drivenClipTrack(t)
	rig := kinetograph.NewRig()
	n, err := rig.Root().Driven(track)
	require.NoError(t, err)
	scene := kinetograph.NewScene(rig)
	require.NoError(t, scene.AddPart("body", n, newBlock(t)))
	require.NoError(t, scene.SetCamera(rig.Root(), defaultCamera()))
	clip, err := kinetograph.NewClip(scene, 24, 250*time.Millisecond)
	require.NoError(t, err)
	require.Equal(t, 6, clip.FrameCount())

	frameTimes := map[time.Duration]struct{}{}
	for i := range clip.FrameCount() {
		frameTimes[clip.FrameTime(i)] = struct{}{}
		f, err := clip.Frame(t.Context(), i)
		require.NoError(t, err)
		require.True(t, f.Poses[0].Transform.Equal(track.poses[clip.FrameTime(i)], 0), "frame %d", i)
	}
	asked := track.times()
	require.NotEmpty(t, asked)
	for _, at := range asked {
		require.Contains(t, frameTimes, at, "the track was asked for %s, which is no frame time", at)
	}
}

func TestDrivenNodeFailureFailsFrame(t *testing.T) {
	// FrameTime(3) of a 24 fps clip is 125 ms.
	for _, tc := range []struct {
		name   string
		attach func(scene *kinetograph.Scene, root, driven *kinetograph.Node) error
		want   string
	}{
		{
			name: "part",
			attach: func(scene *kinetograph.Scene, root, driven *kinetograph.Node) error {
				if err := scene.AddPart("body", driven, newBlock(t)); err != nil {
					return err
				}
				return scene.SetCamera(root, defaultCamera())
			},
			want: `kinetograph: part "body" at 125ms`,
		},
		{
			name: "camera",
			attach: func(scene *kinetograph.Scene, root, driven *kinetograph.Node) error {
				if err := scene.AddPart("body", root, newBlock(t)); err != nil {
					return err
				}
				return scene.SetCamera(driven, defaultCamera())
			},
			want: "kinetograph: camera at 125ms",
		},
		{
			name: "light",
			attach: func(scene *kinetograph.Scene, root, driven *kinetograph.Node) error {
				if err := scene.AddPart("body", root, newBlock(t)); err != nil {
					return err
				}
				if err := scene.SetCamera(root, defaultCamera()); err != nil {
					return err
				}
				return scene.AddLight("lamp", driven, kinetograph.Light{Kind: kinetograph.PointLight, Position: r3.Vec{Z: 40}})
			},
			want: `kinetograph: light "lamp" at 125ms`,
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := kinetograph.NewRig()
			n, err := rig.Root().Driven(drivenClipTrack(t, 3))
			require.NoError(t, err)
			scene := kinetograph.NewScene(rig)
			require.NoError(t, tc.attach(scene, rig.Root(), n))
			clip, err := kinetograph.NewClip(scene, 24, 250*time.Millisecond)
			require.NoError(t, err)

			_, err = clip.Frame(t.Context(), 2)
			require.NoError(t, err)
			_, err = clip.Frame(t.Context(), 3)
			require.ErrorIs(t, err, errTimelineStopped)
			require.ErrorContains(t, err, tc.want)
			_, err = clip.Frame(t.Context(), 4)
			require.NoError(t, err)
		})
	}
}

// cancellingTrack cancels its context and then fails.
type cancellingTrack struct{ cancel context.CancelFunc }

func (c cancellingTrack) At(time.Duration) (r3.Transform, error) {
	c.cancel()
	return r3.Transform{}, errTimelineStopped
}

func TestDrivenNodeFailureUnderCancelledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	rig := kinetograph.NewRig()
	n, err := rig.Root().Driven(cancellingTrack{cancel: cancel})
	require.NoError(t, err)
	scene := kinetograph.NewScene(rig)
	require.NoError(t, scene.AddPart("body", n, newBlock(t)))
	require.NoError(t, scene.SetCamera(rig.Root(), defaultCamera()))

	_, err = scene.AtCached(ctx, 0, kinetograph.NewBuildCache())
	require.Equal(t, context.Canceled, err, "ctx.Err() comes back unwrapped")
}
