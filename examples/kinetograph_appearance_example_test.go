package examples_test

import (
	"context"
	"fmt"
	"math"
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

// oneDecimal rounds x to one decimal place, with no negative zero.
func oneDecimal(x float64) float64 {
	r := math.Round(x*10) / 10
	if r == 0 {
		return 0
	}
	return r
}

// A block fades in over one second while a point light circles it on a
// revolute joint. The root package poses the light; render gives it its color
// and intensity, and mixes the fading block over the background.
func Example_kinetograph_appearance() {
	ctx := context.Background()

	// A 20 x 20 x 10 mm block centred on the Z axis.
	w := sketch.NewWorld()
	s, err := w.CreateSketch(w.XY())
	if err != nil {
		fmt.Printf("failed to create sketch: %s\n", err)
		return
	}
	rect := s.CreateRectangle(-10, -10, 10, 10)
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

	// The light's joint: one full turn about Z over one second.
	circle, err := kinetograph.NewChannel(
		kinetograph.Keyframe{At: 0, Value: units.Degrees(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Degrees(360)},
	)
	if err != nil {
		fmt.Printf("failed to build the circle channel: %s\n", err)
		return
	}
	rig := kinetograph.NewRig()
	orbit, err := rig.Root().Revolute(r3.Vec{}, r3.Vec{Z: 1}, circle)
	if err != nil {
		fmt.Printf("failed to add the light joint: %s\n", err)
		return
	}

	scene := kinetograph.NewScene(rig)
	if err := scene.AddPart("block", rig.Root(), body); err != nil {
		fmt.Printf("failed to add the block: %s\n", err)
		return
	}
	// The light starts 40 mm out on +X and 20 mm up, in the orbit node's frame.
	if err := scene.AddLight("lamp", orbit, kinetograph.Light{Kind: kinetograph.PointLight, Position: r3.Vec{X: 40, Z: 20}}); err != nil {
		fmt.Printf("failed to add the light: %s\n", err)
		return
	}
	err = scene.SetCamera(rig.Root(), kinetograph.Camera{
		Position: r3.Vec{X: 50, Y: -90, Z: 70},
		Target:   r3.Vec{Z: 5},
		Up:       r3.Vec{Z: 1},
		FOV:      kinetograph.Constant(units.Degrees(40)),
	})
	if err != nil {
		fmt.Printf("failed to set the camera: %s\n", err)
		return
	}

	clip, err := kinetograph.NewClip(scene, 4, time.Second)
	if err != nil {
		fmt.Printf("failed to build the clip: %s\n", err)
		return
	}

	// The block's opacity rises from 0 to 1 over the clip's one second.
	fadeIn, err := kinetograph.NewChannel(
		kinetograph.Keyframe{At: 0, Value: units.Scalar(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Scalar(1)},
	)
	if err != nil {
		fmt.Printf("failed to build the fade channel: %s\n", err)
		return
	}
	style := render.Style{
		Width:      160,
		Height:     120,
		Chord:      units.Millimeters(0.5),
		Background: solidlens.RGB(0.82, 0.86, 0.93),
		Default: render.Appearance{
			Material: solidlens.Matte(solidlens.RGB(1, 0.68, 0.08)),
			Edges:    solidlens.Outline(solidlens.RGB(0, 0, 0)),
			Fade:     fadeIn,
		},
		// A point light falls off with the square of the distance in
		// millimetres, so a light about 45 mm away needs an intensity in the
		// thousands.
		Lights: map[string]render.LightAppearance{
			"lamp": {Color: solidlens.RGB(1, 1, 1), Intensity: kinetograph.Constant(units.Scalar(3000))},
		},
	}
	renderer, err := render.New(ctx, clip, style)
	if err != nil {
		fmt.Printf("failed to build the renderer: %s\n", err)
		return
	}

	for i := range clip.FrameCount() {
		f, err := clip.Frame(ctx, i)
		if err != nil {
			fmt.Printf("failed to evaluate frame %d: %s\n", i, err)
			return
		}
		p := f.Lights[0].Position
		fmt.Printf("frame %d: %s at (%.1f, %.1f, %.1f)\n", i, f.Lights[0].Name, oneDecimal(p.X), oneDecimal(p.Y), oneDecimal(p.Z))
	}

	dir, err := os.MkdirTemp("", ".tmp-kinetograph-appearance-*")
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
	fmt.Printf("pattern: %s\n", seq.Pattern)
	// Output:
	// frame 0: lamp at (40.0, 0.0, 20.0)
	// frame 1: lamp at (0.0, 40.0, 20.0)
	// frame 2: lamp at (-40.0, 0.0, 20.0)
	// frame 3: lamp at (0.0, -40.0, 20.0)
	// file: frame_000000.png
	// file: frame_000001.png
	// file: frame_000002.png
	// file: frame_000003.png
	// pattern: frame_%06d.png
}
