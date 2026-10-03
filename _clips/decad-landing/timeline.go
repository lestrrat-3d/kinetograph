package main

import (
	"cmp"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/lestrrat-3d/units"

	"github.com/lestrrat-3d/kinetograph"
)

// Track is one named scalar the clip animates: a joint's angle or slide, a
// field of view, a reshape parameter.
type Track struct {
	Name    string
	Initial units.Value
}

// Move changes one track: the track holds its value until Start, then eases
// to To and reaches it at End. Start and End are on the global clock. A move
// with End equal to Start is a step, and a nil Ease is kinetograph.Linear.
type Move struct {
	Track      string
	Start, End time.Duration
	To         units.Value
	Ease       kinetograph.Easing
}

// Script is every track and move of the clip.
type Script struct {
	Tracks []Track
	Moves  []Move
}

// Channels is a script converted to one kinetograph.Channel per track, on a
// shot's local clock.
type Channels struct {
	byName map[string]*kinetograph.Channel
}

// Get returns the channel of the named track. A name that names no track is
// an error naming it.
func (c *Channels) Get(track string) (*kinetograph.Channel, error) {
	ch, ok := c.byName[track]
	if !ok {
		return nil, fmt.Errorf("no track named %q", track)
	}
	return ch, nil
}

// errScript is wrapped by every error Channels returns for a malformed
// script.
var errScript = errors.New("invalid script")

// Channels converts the script to one channel per track, shifted so that
// global time from is local time 0. It returns an error wrapping errScript for
// two tracks of one name, a move naming no track, a negative Start, an End
// before its Start, or two overlapping moves on one track (a step counts as
// lasting 1ns), naming the track and the moves. A To of another kind than its
// track's Initial is the kinetograph.ErrKind that kinetograph.NewChannel
// returns, wrapped with the track name.
func (s Script) Channels(from time.Duration) (*Channels, error) {
	if from < 0 {
		return nil, fmt.Errorf("%w: window starts at negative time %s", errScript, from)
	}
	index := make(map[string]int, len(s.Tracks))
	for i, track := range s.Tracks {
		if _, ok := index[track.Name]; ok {
			return nil, fmt.Errorf("%w: two tracks named %q", errScript, track.Name)
		}
		index[track.Name] = i
	}
	moves := make([][]Move, len(s.Tracks))
	for _, m := range s.Moves {
		i, ok := index[m.Track]
		if !ok {
			return nil, fmt.Errorf("%w: move at %s names no track %q", errScript, m.Start, m.Track)
		}
		if m.Start < 0 {
			return nil, fmt.Errorf("%w: track %q: move starts at negative time %s", errScript, m.Track, m.Start)
		}
		if m.End < m.Start {
			return nil, fmt.Errorf("%w: track %q: move ends at %s before it starts at %s",
				errScript, m.Track, m.End, m.Start)
		}
		moves[i] = append(moves[i], m)
	}

	out := &Channels{byName: make(map[string]*kinetograph.Channel, len(s.Tracks))}
	for i, track := range s.Tracks {
		keys, err := trackKeyframes(track, moves[i])
		if err != nil {
			return nil, err
		}
		ch, err := shiftKeyframes(keys, from)
		if err != nil {
			return nil, fmt.Errorf("track %q: %w", track.Name, err)
		}
		out.byName[track.Name] = ch
	}
	return out, nil
}

// moveEnd is the time a move reaches its value: End, or Start + 1ns for a
// step.
func moveEnd(m Move) time.Duration {
	if m.End == m.Start {
		return m.Start + time.Nanosecond
	}
	return m.End
}

// trackKeyframes lays out the global keyframes of one track: {0, Initial},
// then per move in Start order a hold keyframe at Start when Start is later
// than the last keyframe, and the move's end keyframe.
func trackKeyframes(track Track, moves []Move) ([]kinetograph.Keyframe, error) {
	slices.SortStableFunc(moves, func(a, b Move) int { return cmp.Compare(a.Start, b.Start) })
	keys := []kinetograph.Keyframe{{At: 0, Value: track.Initial}}
	current := track.Initial
	for i, m := range moves {
		if i > 0 && m.Start < moveEnd(moves[i-1]) {
			prev := moves[i-1]
			return nil, fmt.Errorf("%w: track %q: move %s-%s starts before move %s-%s ends",
				errScript, track.Name, m.Start, m.End, prev.Start, prev.End)
		}
		if m.Start > keys[len(keys)-1].At {
			keys = append(keys, kinetograph.Keyframe{At: m.Start, Value: current})
		}
		keys = append(keys, kinetograph.Keyframe{At: moveEnd(m), Value: m.To, Ease: m.Ease})
		current = m.To
	}
	return keys, nil
}

// shiftKeyframes builds the channel of keys as seen from global time from:
// keyframes before from are dropped, the rest move to At - from, and a
// segment cut by from starts at local 0 with the global value at from and
// ends with the remainder of its easing.
func shiftKeyframes(keys []kinetograph.Keyframe, from time.Duration) (*kinetograph.Channel, error) {
	global, err := kinetograph.NewChannel(keys...)
	if err != nil {
		return nil, err
	}
	j := slices.IndexFunc(keys, func(k kinetograph.Keyframe) bool { return k.At >= from })
	if j < 0 {
		v, err := global.At(from)
		if err != nil {
			return nil, err
		}
		return kinetograph.Constant(v), nil
	}

	shifted := make([]kinetograph.Keyframe, 0, len(keys)-j+1)
	if keys[j].At > from {
		// keys[0] is at 0 and from >= 0, so a keyframe at or after from that is
		// not at from has a predecessor: from lies strictly inside the segment
		// keys[j-1] to keys[j].
		v, err := global.At(from)
		if err != nil {
			return nil, err
		}
		start, end := keys[j-1], keys[j]
		u0 := float64(from-start.At) / float64(end.At-start.At)
		shifted = append(shifted,
			kinetograph.Keyframe{At: 0, Value: v},
			kinetograph.Keyframe{At: end.At - from, Value: end.Value, Ease: remainder(end.Ease, u0)},
		)
		j++
	}
	for _, k := range keys[j:] {
		k.At -= from
		shifted = append(shifted, k)
	}
	return kinetograph.NewChannel(shifted...)
}

// remainder returns the easing of the part of a segment eased by e that lies
// after u0, rescaled to [0, 1]. When e(u0) is exactly 1 the segment already
// holds its end value, so the rest of it is Linear between two equal values.
func remainder(e kinetograph.Easing, u0 float64) kinetograph.Easing {
	if e == nil {
		e = kinetograph.Linear
	}
	e0 := e.Ease(u0)
	if e0 == 1 {
		return kinetograph.Linear
	}
	return remainderEasing{base: e, u0: u0, e0: e0}
}

// remainderEasing is E'(u) = (E(u0 + u·(1 − u0)) − E(u0)) / (1 − E(u0)), with
// E'(1) exactly 1.
type remainderEasing struct {
	base   kinetograph.Easing
	u0, e0 float64
}

// Ease returns E'(u). The explicit float64 conversion keeps Go from fusing
// the product and the sum into one FMA, which arm64 would and amd64 would
// not.
func (r remainderEasing) Ease(u float64) float64 {
	if u == 1 {
		return 1
	}
	return (r.base.Ease(r.u0+float64(u*(1-r.u0))) - r.e0) / (1 - r.e0)
}
