package kinetograph

import (
	"context"
	"fmt"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/lestrrat-3d/decad"
	"github.com/lestrrat-3d/units"

	"github.com/lestrrat-3d/kinetograph/internal/memo"
)

// Params is the values of a parametric part's parameter channels at one time,
// by parameter name.
type Params map[string]units.Value

// Builder builds a part's body from parameters.
//
// Build MUST return equal bodies for equal params: it reads params and nothing
// else that changes (no clock, no randomness, no shared mutable state). Build
// MUST be safe to call from several goroutines at once: render.WithWorkers and
// concurrent Scene.At calls each call it. params is a new map on every call,
// and Build may keep it. Build should create its own decad.New() document on
// every call, so rebuilt bodies do not accumulate in one document and
// concurrent calls share no document.
type Builder interface {
	Build(ctx context.Context, params Params) (*decad.Body, error)
}

// parametric is what AddParametric records for one part. Its address is the
// part's identity in a BuildCache key.
type parametric struct {
	builder  Builder
	names    []string // sorted once; evaluation walks this, never the map
	channels []*Channel
}

// AddParametric attaches a part whose body at time t is b.Build(ctx, p), where
// p[name] is params[name].At(t) for every name in params. The map is copied, so
// a later change to it does not reach the scene.
//
// It returns ErrNilBuilder for a nil b, ErrForeignNode for a node of another
// rig, ErrNilChannel for a nil channel (naming the parameter), an error
// wrapping units.ErrUnnamedKind or units.ErrOverflowedKind for a keyframe value
// that has no text form, and ErrDuplicateName for a name already added. An
// empty params is allowed: the part then has one body per BuildCache.
func (s *Scene) AddParametric(name string, node *Node, b Builder, params map[string]*Channel) error {
	if b == nil {
		return fmt.Errorf("%w: part %q", ErrNilBuilder, name)
	}
	if node == nil || node.rig != s.rig {
		return fmt.Errorf("%w: part %q", ErrForeignNode, name)
	}
	names := slices.Sorted(maps.Keys(params))
	channels := make([]*Channel, len(names))
	for i, pname := range names {
		c := params[pname]
		if c == nil {
			return fmt.Errorf("%w: part %q parameter %q", ErrNilChannel, name, pname)
		}
		for k, key := range c.keys {
			if _, err := key.Value.MarshalText(); err != nil {
				return fmt.Errorf("kinetograph: part %q parameter %q keyframe %d: %w", name, pname, k, err)
			}
		}
		channels[i] = c
	}
	if _, dup := s.names[name]; dup {
		return fmt.Errorf("%w: %q", ErrDuplicateName, name)
	}
	s.names[name] = struct{}{}
	s.parts = append(s.parts, part{
		name: name,
		node: node,
		param: &parametric{
			builder:  b,
			names:    names,
			channels: channels,
		},
	})
	return nil
}

// buildKey is one BuildCache entry: a part and the text of its parameter
// values.
type buildKey struct {
	part   *parametric
	values string
}

// BuildCache holds the bodies Builders returned, by part and parameter tuple.
// The tuple's key is each value's units.Value.MarshalText in parameter-name
// order, joined by "\n"; MarshalText writes only printable ASCII and round-trips
// exactly, so equal keys mean equal values. Build runs once per distinct part
// and tuple: a caller that asks for a tuple while its Build runs waits for that
// call. A failed Build is kept and returned for that tuple again, except one
// that failed while its caller's ctx was done.
//
// A BuildCache is safe for concurrent use and keeps every body it built until
// the cache itself is dropped. Use NewBuildCache to create one.
type BuildCache struct {
	bodies memo.Map[buildKey, *decad.Body]
}

// NewBuildCache returns an empty cache.
func NewBuildCache() *BuildCache {
	return &BuildCache{}
}

// evaluate returns p's parameter values at t and their cache key text.
func (p *parametric) evaluate(t time.Duration) (Params, string, error) {
	params := make(Params, len(p.names))
	var key strings.Builder
	for i, name := range p.names {
		v, err := p.channels[i].At(t)
		if err != nil {
			return nil, "", fmt.Errorf("parameter %q: %w", name, err)
		}
		text, err := v.MarshalText()
		if err != nil {
			return nil, "", fmt.Errorf("parameter %q: %w", name, err)
		}
		if i > 0 {
			key.WriteByte('\n')
		}
		key.Write(text)
		params[name] = v
	}
	return params, key.String(), nil
}

// body returns p's body at t from cache, building it on a miss, and the
// parameter values it was built from.
func (p *parametric) body(ctx context.Context, cache *BuildCache, t time.Duration) (*decad.Body, Params, error) {
	params, key, err := p.evaluate(t)
	if err != nil {
		return nil, nil, err
	}
	body, err := cache.bodies.Get(ctx, buildKey{part: p, values: key}, func(ctx context.Context) (*decad.Body, error) {
		body, err := p.builder.Build(ctx, maps.Clone(params))
		if err != nil {
			return nil, fmt.Errorf("build: %w", err)
		}
		if body == nil {
			return nil, fmt.Errorf("build: %w", ErrNilBody)
		}
		return body, nil
	})
	if err != nil {
		return nil, nil, err
	}
	return body, params, nil
}
