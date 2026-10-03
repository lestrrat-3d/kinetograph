package kinetograph_test

import (
	"testing"
	"time"

	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/units"
	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph"
)

func readyScene(t *testing.T) (*kinetograph.Scene, *kinetograph.Node) {
	t.Helper()
	rig := kinetograph.NewRig()
	turn := mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Degrees(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Degrees(90)},
	)
	n, err := rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, turn)
	require.NoError(t, err)
	scene := kinetograph.NewScene(rig)
	require.NoError(t, scene.AddPart("block", n, newBlock(t)))
	require.NoError(t, scene.SetCamera(rig.Root(), defaultCamera()))
	return scene, n
}

func TestClipFrameCounts(t *testing.T) {
	scene, _ := readyScene(t)
	for _, tc := range []struct {
		name string
		fps  int
		d    time.Duration
		want int
	}{
		{"1s at 24", 24, time.Second, 24},
		{"1s at 30", 30, time.Second, 30},
		{"100ms at 24", 24, 100 * time.Millisecond, 3},
		{"exact multiple", 25, 200 * time.Millisecond, 5},
		{"one nanosecond", 1, time.Nanosecond, 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			c, err := kinetograph.NewClip(scene, tc.fps, tc.d)
			require.NoError(t, err)
			require.Equal(t, tc.want, c.FrameCount())
			require.Equal(t, tc.fps, c.FPS())
			require.Equal(t, tc.d, c.Duration())
			require.Same(t, scene, c.Scene())
		})
	}
}

func TestClipFrameTimeIsExactNanoseconds(t *testing.T) {
	scene, _ := readyScene(t)
	c, err := kinetograph.NewClip(scene, 24, time.Second)
	require.NoError(t, err)
	require.Equal(t, time.Duration(958333333), c.FrameTime(23))
	require.Equal(t, time.Duration(0), c.FrameTime(0))
	require.Equal(t, time.Duration(41666666), c.FrameTime(1))

	// Every frame time is below the duration, and the next one is not.
	for i := range c.FrameCount() {
		require.Less(t, c.FrameTime(i), c.Duration())
	}
	require.GreaterOrEqual(t, c.FrameTime(c.FrameCount()), c.Duration())

	// A long clip: i * 1e9 passes 2^63 near i = 9.2e9, so a naive product wraps.
	long, err := kinetograph.NewClip(scene, 1_000_000, 100*time.Hour)
	require.NoError(t, err)
	i := 20_000_000_000
	require.Equal(t, time.Duration(20_000_000_000_000), long.FrameTime(i), "FrameTime(%d) at 1 MHz", i)
}

func TestNewClipErrors(t *testing.T) {
	scene, _ := readyScene(t)
	_, err := kinetograph.NewClip(scene, 0, time.Second)
	require.ErrorIs(t, err, kinetograph.ErrInvalidClip)
	_, err = kinetograph.NewClip(scene, 24, 0)
	require.ErrorIs(t, err, kinetograph.ErrInvalidClip)
	_, err = kinetograph.NewClip(scene, 24, -time.Second)
	require.ErrorIs(t, err, kinetograph.ErrInvalidClip)
	_, err = kinetograph.NewClip(scene, int(^uint(0)>>1), time.Duration(^uint64(0)>>1))
	require.ErrorIs(t, err, kinetograph.ErrInvalidClip)

	rig := kinetograph.NewRig()
	empty := kinetograph.NewScene(rig)
	_, err = kinetograph.NewClip(empty, 24, time.Second)
	require.ErrorIs(t, err, kinetograph.ErrNoCamera)
	require.NoError(t, empty.SetCamera(rig.Root(), defaultCamera()))
	_, err = kinetograph.NewClip(empty, 24, time.Second)
	require.ErrorIs(t, err, kinetograph.ErrEmptyScene)
}

func TestClipFrame(t *testing.T) {
	scene, node := readyScene(t)
	c, err := kinetograph.NewClip(scene, 24, time.Second)
	require.NoError(t, err)

	f, err := c.Frame(t.Context(), 12)
	require.NoError(t, err)
	require.Equal(t, 12, f.Index)
	require.Equal(t, 500*time.Millisecond, f.Time)
	want, err := node.World(500 * time.Millisecond)
	require.NoError(t, err)
	require.True(t, f.Poses[0].Transform.Equal(want, 0))

	for _, bad := range []int{-1, 24, 1000} {
		_, err = c.Frame(t.Context(), bad)
		require.ErrorIs(t, err, kinetograph.ErrFrameRange, "frame %d", bad)
	}
	//nolint:staticcheck // a nil context is the case under test
	_, err = c.Frame(nil, 0)
	require.ErrorIs(t, err, kinetograph.ErrNilContext)
}
