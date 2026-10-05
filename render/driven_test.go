package render_test

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/units"
	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

// errTimelineStopped is what frameTrack returns from its stop time on.
var errTimelineStopped = errors.New("timeline stopped")

// frameTrack is a TransformTrack over the frame times of a clip at fps: it
// returns poses[i] at FrameTime(i), errTimelineStopped at every frame time
// from index stop on, and an error naming the time for any other time. A stop
// of 0 or less never stops.
type frameTrack struct {
	fps   int
	poses []r3.Transform
	stop  int
}

func (tr frameTrack) At(t time.Duration) (r3.Transform, error) {
	for i, p := range tr.poses {
		if time.Duration(i)*time.Second/time.Duration(tr.fps) != t {
			continue
		}
		if tr.stop > 0 && i >= tr.stop {
			return r3.Transform{}, errTimelineStopped
		}
		return p, nil
	}
	return r3.Transform{}, fmt.Errorf("frameTrack: no pose at %s", t)
}

// hopTrack returns a frameTrack whose six poses turn the block about Z and
// lift it, a distinct pose per frame.
func hopTrack(t *testing.T, stop int) frameTrack {
	t.Helper()
	poses := make([]r3.Transform, 6)
	for i := range poses {
		turn, err := r3.RotationAround(r3.Vec{}, r3.Vec{Z: 1}, units.Degrees(13*float64(i)))
		require.NoError(t, err)
		lift, err := r3.Translation(r3.Vec{X: float64(i) - 2, Z: float64(i)})
		require.NoError(t, err)
		poses[i], err = turn.Then(lift)
		require.NoError(t, err)
	}
	return frameTrack{fps: 24, poses: poses, stop: stop}
}

// drivenRenderer renders a block on a driven node for 6 frames at 24 fps.
func drivenRenderer(t *testing.T, track kinetograph.TransformTrack) *render.Renderer {
	t.Helper()
	rig := kinetograph.NewRig()
	n, err := rig.Root().Driven(track)
	require.NoError(t, err)
	scene := oneBlockScene(t, rig, n, block(t, -5))
	return newRenderer(t, newClip(t, scene, 24, 250*time.Millisecond), baseStyle())
}

func TestSequenceStopsWhereDrivenTrackStops(t *testing.T) {
	for _, workers := range []int{1, 3} {
		t.Run(fmt.Sprintf("%d workers", workers), func(t *testing.T) {
			r := drivenRenderer(t, hopTrack(t, 3))
			dir := t.TempDir()
			seq, err := r.Sequence(t.Context(), dir, render.WithWorkers(workers))
			require.Nil(t, seq)

			var fe *render.FrameError
			require.ErrorAs(t, err, &fe)
			require.Equal(t, 3, fe.Index)
			require.Equal(t, time.Duration(3)*time.Second/24, fe.Time)
			require.ErrorIs(t, err, errTimelineStopped)
			// Every frame from 3 on fails, so no worker can write one.
			require.Equal(t, []string{"frame_000000.png", "frame_000001.png", "frame_000002.png"}, listDir(t, dir))
		})
	}
}

func TestDrivenFrameMatchesFixed(t *testing.T) {
	pose := hopTrack(t, 0).poses[4]
	driven := drivenRenderer(t, frameTrack{fps: 24, poses: []r3.Transform{pose}})

	rig := kinetograph.NewRig()
	fixed, err := rig.Root().Fixed(pose)
	require.NoError(t, err)
	scene := oneBlockScene(t, rig, fixed, block(t, -5))
	reference := newRenderer(t, newClip(t, scene, 24, 250*time.Millisecond), baseStyle())

	got, err := driven.Frame(t.Context(), 0)
	require.NoError(t, err)
	want, err := reference.Frame(t.Context(), 0)
	require.NoError(t, err)
	require.Equal(t, encode(t, want), encode(t, got))
}

func TestFrameFollowsDrivenTranslation(t *testing.T) {
	poses := make([]r3.Transform, 6)
	for i := range poses {
		poses[i] = translation(t, r3.Vec{X: 2.4 * float64(i)}) // 12 mm along +X by frame 5
	}
	r := drivenRenderer(t, frameTrack{fps: 24, poses: poses})

	first, err := r.Frame(t.Context(), 0)
	require.NoError(t, err)
	last, err := r.Frame(t.Context(), 5)
	require.NoError(t, err)
	x0, y0, _ := centroid(first, white)
	x5, y5, _ := centroid(last, white)
	require.GreaterOrEqual(t, x5-x0, 2.0, "centroid x moved %.2f px", x5-x0)
	require.InDelta(t, y0, y5, 1)
}

func TestDrivenSequenceIsDeterministic(t *testing.T) {
	r := drivenRenderer(t, hopTrack(t, 0))
	runs := make([][][]byte, 0, 2)
	for _, workers := range []int{1, 3} {
		dir := t.TempDir()
		seq, err := r.Sequence(t.Context(), dir, render.WithWorkers(workers))
		require.NoError(t, err)
		require.Equal(t, 6, seq.Frames)
		files := make([][]byte, seq.Frames)
		for i := range files {
			files[i], err = os.ReadFile(filepath.Join(dir, fmt.Sprintf(seq.Pattern, i)))
			require.NoError(t, err)
		}
		runs = append(runs, files)
	}
	require.Equal(t, runs[0], runs[1])
	// The six poses differ, so the six frames must too.
	for i := 1; i < 6; i++ {
		require.NotEqual(t, runs[0][0], runs[0][i], "frame %d", i)
	}
}
