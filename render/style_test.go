package render_test

import (
	"testing"
	"time"

	"github.com/lestrrat-3d/units"
	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

func TestNewRefusesBadStyles(t *testing.T) {
	rig := kinetograph.NewRig()
	scene := oneBlockScene(t, rig, rig.Root(), block(t, -5))
	clip := newClip(t, scene, 24, time.Second)

	unknownPart := baseStyle()
	unknownPart.Parts = map[string]render.Appearance{"nothing": {}}
	zeroWidth := baseStyle()
	zeroWidth.Width = 0
	negativeHeight := baseStyle()
	negativeHeight.Height = -1
	angleChord := baseStyle()
	angleChord.Chord = units.Degrees(1)

	for _, tc := range []struct {
		name  string
		style render.Style
		want  error
	}{
		{"unknown part", unknownPart, render.ErrStyle},
		{"zero width", zeroWidth, render.ErrStyle},
		{"negative height", negativeHeight, render.ErrStyle},
		{"angle chord", angleChord, kinetograph.ErrKind},
	} {
		t.Run(tc.name, func(t *testing.T) {
			r, err := render.New(t.Context(), clip, tc.style)
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, r)
		})
	}
}

func TestNewPassesDecadChordErrorsThroughWithThePartName(t *testing.T) {
	rig := kinetograph.NewRig()
	scene := oneBlockScene(t, rig, rig.Root(), block(t, -5))
	clip := newClip(t, scene, 24, time.Second)
	style := baseStyle()
	style.Chord = units.Millimeters(0)
	_, err := render.New(t.Context(), clip, style)
	require.Error(t, err)
	require.ErrorContains(t, err, `"block"`)
}

func TestNewNilContext(t *testing.T) {
	rig := kinetograph.NewRig()
	scene := oneBlockScene(t, rig, rig.Root(), block(t, -5))
	//nolint:staticcheck // a nil context is the case under test
	_, err := render.New(nil, newClip(t, scene, 24, time.Second), baseStyle())
	require.ErrorIs(t, err, kinetograph.ErrNilContext)
}
