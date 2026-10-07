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

// barPrism extrudes a bar of half-width 4 mm from p to q in the XY plane,
// 8 mm tall, into doc.
func barPrism(ctx context.Context, doc *decad.Document, p, q r3.Vec) (*decad.Body, error) {
	d := q.Sub(p)
	n, ok := r3.Vec{X: -d.Y, Y: d.X}.Normalize()
	if !ok {
		return nil, fmt.Errorf("bar from %v to %v has no length", p, q)
	}
	n = n.Scale(4)
	w := sketch.NewWorld()
	s, err := w.CreateSketch(w.XY())
	if err != nil {
		return nil, err
	}
	corners := []r3.Vec{p.Sub(n), q.Sub(n), q.Add(n), p.Add(n)}
	points := make([]*sketch.Point, len(corners))
	for i, c := range corners {
		points[i] = s.CreatePoint(c.X, c.Y)
		s.Fix(points[i])
	}
	for i := range points {
		s.CreateLine(points[i], points[(i+1)%len(points)])
	}
	if _, err := s.Solve(ctx); err != nil {
		return nil, err
	}
	return doc.Extrude(s, s.Profiles()[0], decad.Distance{D: units.Millimeters(8), Dir: decad.Along})
}

// A decad linkage with a closed loop is filmed through a decad.Schedule. The
// drive states only the crank; decad finds the coupler's and the follower's
// joints from the loop, and the schedule keeps that work between frames.
func Example_kinetograph_schedule() {
	ctx := context.Background()

	// A crank-rocker: ground 100 mm, crank 30 mm turning about Z through the
	// origin, coupler 80 mm hung from the crank at A = (30, 0, 0), and
	// follower 70 mm turning about Z through (100, 0, 0). The coupler is
	// closed onto the follower at the pin B above the ground line.
	const ground, crankLen, couplerLen, followerLen = 100.0, 30.0, 80.0, 70.0
	d := ground - crankLen
	theta4 := math.Pi - math.Acos((followerLen*followerLen+d*d-couplerLen*couplerLen)/(2*followerLen*d))
	a := r3.Vec{X: crankLen}
	b := r3.Vec{X: ground + followerLen*math.Cos(theta4), Y: followerLen * math.Sin(theta4)}

	doc := decad.New()
	crankBody, err := barPrism(ctx, doc, r3.Vec{}, a)
	if err != nil {
		fmt.Printf("failed to build the crank: %s\n", err)
		return
	}
	couplerBody, err := barPrism(ctx, doc, a, b)
	if err != nil {
		fmt.Printf("failed to build the coupler: %s\n", err)
		return
	}
	followerBody, err := barPrism(ctx, doc, r3.Vec{X: ground}, b)
	if err != nil {
		fmt.Printf("failed to build the follower: %s\n", err)
		return
	}
	z := r3.Vec{Z: 1}
	linkage := decad.NewLinkage()
	crank, err := linkage.Ground().Revolute(r3.Vec{}, z, []*decad.Body{crankBody})
	if err != nil {
		fmt.Printf("failed to add the crank: %s\n", err)
		return
	}
	coupler, err := crank.Revolute(a, z, []*decad.Body{couplerBody})
	if err != nil {
		fmt.Printf("failed to add the coupler: %s\n", err)
		return
	}
	follower, err := linkage.Ground().Revolute(r3.Vec{X: ground}, z, []*decad.Body{followerBody})
	if err != nil {
		fmt.Printf("failed to add the follower: %s\n", err)
		return
	}
	if _, err := linkage.Close(coupler, follower, b, z); err != nil {
		fmt.Printf("failed to close the loop: %s\n", err)
		return
	}

	// The schedule is built once, before any frame.
	schedule, err := linkage.Schedule(ctx, decad.Drive{
		{Link: crank, From: units.Degrees(0), To: units.Degrees(90)},
	})
	if err != nil {
		fmt.Printf("failed to schedule the drive: %s\n", err)
		return
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
	nodes, err := scene.AddSchedule(schedule, fraction, map[*decad.Body]string{
		crankBody: "crank", couplerBody: "coupler", followerBody: "follower",
	})
	if err != nil {
		fmt.Printf("failed to add the schedule: %s\n", err)
		return
	}
	err = scene.SetCamera(rig.Root(), kinetograph.Camera{
		Position: r3.Vec{X: 50, Y: 30, Z: 260},
		Target:   r3.Vec{X: 50, Y: 30},
		Up:       r3.Vec{Y: 1},
		FOV:      kinetograph.Constant(units.Degrees(40)),
	})
	if err != nil {
		fmt.Printf("failed to set the camera: %s\n", err)
		return
	}

	// Five frames at 4 fps: s runs 0, 1/4, 1/2, 3/4, 1. The follower's node
	// carries the pin B along with the follower, so its world transform
	// gives the pin's position and the follower's angle at each frame.
	clip, err := kinetograph.NewClip(scene, 4, 1250*time.Millisecond)
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
		pose, err := nodes[2].World(at)
		if err != nil {
			fmt.Printf("failed to pose the follower: %s\n", err)
			return
		}
		pin := pose.Apply(b)
		angle := math.Atan2(pin.Y, pin.X-ground) * 180 / math.Pi
		fmt.Printf("frame %d at %s: s = %.2f, pin at (%.1f, %.1f), follower at %.1f°\n",
			i, at, s.Mag(), oneDecimal(pin.X), oneDecimal(pin.Y), oneDecimal(angle))
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

	dir, err := os.MkdirTemp("", ".tmp-kinetograph-schedule-*")
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
	// frame 0 at 0s: s = 0.00, pin at (75.7, 65.7), follower at 110.3°
	// frame 1 at 250ms: s = 0.25, pin at (84.2, 68.2), follower at 103.1°
	// frame 2 at 500ms: s = 0.50, pin at (85.7, 68.5), follower at 101.8°
	// frame 3 at 750ms: s = 0.75, pin at (81.0, 67.4), follower at 105.8°
	// frame 4 at 1s: s = 1.00, pin at (72.3, 64.3), follower at 113.3°
	// file: frame_000000.png
	// file: frame_000001.png
	// file: frame_000002.png
	// file: frame_000003.png
	// file: frame_000004.png
}
