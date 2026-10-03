package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/solidlens"
	"github.com/lestrrat-3d/units"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

// clipDuration is the length of the clip. Every part stops moving at 4 s and
// holds its pose for the last half second while the camera finishes its orbit.
const clipDuration = 4500 * time.Millisecond

// Each pin falls from hanging 60 mm above the plate's bottom face to resting
// 2 mm above it, inside its bolt hole.
const (
	pinStartZ = 60.0
	pinEndZ   = 2.0
)

// The palette and light set are decad's _gallery ones, so the clip reads as
// part of the same project.
var (
	backgroundColor = solidlens.RGB(0.82, 0.86, 0.93)
	blue            = solidlens.RGB(0.18, 0.47, 1)
	violet          = solidlens.RGB(0.58, 0.24, 1)
	gold            = solidlens.RGB(1, 0.68, 0.08)
)

// buildScene assembles the clip's rig:
//
//   - the plate sits on the root, still;
//   - each pin hangs over a bolt hole and drops into it on a prismatic joint,
//     the right pin 300 ms after the left;
//   - the ring rests on the plate over the bore, rises on a prismatic joint,
//     then tumbles half a turn about X on a revolute joint under it;
//   - the camera orbits a quarter turn about Z, the vertical axis through its
//     target.
func buildScene(ctx context.Context) (*kinetograph.Scene, error) {
	plate, err := plateBody(ctx)
	if err != nil {
		return nil, err
	}
	pin, err := pinBody(ctx)
	if err != nil {
		return nil, err
	}
	ring, err := ringBody(ctx)
	if err != nil {
		return nil, err
	}

	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	if err := scene.AddPart("plate", rig.Root(), plate); err != nil {
		return nil, err
	}

	pins := []struct {
		name  string
		x     float64
		start time.Duration
	}{
		{"pin-left", -boltOffset, 0},
		{"pin-right", boltOffset, 300 * time.Millisecond},
	}
	for _, p := range pins {
		node, err := pinNode(rig.Root(), p.x, p.start)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", p.name, err)
		}
		if err := scene.AddPart(p.name, node, pin); err != nil {
			return nil, err
		}
	}

	ringNode, err := ringNode(rig.Root())
	if err != nil {
		return nil, fmt.Errorf("ring: %w", err)
	}
	if err := scene.AddPart("ring", ringNode, ring); err != nil {
		return nil, err
	}

	cameraNode, err := cameraNode(rig.Root())
	if err != nil {
		return nil, fmt.Errorf("camera: %w", err)
	}
	err = scene.SetCamera(cameraNode, kinetograph.Camera{
		Position: r3.NewVec(150, -235, 170),
		Target:   r3.NewVec(0, 0, 38),
		Up:       r3.NewVec(0, 0, 1),
		FOV:      kinetograph.Constant(units.Degrees(26)),
	})
	if err != nil {
		return nil, err
	}
	return scene, nil
}

// pinNode hangs a pin over the bolt hole at x and drops it over one second
// from start.
func pinNode(parent *kinetograph.Node, x float64, start time.Duration) (*kinetograph.Node, error) {
	hang, err := r3.Translation(r3.NewVec(x, 0, pinStartZ))
	if err != nil {
		return nil, err
	}
	over, err := parent.Fixed(hang)
	if err != nil {
		return nil, err
	}
	drop, err := kinetograph.NewChannel(
		kinetograph.Keyframe{At: start, Value: units.Millimeters(0)},
		kinetograph.Keyframe{At: start + time.Second, Value: units.Millimeters(pinStartZ - pinEndZ), Ease: kinetograph.EaseIn},
	)
	if err != nil {
		return nil, err
	}
	return over.Prismatic(r3.NewVec(0, 0, -1), drop)
}

// ringNode rests the ring on the plate, raises it 40 mm from 1.0 s to 2.4 s,
// and turns it half a turn about X through its centre from 2.0 s to 4.0 s.
// The tumble joint is the rise joint's child, so it turns about the ring's
// centre wherever the rise has carried it.
func ringNode(parent *kinetograph.Node) (*kinetograph.Node, error) {
	rest, err := r3.Translation(r3.NewVec(0, 0, plateThickness+ringMinor))
	if err != nil {
		return nil, err
	}
	seated, err := parent.Fixed(rest)
	if err != nil {
		return nil, err
	}
	rise, err := kinetograph.NewChannel(
		kinetograph.Keyframe{At: time.Second, Value: units.Millimeters(0)},
		kinetograph.Keyframe{At: 2400 * time.Millisecond, Value: units.Millimeters(40), Ease: kinetograph.EaseInOut},
	)
	if err != nil {
		return nil, err
	}
	raised, err := seated.Prismatic(r3.NewVec(0, 0, 1), rise)
	if err != nil {
		return nil, err
	}
	tumble, err := kinetograph.NewChannel(
		kinetograph.Keyframe{At: 2 * time.Second, Value: units.Degrees(0)},
		kinetograph.Keyframe{At: 4 * time.Second, Value: units.Degrees(180), Ease: kinetograph.SmoothStep},
	)
	if err != nil {
		return nil, err
	}
	return raised.Revolute(r3.Vec{}, r3.NewVec(1, 0, 0), tumble)
}

// cameraNode orbits a quarter turn about the world Z axis over the whole clip.
// The camera's target lies on that axis, so the camera circles what it looks
// at (docs/design.md D5).
func cameraNode(parent *kinetograph.Node) (*kinetograph.Node, error) {
	orbit, err := kinetograph.NewChannel(
		kinetograph.Keyframe{At: 0, Value: units.Degrees(0)},
		kinetograph.Keyframe{At: clipDuration, Value: units.Degrees(90)},
	)
	if err != nil {
		return nil, err
	}
	return parent.Revolute(r3.Vec{}, r3.NewVec(0, 0, 1), orbit)
}

// clipStyle is the look of every frame at width x height pixels.
func clipStyle(width, height int) render.Style {
	edges := solidlens.Outline(solidlens.RGB(0.08, 0.08, 0.12))
	return render.Style{
		Width:      width,
		Height:     height,
		Chord:      units.Millimeters(0.05),
		Background: backgroundColor,
		Default: render.Appearance{
			Material: solidlens.Matte(violet),
			Edges:    edges,
		},
		Parts: map[string]render.Appearance{
			"pin-left":  {Material: solidlens.Matte(gold), Edges: edges},
			"pin-right": {Material: solidlens.Matte(gold), Edges: edges},
			"ring":      {Material: solidlens.Matte(blue), Edges: edges},
		},
		DirectionalLights: []solidlens.DirectionalLight{
			{
				Direction: solidlens.Vec{X: -0.7, Y: 0.12, Z: -1},
				Color:     solidlens.RGB(1, 1, 1),
				Intensity: 1.25,
			},
			{
				Direction: solidlens.Vec{X: 0.6, Y: -0.2, Z: -0.6},
				Color:     solidlens.RGB(0.25, 0.55, 1),
				Intensity: 0.4,
			},
		},
	}
}
