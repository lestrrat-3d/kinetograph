package main

import (
	"context"
	"fmt"
	"math"

	"github.com/lestrrat-3d/decad"
	"github.com/lestrrat-3d/sketch"
	"github.com/lestrrat-3d/units"
)

// point is a plane-local sketch coordinate in millimetres.
type point struct {
	x, y float64
}

// rectangle is the closed loop of the axis-aligned rectangle with corners
// (x0, y0) and (x1, y1).
func rectangle(x0, y0, x1, y1 float64) []point {
	return []point{{x0, y0}, {x1, y0}, {x1, y1}, {x0, y1}}
}

// circle is a closed polygon of segments points on the circle of radius
// about (cx, cy).
func circle(cx, cy, radius float64, segments int) []point {
	points := make([]point, segments)
	for i := range points {
		angle := 2 * math.Pi * float64(i) / float64(segments)
		points[i] = point{cx + radius*math.Cos(angle), cy + radius*math.Sin(angle)}
	}
	return points
}

// polylineProfile draws each loop as a closed chain of lines in s, solves the
// sketch, and returns the region those loops bound. The first loop is the
// outline and every later one is a hole, so the profile is picked by its hole
// count.
func polylineProfile(ctx context.Context, s *sketch.Sketch, loops [][]point) (*sketch.Profile, error) {
	for loopIndex, loop := range loops {
		if len(loop) < 3 {
			return nil, fmt.Errorf("loop %d has fewer than three points", loopIndex)
		}
		points := make([]*sketch.Point, len(loop))
		for i, p := range loop {
			points[i] = s.CreatePoint(p.x, p.y)
		}
		if loopIndex == 0 {
			s.Fix(points[0])
		}
		for i := range points {
			s.CreateLine(points[i], points[(i+1)%len(points)])
		}
	}
	if _, err := s.Solve(ctx); err != nil {
		return nil, err
	}
	for _, candidate := range s.Profiles() {
		if candidate.Valid && len(candidate.Holes)+1 == len(loops) {
			return candidate, nil
		}
	}
	return nil, fmt.Errorf("sketch produced no profile for %d loops", len(loops))
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

// sketchLoops draws loops on a new sketch on plane and returns the sketch and
// the region the loops bound.
func sketchLoops(
	ctx context.Context, w *sketch.World, plane *sketch.Plane, loops ...[]point,
) (*sketch.Sketch, *sketch.Profile, error) {
	s, err := w.CreateSketch(plane)
	if err != nil {
		return nil, nil, err
	}
	profile, err := polylineProfile(ctx, s, loops)
	if err != nil {
		return nil, nil, err
	}
	return s, profile, nil
}

// prism extrudes the region loops bound on plane by height millimetres
// along the plane normal.
func prism(
	ctx context.Context, doc *decad.Document, w *sketch.World, plane *sketch.Plane, height float64, loops ...[]point,
) (*decad.Body, error) {
	s, profile, err := sketchLoops(ctx, w, plane, loops...)
	if err != nil {
		return nil, err
	}
	return doc.Extrude(s, profile, decad.Distance{D: units.Millimeters(height), Dir: decad.Along})
}

// cylinder extrudes the circle of radius about plane-local (x, 0) on plane
// by extent.
func cylinder(
	ctx context.Context, doc *decad.Document, w *sketch.World, plane *sketch.Plane,
	x, radius float64, extent decad.Extent,
) (*decad.Body, error) {
	s, err := w.CreateSketch(plane)
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
