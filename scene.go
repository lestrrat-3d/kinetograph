package kinetograph

import (
	"context"
	"fmt"
	"time"

	"github.com/lestrrat-3d/decad"
	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/units"
)

type part struct {
	name string
	node *Node
	body *decad.Body
}

// Scene is the set of parts and the camera, each attached to a node of one
// rig. Build it completely before evaluating it; At is safe to call
// concurrently, AddPart and SetCamera are not safe beside At.
type Scene struct {
	rig        *Rig
	parts      []part
	names      map[string]struct{} // lookup only; never iterated
	camera     Camera
	cameraNode *Node
}

// NewScene returns an empty scene over rig.
func NewScene(rig *Rig) *Scene {
	return &Scene{rig: rig, names: map[string]struct{}{}}
}

// AddPart attaches body to node under name. It returns ErrNilBody for a nil
// body, ErrForeignNode for a node of another rig, and ErrDuplicateName for a
// name already added.
func (s *Scene) AddPart(name string, node *Node, body *decad.Body) error {
	if body == nil {
		return fmt.Errorf("%w: part %q", ErrNilBody, name)
	}
	if node == nil || node.rig != s.rig {
		return fmt.Errorf("%w: part %q", ErrForeignNode, name)
	}
	if _, dup := s.names[name]; dup {
		return fmt.Errorf("%w: %q", ErrDuplicateName, name)
	}
	s.names[name] = struct{}{}
	s.parts = append(s.parts, part{name: name, node: node, body: body})
	return nil
}

// SetCamera attaches cam to node, replacing any earlier camera. It returns
// ErrForeignNode for a node of another rig, ErrNilChannel for a nil FOV, and
// ErrKind unless cam.FOV.Kind() == units.Angle.
func (s *Scene) SetCamera(node *Node, cam Camera) error {
	if node == nil || node.rig != s.rig {
		return fmt.Errorf("%w: camera", ErrForeignNode)
	}
	if cam.FOV == nil {
		return fmt.Errorf("%w: camera FOV", ErrNilChannel)
	}
	if cam.FOV.Kind() != units.Angle {
		return fmt.Errorf("%w: camera FOV is %s, want %s", ErrKind, cam.FOV.Kind(), units.Angle)
	}
	s.camera = cam
	s.cameraNode = node
	return nil
}

// validate reports the first reason the scene cannot be evaluated.
func (s *Scene) validate() error {
	if s.cameraNode == nil {
		return ErrNoCamera
	}
	if len(s.parts) == 0 {
		return ErrEmptyScene
	}
	return nil
}

// Pose is one part at one time.
type Pose struct {
	Name      string
	Body      *decad.Body
	Transform r3.Transform // part frame -> world
}

// Frame is the scene evaluated at one time. Poses are in AddPart order.
// Index is -1 from Scene.At; Clip.Frame fills it in.
type Frame struct {
	Index  int
	Time   time.Duration
	Poses  []Pose
	Camera CameraPose
}

// At evaluates the scene at t. ctx is checked once on entry and returned as
// ctx.Err() when done; it is threaded through so a later pass that rebuilds
// bodies can cancel. It returns ErrNilContext for a nil ctx, ErrNoCamera when
// SetCamera was never called and ErrEmptyScene when no part was added.
func (s *Scene) At(ctx context.Context, t time.Duration) (*Frame, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := s.validate(); err != nil {
		return nil, err
	}
	poses := make([]Pose, len(s.parts))
	for i, p := range s.parts {
		world, err := p.node.World(t)
		if err != nil {
			return nil, fmt.Errorf("kinetograph: part %q at %s: %w", p.name, t, err)
		}
		poses[i] = Pose{Name: p.name, Body: p.body, Transform: world}
	}
	world, err := s.cameraNode.World(t)
	if err != nil {
		return nil, fmt.Errorf("kinetograph: camera at %s: %w", t, err)
	}
	fov, err := s.camera.FOV.At(t)
	if err != nil {
		return nil, fmt.Errorf("kinetograph: camera FOV at %s: %w", t, err)
	}
	return &Frame{Index: -1, Time: t, Poses: poses, Camera: s.camera.pose(world, fov)}, nil
}
