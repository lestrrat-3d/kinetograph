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

// slab builds a block params["width"] wide along X, 20 mm deep and 10 mm tall,
// centred on the Z axis. It prints each width it is asked to build.
type slab struct{}

func (slab) Build(ctx context.Context, params kinetograph.Params) (*decad.Body, error) {
	width := params["width"]
	fmt.Printf("build: width %s\n", width)
	w, err := width.In(units.Millimeter)
	if err != nil {
		return nil, err
	}

	// A new sketch world and decad document per call, so rebuilt bodies do not
	// accumulate in one document.
	world := sketch.NewWorld()
	s, err := world.CreateSketch(world.XY())
	if err != nil {
		return nil, err
	}
	rect := s.CreateRectangle(-w/2, -10, w/2, 10)
	s.Fix(rect.A)
	if _, err := s.Solve(ctx); err != nil {
		return nil, err
	}
	return decad.New().Extrude(s, s.Profiles()[0], decad.Distance{D: units.Millimeters(10), Dir: decad.Along})
}

// A slab whose width follows a Length channel. render asks the Builder for a
// body once per distinct width: frames 0 and 1 share 10 mm, so four frames
// take three builds.
func Example_kinetograph_reshape() {
	ctx := context.Background()

	// 10 mm held to 250 ms, then rising linearly to 30 mm at 750 ms. At 4 fps
	// the frames fall at 0, 250, 500 and 750 ms: 10, 10, 20 and 30 mm.
	width, err := kinetograph.NewChannel(
		kinetograph.Keyframe{At: 0, Value: units.Millimeters(10)},
		kinetograph.Keyframe{At: 250 * time.Millisecond, Value: units.Millimeters(10)},
		kinetograph.Keyframe{At: 750 * time.Millisecond, Value: units.Millimeters(30)},
	)
	if err != nil {
		fmt.Printf("failed to build the width channel: %s\n", err)
		return
	}

	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	err = scene.AddParametric("slab", rig.Root(), slab{}, map[string]*kinetograph.Channel{"width": width})
	if err != nil {
		fmt.Printf("failed to add the slab: %s\n", err)
		return
	}
	err = scene.SetCamera(rig.Root(), kinetograph.Camera{
		Position: r3.Vec{X: 40, Y: -90, Z: 60},
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

	style := render.Style{
		Width:      160,
		Height:     120,
		Chord:      units.Millimeters(0.5),
		Background: solidlens.RGB(0.82, 0.86, 0.93),
		Default: render.Appearance{
			Material: solidlens.Matte(solidlens.RGB(1, 0.68, 0.08)),
			Edges:    solidlens.Outline(solidlens.RGB(0, 0, 0)),
		},
		DirectionalLights: []solidlens.DirectionalLight{{
			Direction: solidlens.Vec{X: -0.5, Y: 0.4, Z: -1},
			Color:     solidlens.RGB(1, 1, 1),
			Intensity: 1.2,
		}},
	}
	// New tessellates the parts AddPart attached and calls no Builder.
	renderer, err := render.New(ctx, clip, style)
	if err != nil {
		fmt.Printf("failed to build the renderer: %s\n", err)
		return
	}

	dir, err := os.MkdirTemp("", ".tmp-kinetograph-reshape-*")
	if err != nil {
		fmt.Printf("failed to make a directory: %s\n", err)
		return
	}
	defer os.RemoveAll(dir)

	// One worker renders the frames in order, so the builds print in order.
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
	for _, f := range files {
		fmt.Printf("file: %s\n", filepath.Base(f))
	}
	// Output:
	// build: width 10 mm
	// build: width 20 mm
	// build: width 30 mm
	// frames: 4 at 4 fps
	// file: frame_000000.png
	// file: frame_000001.png
	// file: frame_000002.png
	// file: frame_000003.png
}
