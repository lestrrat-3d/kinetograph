package main

import (
	"github.com/lestrrat-3d/solidlens"
	"github.com/lestrrat-3d/units"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

// The palette is decad's _gallery/scene.go one, so the clip and decad's
// README images share their colours.
var (
	backgroundColor = solidlens.RGB(0.82, 0.86, 0.93)
	cyan            = solidlens.RGB(0.05, 0.85, 0.96)
	blue            = solidlens.RGB(0.18, 0.47, 1)
	violet          = solidlens.RGB(0.58, 0.24, 1)
	coral           = solidlens.RGB(1, 0.25, 0.2)
	gold            = solidlens.RGB(1, 0.68, 0.08)
)

// The wordmark keeps _gallery/hero.go's own colours for the parts that are
// not letters.
var (
	navy   = solidlens.RGB(0.015, 0.06, 0.18)
	orange = solidlens.RGB(1, 0.58, 0.08)
	sky    = solidlens.RGB(0.1, 0.78, 0.95)
)

// edgeColor is the outline every part is drawn with.
var edgeColor = solidlens.RGB(0.08, 0.08, 0.12)

// Chord tolerances: the demo's for the plate and the shelf, the hero's for
// the wordmark.
const (
	partChord     = 0.05
	wordmarkChord = 0.02
)

// galleryDirectionalLights are _gallery/scene.go's two directional lights.
func galleryDirectionalLights() []solidlens.DirectionalLight {
	return []solidlens.DirectionalLight{
		{Direction: solidlens.Vec{X: -0.7, Y: 0.12, Z: -1}, Color: solidlens.RGB(1, 1, 1), Intensity: 1.25},
		{Direction: solidlens.Vec{X: 0.6, Y: -0.2, Z: -0.6}, Color: solidlens.RGB(0.25, 0.55, 1), Intensity: 0.4},
	}
}

// galleryPointLights is _gallery/scene.go's point light.
func galleryPointLights() []solidlens.PointLight {
	return []solidlens.PointLight{
		{Position: solidlens.Vec{X: -140, Y: -190, Z: 240}, Color: solidlens.RGB(0.45, 0.75, 1), Intensity: 3000},
	}
}

// matte is a part drawn in color with the clip's outline.
func matte(color solidlens.Color) render.Appearance {
	return render.Appearance{Material: solidlens.Matte(color), Edges: solidlens.Outline(edgeColor)}
}

// whiteLamp is a moving light's look: white, at intensity over time.
// solidlens reads a light colour as its luminance only, so a coloured lamp
// would light the parts exactly as a dimmer white one does.
func whiteLamp(intensity *kinetograph.Channel) render.LightAppearance {
	return render.LightAppearance{Color: solidlens.RGB(1, 1, 1), Intensity: intensity}
}

// buildStyle is act A's look: the gallery's background, lights and chord,
// the plate violet and the tools and pin gold. lamps are its moving lights.
func buildStyle(parts map[string]render.Appearance, lamps map[string]render.LightAppearance) render.Style {
	return render.Style{
		Chord:             units.Millimeters(partChord),
		Background:        backgroundColor,
		Default:           matte(violet),
		Parts:             parts,
		DirectionalLights: galleryDirectionalLights(),
		PointLights:       galleryPointLights(),
		Lights:            lamps,
	}
}

// shapesStyle is act B's look. It leaves out the gallery's point light: at
// the shelf's distances it adds under 0.03 to any face, and only to the parts
// nearest it (docs/design.md §9, pass 4).
func shapesStyle(parts map[string]render.Appearance) render.Style {
	return render.Style{
		Chord:             units.Millimeters(partChord),
		Background:        backgroundColor,
		Default:           matte(blue),
		Parts:             parts,
		DirectionalLights: galleryDirectionalLights(),
	}
}

// wordmarkStyle is act C's look: _gallery/hero.go's lights and chord, plus
// lamps, its moving lights.
func wordmarkStyle(parts map[string]render.Appearance, lamps map[string]render.LightAppearance) render.Style {
	return render.Style{
		Chord:      units.Millimeters(wordmarkChord),
		Background: backgroundColor,
		Default:    matte(navy),
		Parts:      parts,
		DirectionalLights: []solidlens.DirectionalLight{
			{Direction: solidlens.Vec{X: -0.7, Y: 0.35, Z: -1}, Color: solidlens.RGB(1, 1, 1), Intensity: 1.25},
			{Direction: solidlens.Vec{X: 0.6, Y: -0.2, Z: -0.6}, Color: solidlens.RGB(0.25, 0.55, 1), Intensity: 0.4},
		},
		PointLights: []solidlens.PointLight{
			{Position: solidlens.Vec{X: -90, Y: -135, Z: 160}, Color: solidlens.RGB(0.45, 0.75, 1), Intensity: 1800},
		},
		Lights: lamps,
	}
}
