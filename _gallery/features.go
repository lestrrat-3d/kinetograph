package main

import (
	"context"
	"fmt"
	"time"

	"github.com/lestrrat-3d/decad"
	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/solidlens"
	"github.com/lestrrat-3d/units"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

// tableLength is the length of every table shot. Each one ends in the pose it
// starts in, so the GIF loops without a jump.
const tableLength = 2 * time.Second

// keyAt is a keyframe at ms milliseconds.
func keyAt(ms int, v units.Value, ease kinetograph.Easing) kinetograph.Keyframe {
	return kinetograph.Keyframe{At: time.Duration(ms) * time.Millisecond, Value: v, Ease: ease}
}

// translated is a Fixed child of parent moved by v.
func translated(parent *kinetograph.Node, v r3.Vec) (*kinetograph.Node, error) {
	t, err := r3.Translation(v)
	if err != nil {
		return nil, err
	}
	return parent.Fixed(t)
}

// revoluteShot is a box whose lid opens 110 degrees on a revolute joint about
// the box's top back edge and closes again.
func revoluteShot(ctx context.Context) (*kinetograph.Scene, render.Style, error) {
	box, err := block(ctx, -30, -20, 30, 20, 20)
	if err != nil {
		return nil, render.Style{}, err
	}
	lid, err := block(ctx, -30, -20, 30, 20, 4)
	if err != nil {
		return nil, render.Style{}, err
	}
	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	if err := scene.AddPart("box", rig.Root(), box); err != nil {
		return nil, render.Style{}, err
	}
	open, err := kinetograph.NewChannel(
		keyAt(200, units.Degrees(0), nil),
		keyAt(900, units.Degrees(110), kinetograph.EaseInOut),
		keyAt(1100, units.Degrees(110), nil),
		keyAt(1800, units.Degrees(0), kinetograph.EaseInOut),
	)
	if err != nil {
		return nil, render.Style{}, err
	}
	// The axis runs along -X through the top back edge, so a positive angle
	// lifts the lid's front edge.
	hinge, err := rig.Root().Revolute(r3.NewVec(0, 20, 20), r3.NewVec(-1, 0, 0), open)
	if err != nil {
		return nil, render.Style{}, err
	}
	seated, err := translated(hinge, r3.NewVec(0, 0, 20))
	if err != nil {
		return nil, render.Style{}, err
	}
	if err := scene.AddPart("lid", seated, lid); err != nil {
		return nil, render.Style{}, err
	}
	if err := scene.SetCamera(rig.Root(), tableCamera()); err != nil {
		return nil, render.Style{}, err
	}
	style := baseStyle()
	style.Parts["lid"] = look(gold)
	return scene, style, nil
}

// prismaticShot is a carriage that slides 80 mm along a rail on a prismatic
// joint and back.
func prismaticShot(ctx context.Context) (*kinetograph.Scene, render.Style, error) {
	rail, err := block(ctx, -60, -10, 60, 10, 10)
	if err != nil {
		return nil, render.Style{}, err
	}
	carriage, err := block(ctx, -16, -14, 16, 14, 18)
	if err != nil {
		return nil, render.Style{}, err
	}
	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	if err := scene.AddPart("rail", rig.Root(), rail); err != nil {
		return nil, render.Style{}, err
	}
	slide, err := kinetograph.NewChannel(
		keyAt(100, units.Millimeters(0), nil),
		keyAt(900, units.Millimeters(80), kinetograph.EaseInOut),
		keyAt(1100, units.Millimeters(80), nil),
		keyAt(1900, units.Millimeters(0), kinetograph.EaseInOut),
	)
	if err != nil {
		return nil, render.Style{}, err
	}
	start, err := translated(rig.Root(), r3.NewVec(-40, 0, 10))
	if err != nil {
		return nil, render.Style{}, err
	}
	node, err := start.Prismatic(r3.NewVec(1, 0, 0), slide)
	if err != nil {
		return nil, render.Style{}, err
	}
	if err := scene.AddPart("carriage", node, carriage); err != nil {
		return nil, render.Style{}, err
	}
	if err := scene.SetCamera(rig.Root(), tableCamera()); err != nil {
		return nil, render.Style{}, err
	}
	style := baseStyle()
	style.Parts["carriage"] = look(gold)
	return scene, style, nil
}

// assembly adds the drilled plate with both pins seated in its bolt holes and
// the ring resting over its bore, all still, to scene.
func assembly(ctx context.Context, rig *kinetograph.Rig, scene *kinetograph.Scene) error {
	plate, err := plateBody(ctx)
	if err != nil {
		return err
	}
	pin, err := pinBody(ctx)
	if err != nil {
		return err
	}
	ring, err := ringBody(ctx)
	if err != nil {
		return err
	}
	if err := scene.AddPart("plate", rig.Root(), plate); err != nil {
		return err
	}
	pins := []struct {
		name string
		x    float64
	}{{"pin-left", -boltOffset}, {"pin-right", boltOffset}}
	for _, p := range pins {
		node, err := translated(rig.Root(), r3.NewVec(p.x, 0, 2))
		if err != nil {
			return err
		}
		if err := scene.AddPart(p.name, node, pin); err != nil {
			return err
		}
	}
	seat, err := translated(rig.Root(), r3.NewVec(0, 0, plateThickness+ringMinor))
	if err != nil {
		return err
	}
	return scene.AddPart("ring", seat, ring)
}

// assemblyStyle colours the assembly's pins gold and its ring blue over a
// violet plate.
func assemblyStyle() render.Style {
	style := baseStyle()
	style.Parts["pin-left"] = look(gold)
	style.Parts["pin-right"] = look(gold)
	style.Parts["ring"] = look(blue)
	return style
}

// fullTurn is a channel from 0 to 360 degrees at a constant rate over the
// table shot's length.
func fullTurn() (*kinetograph.Channel, error) {
	return kinetograph.NewChannel(
		keyAt(0, units.Degrees(0), nil),
		keyAt(int(tableLength/time.Millisecond), units.Degrees(360), nil),
	)
}

// orbitShot holds the assembly still while the camera, on a revolute node
// about the Z axis through its target, makes one full turn.
func orbitShot(ctx context.Context) (*kinetograph.Scene, render.Style, error) {
	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	if err := assembly(ctx, rig, scene); err != nil {
		return nil, render.Style{}, err
	}
	turn, err := fullTurn()
	if err != nil {
		return nil, render.Style{}, err
	}
	orbit, err := rig.Root().Revolute(r3.Vec{}, r3.NewVec(0, 0, 1), turn)
	if err != nil {
		return nil, render.Style{}, err
	}
	if err := scene.SetCamera(orbit, tableCamera()); err != nil {
		return nil, render.Style{}, err
	}
	return scene, assemblyStyle(), nil
}

// lightShot holds the assembly and the camera still while a white
// directional light on a revolute node about Z makes one full turn, lighting
// each side of the parts in turn. The fixed lights are dimmed so the moving
// light's sweep shows.
func lightShot(ctx context.Context) (*kinetograph.Scene, render.Style, error) {
	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	if err := assembly(ctx, rig, scene); err != nil {
		return nil, render.Style{}, err
	}
	turn, err := fullTurn()
	if err != nil {
		return nil, render.Style{}, err
	}
	orbit, err := rig.Root().Revolute(r3.Vec{}, r3.NewVec(0, 0, 1), turn)
	if err != nil {
		return nil, render.Style{}, err
	}
	// The light starts on +X, shining down and back toward the parts.
	lamp := kinetograph.Light{Kind: kinetograph.DirectionalLight, Direction: r3.NewVec(-1, 0, -0.5)}
	if err := scene.AddLight("lamp", orbit, lamp); err != nil {
		return nil, render.Style{}, err
	}
	if err := scene.SetCamera(rig.Root(), tableCamera()); err != nil {
		return nil, render.Style{}, err
	}
	style := assemblyStyle()
	style.DirectionalLights = fixedLights(0.15, 0.05)
	style.Lights = map[string]render.LightAppearance{
		"lamp": {Color: solidlens.RGB(1, 1, 1), Intensity: kinetograph.Constant(units.Scalar(1.4))},
	}
	return scene, style, nil
}

// fadeShot is a housing over the ring. The housing fades to 0.15 opacity,
// showing the ring inside, then fades back in.
func fadeShot(ctx context.Context) (*kinetograph.Scene, render.Style, error) {
	housing, err := block(ctx, -36, -36, 36, 36, 30)
	if err != nil {
		return nil, render.Style{}, err
	}
	ring, err := ringBody(ctx)
	if err != nil {
		return nil, render.Style{}, err
	}
	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	seat, err := translated(rig.Root(), r3.NewVec(0, 0, 15))
	if err != nil {
		return nil, render.Style{}, err
	}
	if err := scene.AddPart("ring", seat, ring); err != nil {
		return nil, render.Style{}, err
	}
	if err := scene.AddPart("housing", rig.Root(), housing); err != nil {
		return nil, render.Style{}, err
	}
	if err := scene.SetCamera(rig.Root(), tableCamera()); err != nil {
		return nil, render.Style{}, err
	}
	fade, err := kinetograph.NewChannel(
		keyAt(200, units.Scalar(1), nil),
		keyAt(800, units.Scalar(0.15), kinetograph.EaseInOut),
		keyAt(1200, units.Scalar(0.15), nil),
		keyAt(1800, units.Scalar(1), kinetograph.EaseInOut),
	)
	if err != nil {
		return nil, render.Style{}, err
	}
	style := baseStyle()
	housingLook := look(coral)
	housingLook.Fade = fade
	style.Parts["housing"] = housingLook
	style.Parts["ring"] = look(blue)
	return scene, style, nil
}

// slab builds a block centred on the Z axis, 30 mm deep, whose width and
// height are its "width" and "height" parameters.
type slab struct{}

func (slab) Build(ctx context.Context, params kinetograph.Params) (*decad.Body, error) {
	w, err := params["width"].In(units.Millimeter)
	if err != nil {
		return nil, fmt.Errorf("width: %w", err)
	}
	h, err := params["height"].In(units.Millimeter)
	if err != nil {
		return nil, fmt.Errorf("height: %w", err)
	}
	return block(ctx, -w/2, -15, w/2, 15, h)
}

// reshapeShot is a parametric slab whose width grows from 30 to 90 mm and its
// height from 10 to 26 mm, then both shrink back. Each frame's body is
// rebuilt from the two channels' values.
func reshapeShot(_ context.Context) (*kinetograph.Scene, render.Style, error) {
	ramp := func(from, to float64) (*kinetograph.Channel, error) {
		return kinetograph.NewChannel(
			keyAt(100, units.Millimeters(from), nil),
			keyAt(900, units.Millimeters(to), kinetograph.EaseInOut),
			keyAt(1100, units.Millimeters(to), nil),
			keyAt(1900, units.Millimeters(from), kinetograph.EaseInOut),
		)
	}
	width, err := ramp(30, 90)
	if err != nil {
		return nil, render.Style{}, err
	}
	height, err := ramp(10, 26)
	if err != nil {
		return nil, render.Style{}, err
	}
	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	params := map[string]*kinetograph.Channel{"width": width, "height": height}
	if err := scene.AddParametric("slab", rig.Root(), slab{}, params); err != nil {
		return nil, render.Style{}, err
	}
	if err := scene.SetCamera(rig.Root(), tableCamera()); err != nil {
		return nil, render.Style{}, err
	}
	style := baseStyle()
	style.Parts["slab"] = look(gold)
	return scene, style, nil
}
