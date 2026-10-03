package kinetograph

import (
	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/units"
)

// Camera is a perspective camera in its node's frame. FOV is the vertical
// field of view, an Angle channel. The camera looks from Position toward
// Target with Up as its up direction.
type Camera struct {
	Position r3.Vec
	Target   r3.Vec
	Up       r3.Vec
	FOV      *Channel
}

// CameraPose is the camera at one time, in world coordinates.
type CameraPose struct {
	Position r3.Vec
	Target   r3.Vec
	Up       r3.Vec
	FOV      units.Value
}

// pose maps the camera's node-local vectors through the node's world
// transform: Apply for the two points, ApplyDir for the up direction.
func (c Camera) pose(world r3.Transform, fov units.Value) CameraPose {
	return CameraPose{
		Position: world.Apply(c.Position),
		Target:   world.Apply(c.Target),
		Up:       world.ApplyDir(c.Up),
		FOV:      fov,
	}
}
