package kinetograph

import (
	"fmt"
	"math"
	"slices"
	"time"

	"github.com/lestrrat-3d/units"
)

// Easing maps a normalized segment position u in [0, 1] to an eased position.
// Ease(0) is 0 and Ease(1) is 1; a value outside [0, 1] between them is
// allowed (overshoot).
type Easing interface {
	Ease(u float64) float64
}

type linearEasing struct{}

func (linearEasing) Ease(u float64) float64 { return u }

type smoothStepEasing struct{}

func (smoothStepEasing) Ease(u float64) float64 { return u * u * (3 - 2*u) }

type easeInEasing struct{}

func (easeInEasing) Ease(u float64) float64 { return u * u * u }

type easeOutEasing struct{}

func (easeOutEasing) Ease(u float64) float64 {
	v := 1 - u
	return 1 - v*v*v
}

type easeInOutEasing struct{}

func (easeInOutEasing) Ease(u float64) float64 {
	if u < 0.5 {
		return 4 * u * u * u
	}
	v := 1 - u
	return 1 - 4*v*v*v
}

// The provided easings. They use multiplication and subtraction only, never
// math.Pow, so every result is the same wherever float64 is IEEE
// (docs/design.md §7). Linear is the default for a Keyframe whose Ease is nil.
var (
	// Linear is u.
	Linear Easing = linearEasing{}
	// SmoothStep is 3u² − 2u³.
	SmoothStep Easing = smoothStepEasing{}
	// EaseIn is u³.
	EaseIn Easing = easeInEasing{}
	// EaseOut is 1 − (1 − u)³.
	EaseOut Easing = easeOutEasing{}
	// EaseInOut is cubic and symmetric about u = 1/2: 4u³ below the midpoint
	// and 1 − 4(1 − u)³ above it.
	EaseInOut Easing = easeInOutEasing{}
)

// Keyframe is one value at one time. Ease governs the segment that ENDS at
// this keyframe; it is ignored on the first keyframe of a channel. A nil Ease
// is Linear.
type Keyframe struct {
	At    time.Duration
	Value units.Value
	Ease  Easing
}

// Channel is an immutable scalar function of time: the keyframes' kind, held
// before the first keyframe and after the last, interpolated between
// neighbours as a + (b − a) · Ease(u).
type Channel struct {
	keys []Keyframe
	kind units.Kind
}

// NewChannel returns a channel over keys. It returns ErrNoKeyframes for no
// keys, ErrKeyframeOrder when the times are not strictly increasing, ErrKind
// when two keyframes differ in Kind, and an error wrapping units.ErrNotFinite
// for a non-finite value.
func NewChannel(keys ...Keyframe) (*Channel, error) {
	if len(keys) == 0 {
		return nil, ErrNoKeyframes
	}
	kind := keys[0].Value.Kind()
	owned := make([]Keyframe, len(keys))
	for i, k := range keys {
		if mag := k.Value.Mag(); math.IsNaN(mag) || math.IsInf(mag, 0) {
			return nil, fmt.Errorf("kinetograph: keyframe %d value: %w", i, units.ErrNotFinite)
		}
		if k.Value.Kind() != kind {
			return nil, fmt.Errorf("%w: keyframe %d is %s, keyframe 0 is %s", ErrKind, i, k.Value.Kind(), kind)
		}
		if i > 0 && k.At <= keys[i-1].At {
			return nil, fmt.Errorf("%w: keyframe %d at %s does not follow keyframe %d at %s",
				ErrKeyframeOrder, i, k.At, i-1, keys[i-1].At)
		}
		if k.Ease == nil {
			k.Ease = Linear
		}
		owned[i] = k
	}
	return &Channel{keys: owned, kind: kind}, nil
}

// Constant returns a channel that is v at every time.
func Constant(v units.Value) *Channel {
	return &Channel{keys: []Keyframe{{Value: v, Ease: Linear}}, kind: v.Kind()}
}

// Kind returns the kind every value of the channel carries.
func (c *Channel) Kind() units.Kind { return c.kind }

// Keyframes returns a copy of the channel's keyframes in time order. A
// keyframe given a nil Ease carries Linear. Constant(v) has one keyframe, at
// time 0.
func (c *Channel) Keyframes() []Keyframe {
	return slices.Clone(c.keys)
}

// At returns the channel's value at t: the first keyframe's value before it,
// the last keyframe's after it, and between two keyframes
// a + (b − a) · Ease(u) with u = (t − t0) / (t1 − t0) computed from the
// keyframes' integer nanoseconds. It returns an error wrapping
// units.ErrNotFinite when the interpolation overflows.
func (c *Channel) At(t time.Duration) (units.Value, error) {
	keys := c.keys
	last := len(keys) - 1
	if t <= keys[0].At {
		return keys[0].Value, nil
	}
	if t >= keys[last].At {
		return keys[last].Value, nil
	}
	// keys[0].At < t < keys[last].At: find the segment [keys[hi-1], keys[hi]).
	lo, hi := 0, last
	for hi-lo > 1 {
		mid := lo + (hi-lo)/2
		if keys[mid].At <= t {
			lo = mid
			continue
		}
		hi = mid
	}
	a, b := keys[lo], keys[hi]
	if t == a.At {
		return a.Value, nil
	}
	u := float64(t-a.At) / float64(b.At-a.At)
	e := b.Ease.Ease(u)
	d, err := b.Value.Sub(a.Value)
	if err != nil {
		return units.Value{}, err
	}
	return a.Value.Add(d.Scale(e))
}
