package main

import (
	"math"
	"time"

	"github.com/lestrrat-3d/units"

	"github.com/lestrrat-3d/kinetograph"
)

// ms is n milliseconds on the global clock.
func ms(n int) time.Duration {
	return time.Duration(n) * time.Millisecond
}

// afterStep is the earliest Start a move may have when it follows a step at
// t on the same track: a step lasts 1ns.
func afterStep(t time.Duration) time.Duration {
	return t + time.Nanosecond
}

// drill is one hole of the flange and the tool that drills it.
type drill struct {
	name   string  // the hole's name in its tracks: hole.<name>, tool.<name>.plunge, tool.<name>.fade
	x      float64 // the hole's centre on the plate's X axis
	radius float64 // the hole's radius
	start  int     // the tool's approach start, in ms on the global clock
}

// drills are the flange's three holes in the order the tools land. Each tool
// starts drillStagger after the one before it.
var drills = []drill{
	{name: "bore", x: 0, radius: boreRadius, start: 2600},
	{name: "left", x: -boltOffset, radius: boltRadius, start: 2600 + drillStagger},
	{name: "right", x: boltOffset, radius: boltRadius, start: 2600 + 2*drillStagger},
}

// A tool's plunge has two moves. It approaches for approachLength ms
// (EaseOut) and stops with its tip drillGap millimetres above the plate's top
// face, then drills at a constant speed for drillLength ms until it has
// landed: 36 mm in 0.45 s, so the 16 mm plate takes 0.2 s, six frames at
// 30 fps, and the hole visibly deepens inside the see-through tool. The
// tools start drillStagger apart, so a hole is blind (depth 0.5 to 15.5 mm)
// from 56 to 244 ms into its drill and no two holes are blind in one frame:
// decad refuses a Cut that follows a blind one. The tools retract together
// from 4.1 to 5.0 s, to toolPark millimetres past their start: the bottom of a
// parked tool is then at z = 264 mm, above the act A camera at its highest
// (about 230 mm, at the end of the tilt), so the tilted camera never sees a
// tool.
const (
	approachLength = 500
	drillLength    = 450
	drillStagger   = 250
	drillGap       = 4.0
	retractStart   = 4100
	retractEnd     = 5000
	toolPark       = 120.0
)

// toolContact is the plunge at which a tool's tip reaches the plate's top
// face: the tip starts at toolLandedBottom + toolTravel = 144 mm.
const toolContact = toolLandedBottom + toolTravel - plateThickness

// letterStarts are the times, in ms on the global clock, each wordmark letter
// starts its 0.9 s descent.
var letterStarts = []int{19800, 19950, 20100, 20250, 20400}

// letterTravel is the length of a letter's path, from (0, -80, 200) off its
// landed pose to the pose itself.
var letterTravel = math.Hypot(letterOffsetY, letterOffsetZ)

// landingScript is every track and move of the clip. It is the only place
// motion timing is written.
func landingScript() Script {
	var s Script
	track := func(name string, initial units.Value) {
		s.Tracks = append(s.Tracks, Track{Name: name, Initial: initial})
	}
	move := func(name string, start, end time.Duration, to units.Value, ease kinetograph.Easing) {
		s.Moves = append(s.Moves, Move{Track: name, Start: start, End: end, To: to, Ease: ease})
	}
	mm := units.Millimeters
	deg := units.Degrees

	// Act A: the camera orbits the whole act, narrows for the chamfer and
	// tilts up over the pin.
	track("cam.orbit", deg(0))
	move("cam.orbit", 0, ms(13000), deg(130), kinetograph.Linear)
	track("cam.tilt", deg(0))
	move("cam.tilt", ms(11000), ms(12800), deg(58), kinetograph.EaseInOut)
	track("cam.fov", deg(30))
	move("cam.fov", ms(8000), ms(10000), deg(24), kinetograph.EaseInOut)

	// A1: the flange grows from a 2 mm slab to its 16 mm height.
	track("flange.height", mm(2))
	move("flange.height", ms(300), ms(1500), mm(plateThickness), kinetograph.EaseOut)

	// A2: each tool plunges, holds in its hole and retracts. Each hole's
	// track copies its tool's plunge and holds once the tool has landed. A
	// tool is hidden until its plunge starts, out of view, then drawn at
	// toolFade so the hole shows growing inside it, and fades out as it
	// retracts.
	for _, d := range drills {
		plunge := "tool." + d.name + ".plunge"
		fade := "tool." + d.name + ".fade"
		hole := "hole." + d.name
		track(plunge, mm(0))
		track(fade, units.Scalar(0))
		track(hole, mm(0))
		start := ms(d.start)
		contact := ms(d.start + approachLength)
		end := ms(d.start + approachLength + drillLength)
		for _, t := range []string{plunge, hole} {
			move(t, start, contact, mm(toolContact-drillGap), kinetograph.EaseOut)
			move(t, contact, end, mm(toolTravel), kinetograph.Linear)
		}
		move(fade, start, start, units.Scalar(toolFade), nil)
		move(plunge, ms(retractStart), ms(retractEnd), mm(-toolPark), kinetograph.EaseIn)
		move(fade, ms(retractStart), ms(retractEnd), units.Scalar(0), kinetograph.Linear)
	}

	// A3 and A4: each feature steps from absent to decad's smallest accepted
	// value, then grows to its target.
	track("flange.fillet", mm(0))
	move("flange.fillet", ms(5800), ms(5800), mm(minFillet), nil)
	move("flange.fillet", afterStep(ms(5800)), ms(7300), mm(filletRadius), kinetograph.EaseInOut)
	track("flange.chamfer", mm(0))
	move("flange.chamfer", ms(8200), ms(8200), mm(minChamfer), nil)
	move("flange.chamfer", afterStep(ms(8200)), ms(9200), mm(chamferSetback), nil)

	// A3: a white point light circles the plate past the round nearest the
	// camera, brightening on the way in and dimming on the way out.
	track("lamp.fillet.turn", deg(filletLampFrom))
	move("lamp.fillet.turn", ms(6800), ms(8000), deg(filletLampTo), kinetograph.Linear)
	track("lamp.fillet.intensity", units.Scalar(0))
	move("lamp.fillet.intensity", ms(6800), ms(7100), units.Scalar(filletLampIntensity), kinetograph.EaseOut)
	move("lamp.fillet.intensity", ms(7700), ms(8000), units.Scalar(0), kinetograph.EaseIn)

	// A5: the pin drops into the bore.
	track("pin.drop", mm(0))
	move("pin.drop", ms(10200), ms(11600), mm(pinDrop), kinetograph.EaseOut)

	// Act B: the camera dollies along the shelf while every part turns once.
	// It starts 87.5 mm short of the first part and ends 87.5 mm past the
	// last, so it centres part k at 13.25 + 1.2·k s.
	lastSlot := shelfPitch * float64(len(shelf)-1)
	track("shapes.dolly", mm(-dollyMargin))
	move("shapes.dolly", ms(12500), ms(20000), mm(lastSlot+dollyMargin), kinetograph.Linear)
	for _, slot := range shelf {
		spin := "shape." + slot.name + ".spin"
		track(spin, deg(0))
		move(spin, ms(12500), ms(20000), deg(360), kinetograph.Linear)
	}

	// Act C: the letters drop in one after another and the camera creeps in
	// from 30 mm behind hero.go's camera to hero.go's camera itself. Any
	// closer and the plate's sides leave the frame.
	for i, start := range letterStarts {
		in := letterTrack(i)
		track(in, mm(0))
		move(in, ms(start), ms(start+900), mm(letterTravel), kinetograph.EaseOut)
	}
	track("word.dolly", mm(-30))
	move("word.dolly", ms(19500), ms(24000), mm(0), kinetograph.Linear)

	// Act C: once the name has landed, a white point light slides left to
	// right in front of the letters.
	track("lamp.word.slide", mm(0))
	move("lamp.word.slide", ms(22000), ms(23500), mm(wordLampTravel), kinetograph.Linear)
	track("lamp.word.intensity", units.Scalar(0))
	move("lamp.word.intensity", ms(22000), ms(22300), units.Scalar(wordLampIntensity), kinetograph.EaseOut)
	move("lamp.word.intensity", ms(23200), ms(23500), units.Scalar(0), kinetograph.EaseIn)
	return s
}
