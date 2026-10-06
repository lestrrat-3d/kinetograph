package render_test

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/lestrrat-3d/decad"
	"github.com/lestrrat-3d/decad/decadtest"
	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/units"
	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

// linkageRenderer renders a two-link arm for 6 frames at 24 fps, seen from
// above: the upper arm, x ∈ [0, 20], turns 0° to 90° about Z through the
// origin, and the forearm, x ∈ [20, 40], turns 0° to −90° about Z through
// (20, 0, 0), from time 0 to frame 5's time.
func linkageRenderer(t *testing.T) *render.Renderer {
	t.Helper()
	doc := decad.New()
	upper := decadtest.NewBlock(t, doc, 0, -4, 20, 4, units.Millimeters(5))
	forearm := decadtest.NewBlock(t, doc, 20, -4, 40, 4, units.Millimeters(5))
	linkage := decad.NewLinkage()
	shoulder, err := linkage.Ground().Revolute(r3.Vec{}, r3.Vec{Z: 1}, []*decad.Body{upper})
	require.NoError(t, err)
	elbow, err := shoulder.Revolute(r3.Vec{X: 20}, r3.Vec{Z: 1}, []*decad.Body{forearm})
	require.NoError(t, err)
	drive := decad.Drive{
		{Link: shoulder, From: units.Degrees(0), To: units.Degrees(90)},
		{Link: elbow, From: units.Degrees(0), To: units.Degrees(-90)},
	}

	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	fraction := channel(t,
		kinetograph.Keyframe{At: 0, Value: units.Scalar(0)},
		kinetograph.Keyframe{At: 5 * time.Second / 24, Value: units.Scalar(1)})
	_, err = scene.AddLinkage(linkage, drive, fraction,
		map[*decad.Body]string{upper: partLeft, forearm: partRight})
	require.NoError(t, err)
	require.NoError(t, scene.SetCamera(rig.Root(), kinetograph.Camera{
		Position: r3.Vec{X: 20, Y: 10, Z: 120},
		Target:   r3.Vec{X: 20, Y: 10},
		Up:       r3.Vec{Y: 1},
		FOV:      kinetograph.Constant(units.Degrees(40)),
	}))
	return newRenderer(t, newClip(t, scene, 24, 250*time.Millisecond), baseStyle())
}

func TestLinkageSequenceIsDeterministic(t *testing.T) {
	r := linkageRenderer(t)
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
	// The arm folds over the clip, so its last frame differs from its first.
	require.NotEqual(t, runs[0][0], runs[0][5])
}
