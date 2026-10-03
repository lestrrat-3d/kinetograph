package kinetograph

import (
	"fmt"
	"time"

	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/units"
)

// Rig is a tree of joints. The root node is the world frame.
type Rig struct {
	root *Node
}

// NewRig returns a rig holding only its root node.
func NewRig() *Rig {
	r := &Rig{}
	r.root = &Node{rig: r, kind: jointRoot}
	return r
}

// Root returns the world frame node.
func (r *Rig) Root() *Node { return r.root }

type jointKind int

const (
	jointRoot jointKind = iota
	jointFixed
	jointRevolute
	jointPrismatic
)

// Node is one joint. A node is created only through its parent, so the tree
// has no cycles. A Node is immutable once created.
type Node struct {
	rig    *Rig
	parent *Node
	kind   jointKind

	fixed   r3.Transform // jointFixed
	center  r3.Vec       // jointRevolute
	axis    r3.Vec       // jointRevolute
	dir     r3.Vec       // jointPrismatic, unit length
	channel *Channel     // jointRevolute (Angle), jointPrismatic (Length)
}

// Fixed adds a child whose local transform is the constant t. It returns
// ErrInvalidTransform when !t.IsValid() (the zero Transform{} among them) and
// ErrReflection when t.IsReflection().
func (n *Node) Fixed(t r3.Transform) (*Node, error) {
	if !t.IsValid() {
		return nil, fmt.Errorf("%w: argument t", ErrInvalidTransform)
	}
	if t.IsReflection() {
		return nil, fmt.Errorf("%w: argument t", ErrReflection)
	}
	return &Node{rig: n.rig, parent: n, kind: jointFixed, fixed: t}, nil
}

// Revolute adds a child that rotates about the axis through center along axis
// by angle.At(t). It returns ErrNilChannel for a nil angle, ErrKind unless
// angle.Kind() == units.Angle, and r3.ErrDegenerateAxis (or r3.ErrNonFinite
// for a non-finite center) for an axis or center r3 cannot rotate about.
func (n *Node) Revolute(center, axis r3.Vec, angle *Channel) (*Node, error) {
	if angle == nil {
		return nil, fmt.Errorf("%w: argument angle", ErrNilChannel)
	}
	if angle.Kind() != units.Angle {
		return nil, fmt.Errorf("%w: revolute angle is %s, want %s", ErrKind, angle.Kind(), units.Angle)
	}
	if _, err := r3.RotationAround(center, axis, units.Radians(0)); err != nil {
		return nil, err
	}
	return &Node{rig: n.rig, parent: n, kind: jointRevolute, center: center, axis: axis, channel: angle}, nil
}

// Prismatic adds a child that slides along the unit direction of dir by
// distance.At(t). It returns ErrNilChannel for a nil distance, ErrKind unless
// distance.Kind() == units.Length, and ErrDegenerateDirection when dir has no
// direction.
func (n *Node) Prismatic(dir r3.Vec, distance *Channel) (*Node, error) {
	if distance == nil {
		return nil, fmt.Errorf("%w: argument distance", ErrNilChannel)
	}
	if distance.Kind() != units.Length {
		return nil, fmt.Errorf("%w: prismatic distance is %s, want %s", ErrKind, distance.Kind(), units.Length)
	}
	unit, ok := dir.Normalize()
	if !ok {
		return nil, fmt.Errorf("%w: argument dir", ErrDegenerateDirection)
	}
	return &Node{rig: n.rig, parent: n, kind: jointPrismatic, dir: unit, channel: distance}, nil
}

// Local returns the joint's own transform at t. The root's is the identity.
func (n *Node) Local(t time.Duration) (r3.Transform, error) {
	switch n.kind {
	case jointFixed:
		return n.fixed, nil
	case jointRevolute:
		angle, err := n.channel.At(t)
		if err != nil {
			return r3.Transform{}, err
		}
		return r3.RotationAround(n.center, n.axis, angle)
	case jointPrismatic:
		d, err := n.channel.At(t)
		if err != nil {
			return r3.Transform{}, err
		}
		mm, err := d.In(units.Millimeter)
		if err != nil {
			return r3.Transform{}, err
		}
		return r3.Translation(n.dir.Scale(mm))
	default:
		return r3.Identity(), nil
	}
}

// World returns the transform from this node's frame to the world frame at t:
// Local(t).Then(parent.World(t)), with the root's World the identity.
func (n *Node) World(t time.Duration) (r3.Transform, error) {
	if n.parent == nil {
		return r3.Identity(), nil
	}
	local, err := n.Local(t)
	if err != nil {
		return r3.Transform{}, err
	}
	parent, err := n.parent.World(t)
	if err != nil {
		return r3.Transform{}, err
	}
	return local.Then(parent)
}
