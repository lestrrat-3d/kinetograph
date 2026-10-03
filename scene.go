package kinetograph

import (
	"context"
	"fmt"
	"time"

	"github.com/lestrrat-3d/decad"
	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/units"
)

// part is one attached part: body for AddPart, param for AddParametric.
type part struct {
	name  string
	node  *Node
	body  *decad.Body
	param *parametric
}

// Scene is the set of parts and the camera, each attached to a node of one
// rig. Build it completely before evaluating it; At and AtCached are safe to
// call concurrently, AddPart, AddParametric and SetCamera are not safe beside
// them.
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

// PartInfo is one part as AddPart or AddParametric attached it.
type PartInfo struct {
	Name       string
	Body       *decad.Body // the body AddPart attached; nil for a parametric part
	Parametric bool
}

// Parts returns the scene's parts in the order they were added. It evaluates
// nothing and calls no Builder.
func (s *Scene) Parts() []PartInfo {
	infos := make([]PartInfo, len(s.parts))
	for i, p := range s.parts {
		infos[i] = PartInfo{Name: p.name, Body: p.body, Parametric: p.param != nil}
	}
	return infos
}

// Pose is one part at one time. For a parametric part, Body is the body built
// for that time and Params a new map holding the values it was built from;
// Params is nil for a part AddPart attached.
type Pose struct {
	Name      string
	Body      *decad.Body
	Transform r3.Transform // part frame -> world
	Params    Params
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
// ctx.Err() when done, and is handed to every Builder. It returns
// ErrNilContext for a nil ctx, ErrNoCamera when SetCamera was never called and
// ErrEmptyScene when no part was added. At is AtCached with a new BuildCache,
// so each call builds every parametric part once.
func (s *Scene) At(ctx context.Context, t time.Duration) (*Frame, error) {
	return s.AtCached(ctx, t, NewBuildCache())
}

// AtCached evaluates the scene at t as At does, taking each parametric part's
// body from cache and building it there on a miss. A failure to evaluate a
// part names the part and the time and wraps the cause: a channel error, a
// parameter value with no text form, the Builder's own error, or ErrNilBody
// when Build returns a nil body and a nil error. When ctx is done after a
// failure, AtCached returns ctx.Err() unwrapped instead. cache MUST NOT be nil.
func (s *Scene) AtCached(ctx context.Context, t time.Duration, cache *BuildCache) (*Frame, error) {
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
		pose, err := p.pose(ctx, cache, t)
		if err != nil {
			return nil, s.failure(ctx, fmt.Errorf("kinetograph: part %q at %s: %w", p.name, t, err))
		}
		poses[i] = pose
	}
	world, err := s.cameraNode.World(t)
	if err != nil {
		return nil, s.failure(ctx, fmt.Errorf("kinetograph: camera at %s: %w", t, err))
	}
	fov, err := s.camera.FOV.At(t)
	if err != nil {
		return nil, s.failure(ctx, fmt.Errorf("kinetograph: camera FOV at %s: %w", t, err))
	}
	return &Frame{Index: -1, Time: t, Poses: poses, Camera: s.camera.pose(world, fov)}, nil
}

// failure returns ctx.Err() when ctx is done, and err otherwise.
func (s *Scene) failure(ctx context.Context, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	return err
}

// pose evaluates p at t, building its body through cache when p is
// parametric.
func (p part) pose(ctx context.Context, cache *BuildCache, t time.Duration) (Pose, error) {
	world, err := p.node.World(t)
	if err != nil {
		return Pose{}, err
	}
	if p.param == nil {
		return Pose{Name: p.name, Body: p.body, Transform: world}, nil
	}
	body, params, err := p.param.body(ctx, cache, t)
	if err != nil {
		return Pose{}, err
	}
	return Pose{Name: p.name, Body: body, Transform: world, Params: params}, nil
}
