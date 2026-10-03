// Package render turns a kinetograph.Clip into solidlens scenes and a numbered
// PNG sequence. It is the only kinetograph package that imports solidlens.
package render

import (
	"errors"
	"fmt"
	"maps"
	"slices"

	"github.com/lestrrat-3d/solidlens"
	"github.com/lestrrat-3d/units"

	"github.com/lestrrat-3d/kinetograph"
)

// ErrStyle is returned by New for a Style it cannot draw the clip with: a
// non-positive Width or Height, a Parts name that no part of the clip carries,
// a scene light with no Lights entry, a Lights name that no light carries, a
// light with the zero Color, an Intensity keyframe below 0, or a Fade keyframe
// outside [0, 1]. The message names the part or light, and "Default" for the
// default appearance.
var ErrStyle = errors.New("render: invalid style")

// Appearance is how one part is drawn. Back, when non-nil, shades the inner
// side of an open mesh (a decad sheet body). Edges is solidlens's edge-line
// style; its zero value draws no lines.
type Appearance struct {
	Material solidlens.Material
	Back     *solidlens.Material
	Edges    solidlens.Edges
	// Fade is the part's opacity over time, a Dimensionless channel whose
	// keyframes lie in [0, 1]. At 0 the part, its back side and its edge lines
	// are left out of the frame. At 1 the part is drawn exactly as when Fade is
	// nil. Between them the part is mixed over whatever is behind it, by
	// rendering the frame in layers with and without each fading part (see
	// Renderer.Frame). nil is 1 at every time.
	Fade *kinetograph.Channel
}

// LightAppearance is how one node light shines.
type LightAppearance struct {
	// Color goes to solidlens unchanged. solidlens scales the light by the
	// luminance of Color, 0.2126·R + 0.7152·G + 0.0722·B, and does not tint the
	// surface. New refuses the zero Color: its luminance is 0, so the light
	// could never add anything, and a light is dimmed through Intensity.
	Color solidlens.Color
	// Intensity is solidlens's light Intensity over time, a Dimensionless
	// channel whose keyframes are >= 0. A point light's contribution falls off
	// as 1 / max(1, d²) with d in mesh units, which are millimetres in
	// kinetograph, so a point light 100 mm from a face needs an intensity near
	// 10⁴ to light it as strongly as a directional light of intensity 1.
	Intensity *kinetograph.Channel
}

// Style is everything about a clip that is not geometry or motion.
// DirectionalLights and PointLights are fixed in the world at a constant
// intensity; Lights gives each light the scene attached with
// kinetograph.Scene.AddLight its color and intensity.
type Style struct {
	Width, Height     int
	Chord             units.Value // tessellation tolerance, a Length
	Background        solidlens.Color
	Default           Appearance
	Parts             map[string]Appearance // by part name; absent names use Default
	DirectionalLights []solidlens.DirectionalLight
	PointLights       []solidlens.PointLight
	Lights            map[string]LightAppearance // by light name; every light of the scene needs an entry
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

// validateLights checks s.Lights against the scene's lights, then each
// LightAppearance, walking names in sorted order.
func (s Style) validateLights(lights []kinetograph.LightInfo) error {
	// names is every name either side holds, once, in sorted order.
	inScene := make(map[string]struct{}, len(lights))
	names := slices.Collect(maps.Keys(s.Lights))
	for _, l := range lights {
		inScene[l.Name] = struct{}{}
		if _, ok := s.Lights[l.Name]; !ok {
			names = append(names, l.Name)
		}
	}
	slices.Sort(names)
	for _, name := range names {
		_, styled := s.Lights[name]
		if !styled {
			return fmt.Errorf("%w: light %q has no Lights entry", ErrStyle, name)
		}
		if _, ok := inScene[name]; !ok {
			return fmt.Errorf("%w: Lights names %q, which no light carries", ErrStyle, name)
		}
	}
	for _, name := range names {
		if err := s.Lights[name].validate(name); err != nil {
			return err
		}
	}
	return nil
}

// validate checks one light's appearance.
func (a LightAppearance) validate(name string) error {
	if a.Color == (solidlens.Color{}) {
		return fmt.Errorf("%w: light %q has the zero Color", ErrStyle, name)
	}
	if a.Intensity == nil {
		return fmt.Errorf("%w: light %q Intensity", kinetograph.ErrNilChannel, name)
	}
	if k := a.Intensity.Kind(); k != units.Dimensionless {
		return fmt.Errorf("%w: light %q Intensity is %s, want %s", kinetograph.ErrKind, name, k, units.Dimensionless)
	}
	for i, key := range a.Intensity.Keyframes() {
		v, err := key.Value.In(units.One)
		if err != nil {
			return fmt.Errorf("render: light %q Intensity keyframe %d: %w", name, i, err)
		}
		if v < 0 {
			return fmt.Errorf("%w: light %q Intensity keyframe %d is %s, below 0", ErrStyle, name, i, key.Value)
		}
	}
	return nil
}

// validateFades checks Default.Fade, then each Parts fade in sorted name
// order.
func (s Style) validateFades() error {
	if err := validateFade("Default", s.Default.Fade); err != nil {
		return err
	}
	for _, name := range slices.Sorted(maps.Keys(s.Parts)) {
		if err := validateFade(fmt.Sprintf("part %q", name), s.Parts[name].Fade); err != nil {
			return err
		}
	}
	return nil
}

// validateFade checks one Fade channel; who names its owner in the message.
func validateFade(who string, fade *kinetograph.Channel) error {
	if fade == nil {
		return nil
	}
	if k := fade.Kind(); k != units.Dimensionless {
		return fmt.Errorf("%w: %s Fade is %s, want %s", kinetograph.ErrKind, who, k, units.Dimensionless)
	}
	for i, key := range fade.Keyframes() {
		v, err := key.Value.In(units.One)
		if err != nil {
			return fmt.Errorf("render: %s Fade keyframe %d: %w", who, i, err)
		}
		if v < 0 || v > 1 {
			return fmt.Errorf("%w: %s Fade keyframe %d is %s, outside [0, 1]", ErrStyle, who, i, key.Value)
		}
	}
	return nil
}
