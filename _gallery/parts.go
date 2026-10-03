package main

import (
	"context"
	"fmt"

	"github.com/lestrrat-3d/decad"
	"github.com/lestrrat-3d/sketch"
	"github.com/lestrrat-3d/units"
)

// The drilled plate's size and its holes, as in _clips/demo. Every other part
// of the hero, orbit and light shots is placed relative to these, so the pins
// land in the bolt holes and the ring sits over the bore.
const (
	plateHalfX     = 48.0
	plateHalfY     = 34.0
	plateThickness = 16.0
	boreRadius     = 18.0
	boltOffset     = 36.0
	boltRadius     = 7.0
	pinRadius      = 6.0
	pinLength      = 28.0
	ringMajor      = 24.0
	ringMinor      = 6.0
)

// block extrudes the rectangle from (x0, y0) to (x1, y1) on the XY plane
// upward by height millimetres, so its bottom face lies on Z = 0. It builds the
// body in a document of its own.
func block(ctx context.Context, x0, y0, x1, y1, height float64) (*decad.Body, error) {
	return blockIn(ctx, decad.New(), sketch.NewWorld(), x0, y0, x1, y1, height)
}

// blockIn is block, built in doc from a sketch in w.
func blockIn(
	ctx context.Context, doc *decad.Document, w *sketch.World, x0, y0, x1, y1, height float64,
) (*decad.Body, error) {
	s, err := w.CreateSketch(w.XY())
	if err != nil {
		return nil, err
	}
	rect := s.CreateRectangle(x0, y0, x1, y1)
	s.Fix(rect.A)
	if _, err := s.Solve(ctx); err != nil {
		return nil, fmt.Errorf("solve the rectangle sketch: %w", err)
	}
	profile, err := validProfile(s)
	if err != nil {
		return nil, err
	}
	body, err := doc.Extrude(s, profile,
		decad.Distance{D: units.Millimeters(height), Dir: decad.Along})
	if err != nil {
		return nil, fmt.Errorf("extrude the block: %w", err)
	}
	return body, nil
}

// plateBody is a flange plate drilled with a central bore and two bolt holes,
// one Cut per hole. Its bottom face lies on Z = 0.
func plateBody(ctx context.Context) (*decad.Body, error) {
	w := sketch.NewWorld()
	doc := decad.New()
	plate, err := blockIn(ctx, doc, w, -plateHalfX, -plateHalfY, plateHalfX, plateHalfY, plateThickness)
	if err != nil {
		return nil, err
	}
	holes := []struct{ x, radius float64 }{{0, boreRadius}, {-boltOffset, boltRadius}, {boltOffset, boltRadius}}
	for _, hole := range holes {
		// The drill runs clear past both plate faces: a tool cap resting ON a
		// face is a face-on-face contact the boolean refuses.
		tool, err := cylinder(ctx, doc, w, hole.x, hole.radius, decad.Symmetric{D: units.Millimeters(3 * plateThickness)})
		if err != nil {
			return nil, err
		}
		plate, err = decad.Cut(ctx, plate, tool)
		if err != nil {
			return nil, fmt.Errorf("drill the plate: %w", err)
		}
	}
	return plate, nil
}

// pinBody is a round pin standing on Z = 0, centred on the Z axis.
func pinBody(ctx context.Context) (*decad.Body, error) {
	pin, err := cylinder(ctx, decad.New(), sketch.NewWorld(), 0, pinRadius,
		decad.Distance{D: units.Millimeters(pinLength), Dir: decad.Along})
	if err != nil {
		return nil, fmt.Errorf("extrude the pin: %w", err)
	}
	return pin, nil
}

// ringBody is a torus lying flat, centred on the origin: a circle on the XZ
// plane revolved about Z.
func ringBody(ctx context.Context) (*decad.Body, error) {
	w := sketch.NewWorld()
	s, err := w.CreateSketch(w.XZ())
	if err != nil {
		return nil, err
	}
	center := s.CreatePoint(ringMajor, 0)
	s.Fix(center)
	s.CreateCircle(center, ringMinor)
	if _, err := s.Solve(ctx); err != nil {
		return nil, fmt.Errorf("solve the ring sketch: %w", err)
	}
	profile, err := validProfile(s)
	if err != nil {
		return nil, err
	}
	axis := decad.SketchLine{Start: decad.Point2{U: 0, V: 0}, End: decad.Point2{U: 0, V: 1}}
	ring, err := decad.New().Revolve(s, profile, axis, decad.FullRevolution{})
	if err != nil {
		return nil, fmt.Errorf("revolve the ring: %w", err)
	}
	return ring, nil
}

// cylinder extrudes a circle of radius centred at (x, 0) on the XY plane by
// extent.
func cylinder(
	ctx context.Context, doc *decad.Document, w *sketch.World, x, radius float64, extent decad.Extent,
) (*decad.Body, error) {
	s, err := w.CreateSketch(w.XY())
	if err != nil {
		return nil, err
	}
	origin := s.CreatePoint(x, 0)
	s.Fix(origin)
	s.CreateCircle(origin, radius)
	if _, err := s.Solve(ctx); err != nil {
		return nil, fmt.Errorf("solve the circle sketch: %w", err)
	}
	profile, err := validProfile(s)
	if err != nil {
		return nil, err
	}
	return doc.Extrude(s, profile, extent)
}

// validProfile picks the one region a single-loop sketch bounds.
func validProfile(s *sketch.Sketch) (*sketch.Profile, error) {
	for _, candidate := range s.Profiles() {
		if candidate.Valid {
			return candidate, nil
		}
	}
	return nil, fmt.Errorf("sketch bounds no valid profile")
}
