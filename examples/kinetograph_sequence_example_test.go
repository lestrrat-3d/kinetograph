package examples_test

import (
	"context"
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

// A decad block turns 90 degrees about Z on one revolute joint while the
// camera orbits the origin on a second one. render tessellates the block once,
// poses its vertices for every frame time, and writes one PNG per frame.
func Example_kinetograph_sequence() {
	ctx := context.Background()

	// A 20 x 20 x 10 mm block, built with sketch and decad as any decad body is.
	w := sketch.NewWorld()
	s, err := w.CreateSketch(w.XY())
	if err != nil {
		fmt.Printf("failed to create sketch: %s\n", err)
		return
	}
	rect := s.CreateRectangle(10, -10, 30, 10)
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

	// The block's joint: Z through the origin, 0 to 90 degrees over one second.
	turn, err := kinetograph.NewChannel(
		kinetograph.Keyframe{At: 0, Value: units.Degrees(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Degrees(90), Ease: kinetograph.SmoothStep},
	)
	if err != nil {
		fmt.Printf("failed to build the turn channel: %s\n", err)
		return
	}
	rig := kinetograph.NewRig()
	blockJoint, err := rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, turn)
	if err != nil {
		fmt.Printf("failed to add the block joint: %s\n", err)
		return
	}

	// The camera's joint: Z through the origin, which is the camera's target,
	// so the camera circles what it looks at (docs/design.md D5).
	orbit, err := kinetograph.NewChannel(
		kinetograph.Keyframe{At: 0, Value: units.Degrees(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Degrees(45)},
	)
	if err != nil {
		fmt.Printf("failed to build the orbit channel: %s\n", err)
		return
	}
	cameraJoint, err := rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, orbit)
	if err != nil {
		fmt.Printf("failed to add the camera joint: %s\n", err)
		return
	}

	scene := kinetograph.NewScene(rig)
	if err := scene.AddPart("block", blockJoint, body); err != nil {
		fmt.Printf("failed to add the block: %s\n", err)
		return
	}
	err = scene.SetCamera(cameraJoint, kinetograph.Camera{
		Position: r3.Vec{X: 0, Y: -120, Z: 50},
		Target:   r3.Vec{},
		Up:       r3.Vec{Z: 1},
		FOV:      kinetograph.Constant(units.Degrees(40)),
	})
	if err != nil {
		fmt.Printf("failed to set the camera: %s\n", err)
		return
	}

	// 4 frames at 24 fps cover 1/6 s: ceil(1/6 s * 24) frames.
	clip, err := kinetograph.NewClip(scene, 24, 166*time.Millisecond)
	if err != nil {
		fmt.Printf("failed to build the clip: %s\n", err)
		return
	}

	style := render.Style{
		Width:      160,
		Height:     120,
		Chord:      units.Millimeters(0.5),
		Background: solidlens.RGB(0.82, 0.86, 0.93),
		Default: render.Appearance{
			Material: solidlens.Matte(solidlens.RGB(0.18, 0.47, 1)),
			Edges:    solidlens.Outline(solidlens.RGB(0, 0, 0)),
		},
		DirectionalLights: []solidlens.DirectionalLight{{
			Direction: solidlens.Vec{X: -0.5, Y: 0.4, Z: -1},
			Color:     solidlens.RGB(1, 1, 1),
			Intensity: 1.2,
		}},
	}
	renderer, err := render.New(ctx, clip, style)
	if err != nil {
		fmt.Printf("failed to build the renderer: %s\n", err)
		return
	}

	dir, err := os.MkdirTemp("", ".tmp-kinetograph-*")
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
	fmt.Printf("frames: %d at %d fps\n", seq.Frames, seq.FPS)
	fmt.Printf("pattern: %s\n", seq.Pattern)
	for _, f := range files {
		fmt.Printf("file: %s\n", filepath.Base(f))
	}
	// Output:
	// frames: 4 at 24 fps
	// pattern: frame_%06d.png
	// file: frame_000000.png
	// file: frame_000001.png
	// file: frame_000002.png
	// file: frame_000003.png
}
