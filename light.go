package kinetograph

import (
	"fmt"
	"math"

	"github.com/lestrrat-3d/r3"
)

// LightKind says how a Light's node-local vectors are read.
type LightKind int

const (
	// PointLight shines outward in every direction from Light.Position.
	PointLight LightKind = iota + 1
	// DirectionalLight shines with parallel rays along Light.Direction, as
	// from infinitely far away.
	DirectionalLight
)

// Light is a light source in its node's frame. It says where the light is and
// which way it shines. Its color and its intensity over time are
// render.Style's, bound by the name the light is added under.
type Light struct {
	Kind      LightKind
	Position  r3.Vec // PointLight: the light's position. The zero Vec for a DirectionalLight.
	Direction r3.Vec // DirectionalLight: the way the light travels, toward the scene. The zero Vec for a PointLight.
}

// LightPose is a light at one time, in world coordinates. A PointLight's
// Direction and a DirectionalLight's Position are the zero Vec; a
// DirectionalLight's Direction has unit length.
type LightPose struct {
	Name      string
	Kind      LightKind
	Position  r3.Vec
	Direction r3.Vec
}

// sceneLight is one attached light. For a DirectionalLight, light.Direction
// holds the unit direction AddLight computed.
type sceneLight struct {
	name  string
	node  *Node
	light Light
}

// normalizeLight checks light as AddLight documents and returns it with a
// DirectionalLight's direction made unit length.
func normalizeLight(name string, light Light) (Light, error) {
	switch light.Kind {
	case PointLight:
		if light.Direction != (r3.Vec{}) {
			return Light{}, fmt.Errorf("%w: point light %q has a Direction", ErrInvalidLight, name)
		}
		if !finiteVec(light.Position) {
			return Light{}, fmt.Errorf("%w: point light %q has a non-finite Position", ErrInvalidLight, name)
		}
		return light, nil
	case DirectionalLight:
		if light.Position != (r3.Vec{}) {
			return Light{}, fmt.Errorf("%w: directional light %q has a Position", ErrInvalidLight, name)
		}
		unit, ok := light.Direction.Normalize()
		if !ok {
			return Light{}, fmt.Errorf("%w: directional light %q", ErrDegenerateDirection, name)
		}
		light.Direction = unit
		return light, nil
	default:
		return Light{}, fmt.Errorf("%w: light %q has kind %d", ErrInvalidLight, name, int(light.Kind))
	}
}

// finiteVec reports whether every component of v is finite.
func finiteVec(v r3.Vec) bool {
	return finite(v.X) && finite(v.Y) && finite(v.Z)
}

func finite(x float64) bool { return !math.IsNaN(x) && !math.IsInf(x, 0) }

// pose maps the light's node-local vectors through the node's world
// transform: Apply for a position, ApplyDir for a direction, as the camera's
// are.
func (l sceneLight) pose(world r3.Transform) LightPose {
	pose := LightPose{Name: l.name, Kind: l.light.Kind}
	if l.light.Kind == PointLight {
		pose.Position = world.Apply(l.light.Position)
		return pose
	}
	pose.Direction = world.ApplyDir(l.light.Direction)
	return pose
}
