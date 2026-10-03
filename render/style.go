// Package render turns a kinetograph.Clip into solidlens scenes and a numbered
// PNG sequence. It is the only kinetograph package that imports solidlens.
package render

import (
	"errors"
	"fmt"

	"github.com/lestrrat-3d/solidlens"
	"github.com/lestrrat-3d/units"

	"github.com/lestrrat-3d/kinetograph"
)

// ErrStyle is returned by New for a Style with a non-positive Width or
// Height, or a Parts name that no part of the clip carries.
var ErrStyle = errors.New("render: invalid style")

// Appearance is how one part is drawn. Back, when non-nil, shades the inner
// side of an open mesh (a decad sheet body). Edges is solidlens's edge-line
// style; its zero value draws no lines.
type Appearance struct {
	Material solidlens.Material
	Back     *solidlens.Material
	Edges    solidlens.Edges
}

// Style is everything about a clip that is not geometry or motion.
type Style struct {
	Width, Height     int
	Chord             units.Value // tessellation tolerance, a Length
	Background        solidlens.Color
	Default           Appearance
	Parts             map[string]Appearance // by part name; absent names use Default
	DirectionalLights []solidlens.DirectionalLight
	PointLights       []solidlens.PointLight
}

// validate checks everything about s that does not depend on the clip.
func (s Style) validate() error {
	if s.Width <= 0 || s.Height <= 0 {
		return fmt.Errorf("%w: image size %dx%d is not positive", ErrStyle, s.Width, s.Height)
	}
	if s.Chord.Kind() != units.Length {
		return fmt.Errorf("%w: chord is %s, want %s", kinetograph.ErrKind, s.Chord.Kind(), units.Length)
	}
	return nil
}
