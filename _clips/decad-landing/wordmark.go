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

// The masthead's geometry is decad's _gallery/hero.go and
// _gallery/sketching.go at decad commit 7dde3ae229fd. hero.go returns
// tessellated meshes; the builders here return the bodies, so the rig can
// attach them and render tessellates them.
const (
	letterFilletRadius  = 2.0
	letterCapChamfer    = 1.5
	letterDepth         = 14.0
	plateDepth          = 12.0
	plateShellThickness = 1.8
	plateHalfDepth      = plateDepth / 2
	studRadius          = 6.0
)

// Each letter starts at (0, -letterOffsetY, letterOffsetZ) off its landed
// pose, above the frame and in front of the plate, and moves straight to it.
const (
	letterOffsetY = 80.0
	letterOffsetZ = 200.0
)

// The word lamp is a white point light that starts at wordLampStart, left of
// the plate and 46 mm in front of the letters' front faces (y = -14 mm), and
// slides wordLampTravel millimetres along +X. At wordLampIntensity it adds
// about 2.4 to a letter face it passes.
var wordLampStart = r3.NewVec(-170, -60, 40)

const (
	wordLampTravel    = 340.0
	wordLampIntensity = 5000.0
)

// wordLamp is the name of act C's moving light.
const wordLamp = "lamp.word"

// The wordmark camera is _gallery/hero.go's, in the dolly joint's frame.
var (
	wordCameraPosition = r3.NewVec(18, -360, 68)
	wordCameraTarget   = r3.NewVec(0, 0, 4)
)

// letterTrack is the name of letter i's descent track.
func letterTrack(i int) string {
	return "word." + strconv.Itoa(i) + ".in"
}

// letter is one wordmark letter: its colour and its shapes, each a list of
// loops on the XZ plane (outline first, then holes). chamferCap bevels each
// shape's front cap loop instead of filleting its convex vertical edges; see
// hero.go's letter type for why D and A keep the fillet.
type letter struct {
	color      solidlens.Color
	shapes     [][][]point
	chamferCap bool
}

// wordmarkTake is act C: the shelled plate, peg and dome on root, and five
// letters that drop in one after another while the camera creeps closer.
func wordmarkTake(ctx context.Context, ch *Channels) (*Take, error) {
	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	parts := map[string]render.Appearance{}

	plate, err := heroPlate(ctx)
	if err != nil {
		return nil, fmt.Errorf("backing plate: %w", err)
	}
	if err := scene.AddPart("plate", rig.Root(), plate); err != nil {
		return nil, err
	}
	parts["plate"] = matte(navy)

	for i, l := range decadLetters() {
		node, err := letterNode(rig.Root(), ch, i)
		if err != nil {
			return nil, fmt.Errorf("letter %d: %w", i, err)
		}
		for j, shape := range l.shapes {
			body, err := letterBody(ctx, shape, l.chamferCap)
			if err != nil {
				return nil, fmt.Errorf("letter %d shape %d: %w", i, j, err)
			}
			name := "letter." + strconv.Itoa(i) + "." + strconv.Itoa(j)
			if err := scene.AddPart(name, node, body); err != nil {
				return nil, err
			}
			parts[name] = matte(l.color)
		}
	}

	peg, err := xzPrism(ctx, [][]point{circle(-126, -70, 4.5, 24)}, decad.Distance{D: units.Millimeters(8), Dir: decad.Along})
	if err != nil {
		return nil, fmt.Errorf("peg: %w", err)
	}
	if err := scene.AddPart("peg", rig.Root(), peg); err != nil {
		return nil, err
	}
	parts["peg"] = matte(orange)

	dome, err := heroStud(ctx, 126, -70, studRadius)
	if err != nil {
		return nil, fmt.Errorf("dome: %w", err)
	}
	if err := scene.AddPart("dome", rig.Root(), dome); err != nil {
		return nil, err
	}
	parts["dome"] = matte(sky)

	lamp, err := addWordLamp(scene, rig.Root(), ch)
	if err != nil {
		return nil, fmt.Errorf("word lamp: %w", err)
	}
	if err := setWordCamera(scene, rig.Root(), ch); err != nil {
		return nil, fmt.Errorf("camera: %w", err)
	}
	return &Take{Scene: scene, Style: wordmarkStyle(parts, map[string]render.LightAppearance{wordLamp: lamp})}, nil
}

// addWordLamp hangs the word lamp off root -> Fixed translation to
// wordLampStart -> Prismatic +X (lamp.word.slide) and returns its look:
// white, at the intensity of track lamp.word.intensity.
func addWordLamp(scene *kinetograph.Scene, root *kinetograph.Node, ch *Channels) (render.LightAppearance, error) {
	slide, err := ch.Get("lamp.word.slide")
	if err != nil {
		return render.LightAppearance{}, err
	}
	intensity, err := ch.Get("lamp.word.intensity")
	if err != nil {
		return render.LightAppearance{}, err
	}
	start, err := r3.Translation(wordLampStart)
	if err != nil {
		return render.LightAppearance{}, err
	}
	at, err := root.Fixed(start)
	if err != nil {
		return render.LightAppearance{}, err
	}
	node, err := at.Prismatic(r3.NewVec(1, 0, 0), slide)
	if err != nil {
		return render.LightAppearance{}, err
	}
	if err := scene.AddLight(wordLamp, node, kinetograph.Light{Kind: kinetograph.PointLight}); err != nil {
		return render.LightAppearance{}, err
	}
	return whiteLamp(intensity), nil
}

// letterNode is root -> Fixed translation (0, -80, 200) -> Prismatic along
// (0, 80, -200) by letter i's track, which ends at that vector's length.
func letterNode(root *kinetograph.Node, ch *Channels, i int) (*kinetograph.Node, error) {
	start, err := r3.Translation(r3.NewVec(0, -letterOffsetY, letterOffsetZ))
	if err != nil {
		return nil, err
	}
	above, err := root.Fixed(start)
	if err != nil {
		return nil, err
	}
	in, err := ch.Get(letterTrack(i))
	if err != nil {
		return nil, err
	}
	return above.Prismatic(r3.NewVec(0, letterOffsetY, -letterOffsetZ), in)
}

// setWordCamera puts hero.go's camera on root -> Prismatic along its view
// direction by track word.dolly.
func setWordCamera(scene *kinetograph.Scene, root *kinetograph.Node, ch *Channels) error {
	dolly, err := ch.Get("word.dolly")
	if err != nil {
		return err
	}
	node, err := root.Prismatic(wordCameraTarget.Sub(wordCameraPosition), dolly)
	if err != nil {
		return err
	}
	return scene.SetCamera(node, kinetograph.Camera{
		Position: wordCameraPosition,
		Target:   wordCameraTarget,
		Up:       r3.NewVec(0, 0, 1),
		FOV:      kinetograph.Constant(units.Degrees(30)),
	})
}

// heroPlate is a 302×168 mm slab 12 mm deep, symmetric about the XZ plane,
// shelled through its camera-facing cap to a 1.8 mm wall.
func heroPlate(ctx context.Context) (*decad.Body, error) {
	w := sketch.NewWorld()
	s, profile, err := sketchLoops(ctx, w, w.XZ(), rectangle(-151, -84, 151, 84))
	if err != nil {
		return nil, err
	}
	slab, err := decad.New().Extrude(s, profile, decad.Symmetric{D: units.Millimeters(plateDepth)})
	if err != nil {
		return nil, err
	}
	// Along on an XZ sketch is -Y, so Facing(0, -1, 0) is the cap nearest the
	// camera.
	shelled, err := slab.Shell(ctx, decad.Faces(decad.Facing(r3.NewVec(0, -1, 0))), units.Millimeters(plateShellThickness))
	if err != nil {
		return nil, fmt.Errorf("shell the plate: %w", err)
	}
	return shelled, nil
}

// decadLetters lays out D, E, C, A, D left to right on the XZ plane, each
// 45 mm wide and 120 mm tall with 9 mm strokes and 9 mm gaps. A is three
// shapes: two legs and the bar.
func decadLetters() []letter {
	const (
		width  = 45.0
		height = 120.0
		stroke = 9.0
		gap    = 9.0
	)
	x := -130.0
	placeLoops := func(loops [][]point) [][]point {
		placed := make([][]point, len(loops))
		for i, loop := range loops {
			placed[i] = make([]point, len(loop))
			for j, p := range loop {
				placed[i][j] = point{x: p.x + x, y: p.y}
			}
		}
		return placed
	}
	place := func(loops ...[]point) [][]point {
		placed := placeLoops(loops)
		x += width + gap
		return placed
	}
	placeShapes := func(shapes ...[][]point) [][][]point {
		placed := make([][][]point, len(shapes))
		for i, shape := range shapes {
			placed[i] = placeLoops(shape)
		}
		x += width + gap
		return placed
	}

	dOuter := []point{
		{0, -height / 2}, {width - stroke, -height / 2}, {width, -height/2 + stroke},
		{width, height/2 - stroke}, {width - stroke, height / 2}, {0, height / 2},
	}
	dInner := []point{
		{stroke, -height/2 + stroke}, {stroke, height/2 - stroke},
		{width - 2*stroke, height/2 - stroke}, {width - 2*stroke, -height/2 + stroke},
	}
	e := []point{
		{0, -height / 2}, {width, -height / 2}, {width, -height/2 + stroke},
		{stroke, -height/2 + stroke}, {stroke, -stroke / 2}, {width - stroke, -stroke / 2},
		{width - stroke, stroke / 2}, {stroke, stroke / 2}, {stroke, height/2 - stroke},
		{width, height/2 - stroke}, {width, height / 2}, {0, height / 2},
	}
	c := []point{
		{width, height / 2}, {stroke, height / 2}, {0, height/2 - stroke},
		{0, -height/2 + stroke}, {stroke, -height / 2}, {width, -height / 2},
		{width, -height/2 + stroke}, {2 * stroke, -height/2 + stroke},
		{stroke, -height/2 + 2*stroke}, {stroke, height/2 - 2*stroke},
		{2 * stroke, height/2 - stroke}, {width, height/2 - stroke},
	}
	aLeft := []point{{0, -height / 2}, {stroke, -height / 2}, {width/2 + stroke/2, height / 2}, {width/2 - stroke/2, height / 2}}
	aRight := []point{{width - stroke, -height / 2}, {width, -height / 2}, {width/2 + stroke/2, height / 2}, {width/2 - stroke/2, height / 2}}
	aBar := []point{{stroke, -stroke / 2}, {width - stroke, -stroke / 2}, {width - stroke, stroke / 2}, {stroke, stroke / 2}}

	return []letter{
		{color: cyan, shapes: [][][]point{place(dOuter, dInner)}},
		{color: blue, shapes: [][][]point{place(e)}, chamferCap: true},
		{color: violet, shapes: [][][]point{place(c)}, chamferCap: true},
		{color: coral, shapes: placeShapes([][]point{aLeft}, [][]point{aRight}, [][]point{aBar})},
		{color: gold, shapes: [][][]point{place(dOuter, dInner)}},
	}
}

// xzPrism extrudes the region loops bound on the XZ plane by extent.
func xzPrism(ctx context.Context, loops [][]point, extent decad.Extent) (*decad.Body, error) {
	w := sketch.NewWorld()
	s, profile, err := sketchLoops(ctx, w, w.XZ(), loops...)
	if err != nil {
		return nil, err
	}
	return decad.New().Extrude(s, profile, extent)
}

// letterBody extrudes one letter shape 14 mm toward the camera, then either
// bevels its front cap loop (chamferCap) or fillets its convex edges along Y.
func letterBody(ctx context.Context, loops [][]point, chamferCap bool) (*decad.Body, error) {
	extruded, err := xzPrism(ctx, loops, decad.Distance{D: units.Millimeters(letterDepth), Dir: decad.Along})
	if err != nil {
		return nil, err
	}
	if chamferCap {
		beveled, err := extruded.Chamfer(ctx, decad.Edges(decad.CreatedBy(decad.CapEnd(extruded))),
			units.Millimeters(letterCapChamfer))
		if err != nil {
			return nil, fmt.Errorf("chamfer the letter cap: %w", err)
		}
		return beveled, nil
	}
	filleted, err := extruded.Fillet(ctx, decad.Edges(decad.ParallelTo(r3.NewVec(0, 1, 0)), decad.Convex()),
		units.Millimeters(letterFilletRadius))
	if err != nil {
		return nil, fmt.Errorf("fillet the letter: %w", err)
	}
	return filleted, nil
}

// heroStud revolves a quarter disc about an axis along Y through plate-local
// (x, z) into a dome standing on the plate's open front.
func heroStud(ctx context.Context, x, z, radius float64) (*decad.Body, error) {
	w := sketch.NewWorld()
	plane, err := w.CreateOffsetPlane(w.YZ(), x)
	if err != nil {
		return nil, err
	}
	s, err := w.CreateSketch(plane)
	if err != nil {
		return nil, err
	}
	const base = -plateHalfDepth
	center := s.CreatePoint(base, z)
	s.Fix(center)
	rim := s.CreatePoint(base, z+radius)
	tip := s.CreatePoint(base-radius, z)
	s.CreateLine(center, rim)
	s.CreateArc(center, rim, tip)
	s.CreateLine(tip, center)
	if _, err := s.Solve(ctx); err != nil {
		return nil, err
	}
	profile, err := validProfile(s)
	if err != nil {
		return nil, err
	}
	axis := decad.SketchLine{Start: decad.Point2{U: 0, V: z}, End: decad.Point2{U: 1, V: z}}
	dome, err := decad.New().Revolve(s, profile, axis, decad.FullRevolution{})
	if err != nil {
		return nil, fmt.Errorf("revolve the dome: %w", err)
	}
	return dome, nil
}
