package main

import (
	"context"
	"fmt"
	"strconv"

	"github.com/lestrrat-3d/decad"
	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/sketch"
	"github.com/lestrrat-3d/solidlens"
	"github.com/lestrrat-3d/units"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

// shelfPitch is the distance between two neighbouring parts on the shelf
// along X. The largest footprint half-diagonals, the tray's 56 mm and the
// dish's at most 59 mm, leave at least 25 mm between neighbours at every spin
// angle.
const shelfPitch = 140.0

// dollyMargin is how far before the first part the shelf camera starts, and
// how far past the last part it ends.
const dollyMargin = 87.5

// The shelf camera is _gallery/scene.go's, in the dolly joint's frame.
var (
	shelfCameraPosition = r3.NewVec(105, -165, 110)
	shelfCameraTarget   = r3.NewVec(0, 0, 12)
)

// shelfPart is one of act B's parts: one or more bodies drawn alike that
// turn together.
type shelfPart struct {
	bodies     []*decad.Body
	appearance render.Appearance
}

// shelfSlot is one place on the shelf: the part's name, which names its spin
// track shape.<name>.spin, and its builder.
type shelfSlot struct {
	name  string
	build func(context.Context) (shelfPart, error)
}

// shelf is act B's parts in shelf order. Each builder builds its part as
// _gallery/features.go does, at the gallery's own coordinates.
var shelf = []shelfSlot{
	{name: "ring", build: ringPart},
	{name: "duct", build: ductPart},
	{name: "loft", build: loftPart},
	{name: "blade", build: bladePart},
	{name: "tray", build: trayPart},
	{name: "dish", build: dishPart},
}

// shapesTake is act B: the six parts in shelf order, each turning about the
// centre of its own footprint, passed by a dollying camera.
func shapesTake(ctx context.Context, ch *Channels) (*Take, error) {
	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	parts := map[string]render.Appearance{}
	for k, slot := range shelf {
		name := slot.name
		part, err := slot.build(ctx)
		if err != nil {
			return nil, fmt.Errorf("shelf part %s: %w", name, err)
		}
		node, err := shelfNode(rig.Root(), ch, k, name, part.bodies)
		if err != nil {
			return nil, fmt.Errorf("shelf part %s: %w", name, err)
		}
		for i, body := range part.bodies {
			partName := name
			if len(part.bodies) > 1 {
				partName = name + "." + strconv.Itoa(i)
			}
			if err := scene.AddPart(partName, node, body); err != nil {
				return nil, err
			}
			parts[partName] = part.appearance
		}
	}

	dolly, err := ch.Get("shapes.dolly")
	if err != nil {
		return nil, err
	}
	cameraNode, err := rig.Root().Prismatic(r3.NewVec(1, 0, 0), dolly)
	if err != nil {
		return nil, fmt.Errorf("camera: %w", err)
	}
	err = scene.SetCamera(cameraNode, kinetograph.Camera{
		Position: shelfCameraPosition,
		Target:   shelfCameraTarget,
		Up:       r3.NewVec(0, 0, 1),
		FOV:      kinetograph.Constant(units.Degrees(30)),
	})
	if err != nil {
		return nil, fmt.Errorf("camera: %w", err)
	}
	return &Take{Scene: scene, Style: shapesStyle(parts)}, nil
}

// shelfNode is root -> Fixed translation to slot k -> Revolute about Z by
// track shape.<name>.spin -> Fixed translation by -c, where c is the centre
// of the bodies' combined XY extent.
func shelfNode(root *kinetograph.Node, ch *Channels, k int, name string, bodies []*decad.Body) (*kinetograph.Node, error) {
	centre, err := footprintCentre(bodies)
	if err != nil {
		return nil, err
	}
	slot, err := r3.Translation(r3.NewVec(shelfPitch*float64(k), 0, 0))
	if err != nil {
		return nil, err
	}
	slotNode, err := root.Fixed(slot)
	if err != nil {
		return nil, err
	}
	spin, err := ch.Get("shape." + name + ".spin")
	if err != nil {
		return nil, err
	}
	spinNode, err := slotNode.Revolute(r3.Vec{}, r3.NewVec(0, 0, 1), spin)
	if err != nil {
		return nil, err
	}
	recentre, err := r3.Translation(r3.NewVec(-centre.X, -centre.Y, 0))
	if err != nil {
		return nil, err
	}
	return spinNode.Fixed(recentre)
}

// footprintCentre is the centre of the bodies' combined XY extent, from
// Body.Bounds.
func footprintCentre(bodies []*decad.Body) (r3.Vec, error) {
	var lo, hi r3.Vec
	for i, body := range bodies {
		box, err := body.Bounds()
		if err != nil {
			return r3.Vec{}, err
		}
		if i == 0 {
			lo, hi = box.Min, box.Max
			continue
		}
		lo = r3.NewVec(min(lo.X, box.Min.X), min(lo.Y, box.Min.Y), 0)
		hi = r3.NewVec(max(hi.X, box.Max.X), max(hi.Y, box.Max.Y), 0)
	}
	return r3.NewVec((lo.X+hi.X)/2, (lo.Y+hi.Y)/2, 0), nil
}

// ringPart revolves a circle of radius 14 mm, 38 mm off the axis, into a
// flat torus.
func ringPart(ctx context.Context) (shelfPart, error) {
	w := sketch.NewWorld()
	s, err := w.CreateSketch(w.XZ())
	if err != nil {
		return shelfPart{}, err
	}
	center := s.CreatePoint(38, 0)
	s.Fix(center)
	s.CreateCircle(center, 14)
	if _, err := s.Solve(ctx); err != nil {
		return shelfPart{}, err
	}
	profile, err := validProfile(s)
	if err != nil {
		return shelfPart{}, err
	}
	axis := decad.SketchLine{Start: decad.Point2{U: 0, V: 0}, End: decad.Point2{U: 0, V: 1}}
	ring, err := decad.New().Revolve(s, profile, axis, decad.FullRevolution{})
	if err != nil {
		return shelfPart{}, fmt.Errorf("revolve the ring: %w", err)
	}
	return shelfPart{bodies: []*decad.Body{ring}, appearance: matte(blue)}, nil
}

// ductPart sweeps a 12 mm square section along an arc, a line and a second
// arc in another plane, which proves the path. decad cannot tessellate a
// Sweep body, so the part is drawn as _gallery draws it: the two Revolve
// spans and the Extrude span that fill the same space.
func ductPart(ctx context.Context) (shelfPart, error) {
	w := sketch.NewWorld()
	section := rectangle(-46, -6, -34, 6)
	s, profile, err := sketchLoops(ctx, w, w.XY(), section)
	if err != nil {
		return shelfPart{}, err
	}
	path, err := decad.NewPath(
		r3.NewVec(-40, 0, 0),
		decad.ArcThrough{Through: r3.NewVec(-32, 0, 16), End: r3.NewVec(-20, 0, 20)},
		decad.LineTo{End: r3.NewVec(20, 0, 20)},
		decad.ArcThrough{Through: r3.NewVec(36, 8, 20), End: r3.NewVec(40, 20, 20)},
	)
	if err != nil {
		return shelfPart{}, fmt.Errorf("record the sweep path: %w", err)
	}
	if _, err := decad.New().Sweep(ctx, s, profile, path); err != nil {
		return shelfPart{}, fmt.Errorf("sweep the spatial path: %w", err)
	}

	first, err := decad.New().Revolve(s, profile,
		decad.SketchLine{Start: decad.Point2{U: -20, V: -1}, End: decad.Point2{U: -20, V: 1}},
		decad.AngleExtent{A: units.Degrees(90), Dir: decad.Along})
	if err != nil {
		return shelfPart{}, fmt.Errorf("build the first span: %w", err)
	}
	middleSketch, middleProfile, err := framedSketch(ctx, w, r3.NewVec(-20, 0, -20), section)
	if err != nil {
		return shelfPart{}, err
	}
	middle, err := decad.New().Extrude(middleSketch, middleProfile,
		decad.Distance{D: units.Millimeters(40), Dir: decad.Along})
	if err != nil {
		return shelfPart{}, fmt.Errorf("build the straight span: %w", err)
	}
	lastSketch, lastProfile, err := framedSketch(ctx, w, r3.NewVec(20, 0, -20), section)
	if err != nil {
		return shelfPart{}, err
	}
	last, err := decad.New().Revolve(lastSketch, lastProfile,
		decad.SketchLine{Start: decad.Point2{U: -39, V: 20}, End: decad.Point2{U: -41, V: 20}},
		decad.AngleExtent{A: units.Degrees(90), Dir: decad.Along})
	if err != nil {
		return shelfPart{}, fmt.Errorf("build the second span: %w", err)
	}
	return shelfPart{bodies: []*decad.Body{first, middle, last}, appearance: matte(cyan)}, nil
}

// framedSketch draws loop on a plane with origin at origin, normal -Z and u
// axis +Y, the frame _gallery's duct spans use.
func framedSketch(
	ctx context.Context, w *sketch.World, origin r3.Vec, loop []point,
) (*sketch.Sketch, *sketch.Profile, error) {
	frame, err := r3.NewFrame(origin, r3.NewVec(0, 0, -1), r3.NewVec(0, 1, 0))
	if err != nil {
		return nil, nil, err
	}
	plane, err := w.CreatePlaneFromFrame(frame)
	if err != nil {
		return nil, nil, err
	}
	return sketchLoops(ctx, w, plane, loop)
}

// loftPart lofts an 84×60 mm rectangle into a smaller, offset one 46 mm
// above it: a transition duct.
func loftPart(ctx context.Context) (shelfPart, error) {
	w := sketch.NewWorld()
	bottom, bottomProfile, err := sketchLoops(ctx, w, w.XY(), rectangle(-42, -30, 42, 30))
	if err != nil {
		return shelfPart{}, err
	}
	topPlane, err := w.CreateOffsetPlane(w.XY(), 46)
	if err != nil {
		return shelfPart{}, err
	}
	top, topProfile, err := sketchLoops(ctx, w, topPlane, rectangle(-4, -14, 32, 14))
	if err != nil {
		return shelfPart{}, err
	}
	duct, err := decad.New().Loft(ctx, bottom, bottomProfile, top, topProfile)
	if err != nil {
		return shelfPart{}, fmt.Errorf("loft the duct: %w", err)
	}
	return shelfPart{bodies: []*decad.Body{duct}, appearance: matte(violet)}, nil
}

// bladePart extrudes a section bounded by a fit spline and a straight chord
// 30 mm.
func bladePart(ctx context.Context) (shelfPart, error) {
	w := sketch.NewWorld()
	s, err := w.CreateSketch(w.XY())
	if err != nil {
		return shelfPart{}, err
	}
	fit := make([]*sketch.Point, 0, 5)
	for _, p := range []point{{-46, 14}, {-26, -6}, {0, -14}, {26, -6}, {46, 14}} {
		fit = append(fit, s.CreatePoint(p.x, p.y))
	}
	s.Fix(fit[0])
	if _, err := s.CreateFitSpline(fit...); err != nil {
		return shelfPart{}, err
	}
	s.CreateLine(fit[len(fit)-1], fit[0])
	if _, err := s.Solve(ctx); err != nil {
		return shelfPart{}, err
	}
	profile, err := validProfile(s)
	if err != nil {
		return shelfPart{}, err
	}
	blade, err := decad.New().Extrude(s, profile, decad.Distance{D: units.Millimeters(30), Dir: decad.Along})
	if err != nil {
		return shelfPart{}, fmt.Errorf("extrude the blade: %w", err)
	}
	return shelfPart{bodies: []*decad.Body{blade}, appearance: matte(coral)}, nil
}

// trayPart shells a 92×64×34 mm block through its top face, leaving a 7 mm
// wall.
func trayPart(ctx context.Context) (shelfPart, error) {
	w := sketch.NewWorld()
	block, err := prism(ctx, decad.New(), w, w.XY(), 34, rectangle(-46, -32, 46, 32))
	if err != nil {
		return shelfPart{}, err
	}
	tray, err := block.Shell(ctx, decad.Faces(decad.Facing(r3.NewVec(0, 0, 1))), units.Millimeters(7))
	if err != nil {
		return shelfPart{}, fmt.Errorf("shell the block: %w", err)
	}
	return shelfPart{bodies: []*decad.Body{tray}, appearance: matte(blue)}, nil
}

// dishPart revolves a half-disc of radius 42 mm through 150° and keeps only
// the swept wall: a sheet, violet outside and gold on its inner side.
func dishPart(ctx context.Context) (shelfPart, error) {
	w := sketch.NewWorld()
	plane, err := w.CreateOffsetPlane(w.XZ(), 26)
	if err != nil {
		return shelfPart{}, err
	}
	s, err := w.CreateSketch(plane)
	if err != nil {
		return shelfPart{}, err
	}
	const radius, rise = 42.0, 20.0
	center := s.CreatePoint(0, rise)
	s.Fix(center)
	bottom := s.CreatePoint(0, rise-radius)
	top := s.CreatePoint(0, rise+radius)
	s.CreateLine(top, bottom)
	s.CreateArc(center, bottom, top)
	if _, err := s.Solve(ctx); err != nil {
		return shelfPart{}, err
	}
	profile, err := validProfile(s)
	if err != nil {
		return shelfPart{}, err
	}
	axis := decad.SketchLine{Start: decad.Point2{U: 0, V: 0}, End: decad.Point2{U: 0, V: 1}}
	dish, err := decad.New().Revolve(s, profile, axis,
		decad.AngleExtent{A: units.Degrees(150), Dir: decad.Along},
		decad.WithSurfaceResult())
	if err != nil {
		return shelfPart{}, fmt.Errorf("revolve the dish: %w", err)
	}
	if dish.Kind() != decad.BodySheet {
		return shelfPart{}, fmt.Errorf("dish is %v, want a sheet", dish.Kind())
	}
	inner := solidlens.Matte(gold)
	appearance := matte(violet)
	appearance.Back = &inner
	return shelfPart{bodies: []*decad.Body{dish}, appearance: appearance}, nil
}
