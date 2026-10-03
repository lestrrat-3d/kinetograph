package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/units"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

// heroLength is the length of the hero shot. Every part returns to its
// starting pose by then and the camera completes a full turn, so the GIF loops
// without a jump.
const heroLength = 6 * time.Second

// pinDrop is how far each pin falls: from hanging 60 mm above the plate's
// bottom face to resting 2 mm above it, inside its bolt hole.
const (
	pinStartZ = 60.0
	pinEndZ   = 2.0
)

// heroShot is _clips/demo's motion, reversed back to its start:
//
//   - the plate sits on the root, still;
//   - each pin drops into a bolt hole on a prismatic joint and later rises out
//     of it, the right pin 300 ms after the left;
//   - the ring rises off the plate on a prismatic joint, turns a full turn
//     about X on a revolute joint under it, and settles back;
//   - the camera orbits a full turn about Z, the vertical axis through its
//     target.
func heroShot(ctx context.Context) (*kinetograph.Scene, render.Style, error) {
	plate, err := plateBody(ctx)
	if err != nil {
		return nil, render.Style{}, err
	}
	pin, err := pinBody(ctx)
	if err != nil {
		return nil, render.Style{}, err
	}
	ring, err := ringBody(ctx)
	if err != nil {
		return nil, render.Style{}, err
	}

	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	if err := scene.AddPart("plate", rig.Root(), plate); err != nil {
		return nil, render.Style{}, err
	}

	pins := []struct {
		name  string
		x     float64
		delay int
	}{
		{"pin-left", -boltOffset, 0},
		{"pin-right", boltOffset, 300},
	}
	for _, p := range pins {
		node, err := heroPinNode(rig.Root(), p.x, p.delay)
		if err != nil {
			return nil, render.Style{}, fmt.Errorf("%s: %w", p.name, err)
		}
		if err := scene.AddPart(p.name, node, pin); err != nil {
			return nil, render.Style{}, err
		}
	}

	ringNode, err := heroRingNode(rig.Root())
	if err != nil {
		return nil, render.Style{}, fmt.Errorf("ring: %w", err)
	}
	if err := scene.AddPart("ring", ringNode, ring); err != nil {
		return nil, render.Style{}, err
	}

	turn, err := kinetograph.NewChannel(
		keyAt(0, units.Degrees(0), nil),
		keyAt(int(heroLength/time.Millisecond), units.Degrees(360), nil),
	)
	if err != nil {
		return nil, render.Style{}, err
	}
	orbit, err := rig.Root().Revolute(r3.Vec{}, r3.NewVec(0, 0, 1), turn)
	if err != nil {
		return nil, render.Style{}, err
	}
	err = scene.SetCamera(orbit, kinetograph.Camera{
		Position: r3.NewVec(150, -235, 170),
		Target:   r3.NewVec(0, 0, 38),
		Up:       r3.NewVec(0, 0, 1),
		FOV:      kinetograph.Constant(units.Degrees(26)),
	})
	if err != nil {
		return nil, render.Style{}, err
	}
	return scene, assemblyStyle(), nil
}

// heroPinNode hangs a pin over the bolt hole at x, drops it into the hole from
// 200 ms after delay, and lifts it back out from 4.6 s after delay.
func heroPinNode(parent *kinetograph.Node, x float64, delay int) (*kinetograph.Node, error) {
	over, err := translated(parent, r3.NewVec(x, 0, pinStartZ))
	if err != nil {
		return nil, err
	}
	drop, err := kinetograph.NewChannel(
		keyAt(delay+200, units.Millimeters(0), nil),
		keyAt(delay+1200, units.Millimeters(pinStartZ-pinEndZ), kinetograph.EaseIn),
		keyAt(delay+4600, units.Millimeters(pinStartZ-pinEndZ), nil),
		keyAt(delay+5500, units.Millimeters(0), kinetograph.EaseInOut),
	)
	if err != nil {
		return nil, err
	}
	return over.Prismatic(r3.NewVec(0, 0, -1), drop)
}

// heroRingNode rests the ring on the plate, raises it 40 mm from 1.4 s to
// 2.4 s, turns it a full turn about X through its centre from 2.0 s to 4.2 s,
// and lowers it back by 4.9 s. The turn joint is the rise joint's child, so it
// turns about the ring's centre wherever the rise has carried it.
func heroRingNode(parent *kinetograph.Node) (*kinetograph.Node, error) {
	seated, err := translated(parent, r3.NewVec(0, 0, plateThickness+ringMinor))
	if err != nil {
		return nil, err
	}
	rise, err := kinetograph.NewChannel(
		keyAt(1400, units.Millimeters(0), nil),
		keyAt(2400, units.Millimeters(40), kinetograph.EaseInOut),
		keyAt(4200, units.Millimeters(40), nil),
		keyAt(4900, units.Millimeters(0), kinetograph.EaseInOut),
	)
	if err != nil {
		return nil, err
	}
	raised, err := seated.Prismatic(r3.NewVec(0, 0, 1), rise)
	if err != nil {
		return nil, err
	}
	tumble, err := kinetograph.NewChannel(
		keyAt(2000, units.Degrees(0), nil),
		keyAt(4200, units.Degrees(360), kinetograph.SmoothStep),
	)
	if err != nil {
		return nil, err
	}
	return raised.Revolute(r3.Vec{}, r3.NewVec(1, 0, 0), tumble)
}
