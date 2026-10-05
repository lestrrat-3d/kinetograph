package examples_test

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"time"

	"github.com/lestrrat-3d/decad"
	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/sketch"
	"github.com/lestrrat-3d/solidlens"
	"github.com/lestrrat-3d/units"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

// errReplayEnded is what hopReplay returns at and past its end.
var errReplayEnded = errors.New("replay ended")

// hopReplay stands in for a recorded simulation: At computes the pose at t
// from t alone, and refuses any time at or past end. A block hops 40 mm along
// +X over end, rising 20 mm at the midpoint and tipping a quarter turn about Y.
type hopReplay struct{ end time.Duration }

func (h hopReplay) At(t time.Duration) (r3.Transform, error) {
	if t < 0 || t >= h.end {
		return r3.Transform{}, fmt.Errorf("%w: no pose at %s", errReplayEnded, t)
	}
	s := float64(t) / float64(h.end)
	tip, err := r3.Rotation(r3.Vec{Y: 1}, units.Degrees(90*s))
	if err != nil {
		return r3.Transform{}, err
	}
	move, err := r3.Translation(r3.Vec{X: 40*s - 20, Z: 80 * s * (1 - s)})
	if err != nil {
		return r3.Transform{}, err
	}
	return tip.Then(move)
}

// A block's pose comes from a caller's track rather than from channels.
// kinetograph asks the track at every frame time and draws exactly the
// transform it returns; a time the track refuses fails that frame.
func Example_kinetograph_driven() {
	ctx := context.Background()

	// A 10 x 10 x 10 mm block centred on the Z axis.
	w := sketch.NewWorld()
	s, err := w.CreateSketch(w.XY())
	if err != nil {
		fmt.Printf("failed to create sketch: %s\n", err)
		return
	}
	rect := s.CreateRectangle(-5, -5, 5, 5)
	s.Fix(rect.A)
	if _, err := s.Solve(ctx); err != nil {
		fmt.Printf("failed to solve: %s\n", err)
		return
	}
	body, err := decad.New().Extrude(s, s.Profiles()[0], decad.Distance{D: units.Millimeters(10), Dir: decad.Along})
	if err != nil {
		fmt.Printf("failed to extrude: %s\n", err)
		return
	}

	rig := kinetograph.NewRig()
	hop, err := rig.Root().Driven(hopReplay{end: time.Second})
	if err != nil {
		fmt.Printf("failed to add the driven node: %s\n", err)
		return
	}
	scene := kinetograph.NewScene(rig)
	if err := scene.AddPart("block", hop, body); err != nil {
		fmt.Printf("failed to add the block: %s\n", err)
		return
	}
	err = scene.SetCamera(rig.Root(), kinetograph.Camera{
		Position: r3.Vec{Y: -120, Z: 30},
		Target:   r3.Vec{Z: 10},
		Up:       r3.Vec{Z: 1},
		FOV:      kinetograph.Constant(units.Degrees(40)),
	})
	if err != nil {
		fmt.Printf("failed to set the camera: %s\n", err)
		return
	}

	// Four frames at 4 fps are at 0, 250, 500 and 750 ms: all before the
	// replay's end at 1 s.
	clip, err := kinetograph.NewClip(scene, 4, time.Second)
	if err != nil {
		fmt.Printf("failed to build the clip: %s\n", err)
		return
	}
	for i := range clip.FrameCount() {
		f, err := clip.Frame(ctx, i)
		if err != nil {
			fmt.Printf("failed to evaluate frame %d: %s\n", i, err)
			return
		}
		p := f.Poses[0].Transform.Translation()
		fmt.Printf("frame %d at %s: block at (%.1f, %.1f, %.1f)\n", i, f.Time, oneDecimal(p.X), oneDecimal(p.Y), oneDecimal(p.Z))
	}

	// The replay has no pose at 1 s, so the scene cannot be evaluated there.
	if _, err := scene.At(ctx, time.Second); errors.Is(err, errReplayEnded) {
		fmt.Println("at 1s: the replay has ended")
	}

	renderer, err := render.New(ctx, clip, render.Style{
		Width:      160,
		Height:     120,
		Chord:      units.Millimeters(0.5),
		Background: solidlens.RGB(0.82, 0.86, 0.93),
		Default: render.Appearance{
			Material: solidlens.Matte(solidlens.RGB(1, 0.68, 0.08)),
			Edges:    solidlens.Outline(solidlens.RGB(0, 0, 0)),
		},
		DirectionalLights: []solidlens.DirectionalLight{
			{Direction: r3.Vec{X: 1, Y: 2, Z: -3}, Color: solidlens.RGB(1, 1, 1), Intensity: 1},
		},
	})
	if err != nil {
		fmt.Printf("failed to build the renderer: %s\n", err)
		return
	}

	dir, err := os.MkdirTemp("", ".tmp-kinetograph-driven-*")
	if err != nil {
		fmt.Printf("failed to make a directory: %s\n", err)
		return
	}
	defer os.RemoveAll(dir)

	seq, err := renderer.Sequence(ctx, dir)
	if err != nil {
		fmt.Printf("failed to render the sequence: %s\n", err)
		return
	}
	files, err := filepath.Glob(filepath.Join(seq.Dir, "*.png"))
	if err != nil {
		fmt.Printf("failed to list the frames: %s\n", err)
		return
	}
	for _, f := range files {
		fmt.Printf("file: %s\n", filepath.Base(f))
	}
	// Output:
	// frame 0 at 0s: block at (-20.0, 0.0, 0.0)
	// frame 1 at 250ms: block at (-10.0, 0.0, 15.0)
	// frame 2 at 500ms: block at (0.0, 0.0, 20.0)
	// frame 3 at 750ms: block at (10.0, 0.0, 15.0)
	// at 1s: the replay has ended
	// file: frame_000000.png
	// file: frame_000001.png
	// file: frame_000002.png
	// file: frame_000003.png
}
