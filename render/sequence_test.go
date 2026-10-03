package render_test

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/units"
	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

func contextCancelled(t *testing.T) (context.Context, context.CancelFunc) {
	t.Helper()
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	return ctx, cancel
}

func translation(t *testing.T, v r3.Vec) r3.Transform {
	t.Helper()
	tr, err := r3.Translation(v)
	require.NoError(t, err)
	return tr
}

// spinRenderer renders a block turning about Z over one second.
func spinRenderer(t *testing.T, fps int) *render.Renderer {
	t.Helper()
	rig := kinetograph.NewRig()
	turn, err := rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, ramp(t, units.Degrees(0), units.Degrees(90)))
	require.NoError(t, err)
	scene := oneBlockScene(t, rig, turn, block(t, 5))
	return newRenderer(t, newClip(t, scene, fps, time.Second), baseStyle())
}

// failingRenderer's camera field of view passes solidlens's 179 degree limit
// at frame 4 of a 24 fps clip only: frames 0 to 3 and 5 onward render and frame
// 4 does not. A camera cannot fail the "up parallel to the view direction" way
// at one frame, because camera and node move rigidly together.
func failingRenderer(t *testing.T) *render.Renderer {
	t.Helper()
	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	require.NoError(t, scene.AddPart("block", rig.Root(), block(t, -5)))
	cam := sideCamera()
	frame3 := time.Duration(3) * time.Second / 24
	frame4 := time.Duration(4) * time.Second / 24
	cam.FOV = channel(t,
		kinetograph.Keyframe{At: 0, Value: units.Degrees(40)},
		kinetograph.Keyframe{At: frame3, Value: units.Degrees(40)},
		kinetograph.Keyframe{At: frame4, Value: units.Degrees(179.5)},
		kinetograph.Keyframe{At: frame4 + time.Millisecond, Value: units.Degrees(40)},
	)
	require.NoError(t, scene.SetCamera(rig.Root(), cam))
	return newRenderer(t, newClip(t, scene, 24, time.Second), baseStyle())
}

func listDir(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	require.NoError(t, err)
	names := make([]string, len(entries))
	for i, e := range entries {
		names[i] = e.Name()
	}
	return names
}

func TestSequenceWritesEveryFrame(t *testing.T) {
	for _, workers := range []int{1, 3} {
		t.Run(fmt.Sprintf("%d workers", workers), func(t *testing.T) {
			r := spinRenderer(t, 6)
			dir := filepath.Join(t.TempDir(), "out", "frames") // Sequence creates it
			seq, err := r.Sequence(t.Context(), dir, render.WithWorkers(workers))
			require.NoError(t, err)

			require.Equal(t, &render.Sequence{Dir: dir, Pattern: "frame_%06d.png", Frames: 6, FPS: 6}, seq)
			want := make([]string, 6)
			for i := range 6 {
				want[i] = fmt.Sprintf(seq.Pattern, i)
			}
			require.Equal(t, want, listDir(t, dir), "exactly the six frame files and nothing else")

			for i := range 6 {
				img, err := r.Frame(t.Context(), i)
				require.NoError(t, err)
				got, err := os.ReadFile(filepath.Join(dir, want[i]))
				require.NoError(t, err)
				require.Equal(t, encode(t, img), got, "frame %d", i)
			}
		})
	}
}

func TestSequenceWithPrefix(t *testing.T) {
	r := spinRenderer(t, 3)
	dir := t.TempDir()
	seq, err := r.Sequence(t.Context(), dir, render.WithPrefix("100%_shot_"))
	require.NoError(t, err)
	require.Equal(t, "100%%_shot_%06d.png", seq.Pattern)
	require.Equal(t, []string{"100%_shot_000000.png", "100%_shot_000001.png", "100%_shot_000002.png"}, listDir(t, dir))
}

func TestSequenceReportsLowestFailingFrame(t *testing.T) {
	for _, workers := range []int{1, 3} {
		t.Run(fmt.Sprintf("%d workers", workers), func(t *testing.T) {
			r := failingRenderer(t)
			dir := t.TempDir()
			seq, err := r.Sequence(t.Context(), dir, render.WithWorkers(workers))
			require.Nil(t, seq)

			var fe *render.FrameError
			require.ErrorAs(t, err, &fe)
			require.Equal(t, 4, fe.Index)
			require.Equal(t, time.Duration(4)*time.Second/24, fe.Time)

			names := listDir(t, dir)
			for i := range 4 {
				require.Contains(t, names, fmt.Sprintf("frame_%06d.png", i))
			}
			require.NotContains(t, names, "frame_000004.png")
			// Nothing after the failure is claimed: only frames already in
			// flight on the other workers can still finish.
			require.LessOrEqual(t, len(names), 4+workers-1, "files: %v", names)
			for _, n := range names {
				require.False(t, strings.HasSuffix(n, ".tmp"), "leftover temporary file %s", n)
			}
		})
	}
}

// cancelAfterFile is a context that reports context.Canceled from the moment
// the named file exists. Sequence checks ctx.Err() before it starts each frame,
// so with one worker the run stops at a fixed frame whatever the machine speed.
type cancelAfterFile struct {
	context.Context //nolint:containedctx // a test context wrapper that overrides Err
	path            string
}

func (c cancelAfterFile) Err() error {
	if _, err := os.Stat(c.path); err == nil {
		return context.Canceled
	}
	return c.Context.Err()
}

func TestSequenceCancellation(t *testing.T) {
	r := spinRenderer(t, 24)
	dir := t.TempDir()
	ctx := cancelAfterFile{Context: t.Context(), path: filepath.Join(dir, "frame_000002.png")}

	seq, err := r.Sequence(ctx, dir)
	require.Nil(t, seq)
	require.Equal(t, context.Canceled, err, "ctx.Err() comes back unwrapped")
	require.Equal(t, []string{"frame_000000.png", "frame_000001.png", "frame_000002.png"}, listDir(t, dir))
}

func TestSequenceNilContext(t *testing.T) {
	r := spinRenderer(t, 2)
	//nolint:staticcheck // a nil context is the case under test
	_, err := r.Sequence(nil, t.TempDir())
	require.ErrorIs(t, err, kinetograph.ErrNilContext)
}
