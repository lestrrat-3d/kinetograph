package main

import (
	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/solidlens"
	"github.com/lestrrat-3d/units"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

// The palette and the fixed lights are decad's _gallery ones, as _clips/demo
// uses them.
var (
	backgroundColor = solidlens.RGB(0.82, 0.86, 0.93)
	blue            = solidlens.RGB(0.18, 0.47, 1)
	violet          = solidlens.RGB(0.58, 0.24, 1)
	coral           = solidlens.RGB(1, 0.25, 0.2)
	gold            = solidlens.RGB(1, 0.68, 0.08)
	edgeColor       = solidlens.RGB(0.08, 0.08, 0.12)
)

// fixedLights returns decad's _gallery two directional lights, the white key
// light at intensity key and the blue fill light at intensity fill.
func fixedLights(key, fill float64) []solidlens.DirectionalLight {
	return []solidlens.DirectionalLight{
		{
			Direction: solidlens.Vec{X: -0.7, Y: 0.12, Z: -1},
			Color:     solidlens.RGB(1, 1, 1),
			Intensity: key,
		},
		{
			Direction: solidlens.Vec{X: 0.6, Y: -0.2, Z: -0.6},
			Color:     solidlens.RGB(0.25, 0.55, 1),
			Intensity: fill,
		},
	}
}

// look is a matte material in c with dark edge lines.
func look(c solidlens.Color) render.Appearance {
	return render.Appearance{Material: solidlens.Matte(c), Edges: solidlens.Outline(edgeColor)}
}

// baseStyle is the style every shot starts from: the background, the fixed
// lights at decad's _gallery intensities, violet parts, and a 0.05 mm chord.
// The caller sets Width and Height.
func baseStyle() render.Style {
	return render.Style{
		Chord:             units.Millimeters(0.05),
		Background:        backgroundColor,
		Default:           look(violet),
		Parts:             map[string]render.Appearance{},
		DirectionalLights: fixedLights(1.25, 0.4),
	}
}

// tableCamera is the camera every table shot except the orbit holds still:
// decad's _gallery feature camera, so the parts are drawn at one scale from
// one viewpoint.
func tableCamera() kinetograph.Camera {
	return kinetograph.Camera{
		Position: r3.NewVec(105, -165, 110),
		Target:   r3.NewVec(0, 0, 12),
		Up:       r3.NewVec(0, 0, 1),
		FOV:      kinetograph.Constant(units.Degrees(30)),
	}
}
