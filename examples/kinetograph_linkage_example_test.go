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

// armBlock extrudes the rectangle (x0, -7) to (x1, 7) 10 mm tall into doc.
func armBlock(ctx context.Context, doc *decad.Document, x0, x1 float64) (*decad.Body, error) {
	w := sketch.NewWorld()
	s, err := w.CreateSketch(w.XY())
	if err != nil {
		return nil, err
	}
	rect := s.CreateRectangle(x0, -7, x1, 7)
	s.Fix(rect.A)
	if _, err := s.Solve(ctx); err != nil {
		return nil, err
	}
	return doc.Extrude(s, s.Profiles()[0], decad.Distance{D: units.Millimeters(10), Dir: decad.Along})
}

// A decad linkage is filmed along its drive. Each link's node takes the pose
// decad's Linkage.PoseAt returns at the drive fraction s, the same pose
// Document.VerifyLinkage checks, and s at each time comes from a channel.
func Example_kinetograph_linkage() {
	ctx := context.Background()

	// An upper arm turns 0° to 90° about Z through the origin, and a forearm
	// turns 0° to −90° about Z through the elbow at (40, 0, 0). The two turns
	// cancel, so the forearm keeps its orientation while the elbow swings.
	doc := decad.New()
	upper, err := armBlock(ctx, doc, 0, 40)
	if err != nil {
		fmt.Printf("failed to build the upper arm: %s\n", err)
		return
	}
	forearm, err := armBlock(ctx, doc, 40, 80)
	if err != nil {
		fmt.Printf("failed to build the forearm: %s\n", err)
		return
	}
	linkage := decad.NewLinkage()
	shoulder, err := linkage.Ground().Revolute(r3.Vec{}, r3.Vec{Z: 1}, []*decad.Body{upper})
	if err != nil {
		fmt.Printf("failed to add the shoulder: %s\n", err)
		return
	}
	elbow, err := shoulder.Revolute(r3.Vec{X: 40}, r3.Vec{Z: 1}, []*decad.Body{forearm})
	if err != nil {
		fmt.Printf("failed to add the elbow: %s\n", err)
		return
	}
	drive := decad.Drive{
		{Link: shoulder, From: units.Degrees(0), To: units.Degrees(90)},
		{Link: elbow, From: units.Degrees(0), To: units.Degrees(-90)},
	}

	// The drive runs once over the first second and holds its end after it.
	fraction, err := kinetograph.NewChannel(
		kinetograph.Keyframe{At: 0, Value: units.Scalar(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Scalar(1)},
	)
	if err != nil {
		fmt.Printf("failed to build the fraction: %s\n", err)
		return
	}

	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	nodes, err := scene.AddLinkage(linkage, drive, fraction,
		map[*decad.Body]string{upper: "upper", forearm: "forearm"})
	if err != nil {
		fmt.Printf("failed to add the linkage: %s\n", err)
		return
	}
	err = scene.SetCamera(rig.Root(), kinetograph.Camera{
		Position: r3.Vec{X: 30, Y: 20, Z: 220},
		Target:   r3.Vec{X: 30, Y: 20},
		Up:       r3.Vec{Y: 1},
		FOV:      kinetograph.Constant(units.Degrees(40)),
	})
	if err != nil {
		fmt.Printf("failed to set the camera: %s\n", err)
		return
	}

	// Six frames at 4 fps: s runs 0, 1/4, 1/2, 3/4, 1, and the last frame,
	// at 1.25 s, holds s = 1.
	clip, err := kinetograph.NewClip(scene, 4, 1500*time.Millisecond)
	if err != nil {
		fmt.Printf("failed to build the clip: %s\n", err)
		return
	}
	for i := range clip.FrameCount() {
		at := clip.FrameTime(i)
		s, err := fraction.At(at)
		if err != nil {
			fmt.Printf("failed to read the fraction: %s\n", err)
			return
		}
		pose, err := nodes[1].World(at)
		if err != nil {
			fmt.Printf("failed to pose the forearm: %s\n", err)
			return
		}
		tip := pose.Apply(r3.Vec{X: 80})
		fmt.Printf("frame %d at %s: s = %.2f, forearm tip at (%.1f, %.1f, %.1f)\n",
			i, at, s.Mag(), oneDecimal(tip.X), oneDecimal(tip.Y), oneDecimal(tip.Z))
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

	dir, err := os.MkdirTemp("", ".tmp-kinetograph-linkage-*")
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
	// frame 0 at 0s: s = 0.00, forearm tip at (80.0, 0.0, 0.0)
	// frame 1 at 250ms: s = 0.25, forearm tip at (77.0, 15.3, 0.0)
	// frame 2 at 500ms: s = 0.50, forearm tip at (68.3, 28.3, 0.0)
	// frame 3 at 750ms: s = 0.75, forearm tip at (55.3, 37.0, 0.0)
	// frame 4 at 1s: s = 1.00, forearm tip at (40.0, 40.0, 0.0)
	// frame 5 at 1.25s: s = 1.00, forearm tip at (40.0, 40.0, 0.0)
	// file: frame_000000.png
	// file: frame_000001.png
	// file: frame_000002.png
	// file: frame_000003.png
	// file: frame_000004.png
	// file: frame_000005.png
}
