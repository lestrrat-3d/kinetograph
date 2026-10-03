package kinetograph_test

import (
	"testing"

	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/units"
	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph"
)

func TestCameraOrbitHalfTurn(t *testing.T) {
	// The axis passes through the target (5, 5, 0), so a half turn mirrors the
	// camera through the target in XY, keeps its height and leaves the target
	// where it was. Up is a direction: a rotation about Z leaves it alone.
	target := r3.Vec{X: 5, Y: 5}
	rig := kinetograph.NewRig()
	orbit, err := rig.Root().Revolute(target, r3.Vec{Z: 1}, kinetograph.Constant(units.Degrees(180)))
	require.NoError(t, err)

	scene := kinetograph.NewScene(rig)
	require.NoError(t, scene.AddPart("block", rig.Root(), newBlock(t)))
	require.NoError(t, scene.SetCamera(orbit, kinetograph.Camera{
		Position: r3.Vec{X: 15, Y: 5, Z: 3},
		Target:   target,
		Up:       r3.Vec{Z: 1},
		FOV:      kinetograph.Constant(units.Degrees(35)),
	}))

	f, err := scene.At(t.Context(), 0)
	require.NoError(t, err)
	require.True(t, f.Camera.Position.Equal(r3.Vec{X: -5, Y: 5, Z: 3}, 1e-12), "position %v", f.Camera.Position)
	require.True(t, f.Camera.Target.Equal(target, 1e-12), "target %v", f.Camera.Target)
	require.True(t, f.Camera.Up.Equal(r3.Vec{Z: 1}, 1e-12), "up %v", f.Camera.Up)
	require.True(t, f.Camera.FOV.Equal(units.Degrees(35), 0))
}

func TestCameraUpIsADirection(t *testing.T) {
	// A translation moves the camera's points but never its up direction.
	rig := kinetograph.NewRig()
	slide, err := rig.Root().Prismatic(r3.Vec{X: 1}, kinetograph.Constant(units.Millimeters(7)))
	require.NoError(t, err)
	scene := kinetograph.NewScene(rig)
	require.NoError(t, scene.AddPart("block", rig.Root(), newBlock(t)))
	require.NoError(t, scene.SetCamera(slide, kinetograph.Camera{
		Position: r3.Vec{Y: -50},
		Target:   r3.Vec{},
		Up:       r3.Vec{Z: 1},
		FOV:      kinetograph.Constant(units.Degrees(35)),
	}))
	f, err := scene.At(t.Context(), 0)
	require.NoError(t, err)
	require.Equal(t, r3.Vec{X: 7, Y: -50}, f.Camera.Position)
	require.Equal(t, r3.Vec{X: 7}, f.Camera.Target)
	require.Equal(t, r3.Vec{Z: 1}, f.Camera.Up)
}
