package main

import (
	"testing"
	"time"

	"github.com/lestrrat-3d/units"
	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph"
)

// testScript has a step, an eased track of two moves and a held track.
func testScript() Script {
	return Script{
		Tracks: []Track{
			{Name: "step", Initial: units.Millimeters(0)},
			{Name: "eased", Initial: units.Degrees(0)},
			{Name: "held", Initial: units.Millimeters(7)},
		},
		Moves: []Move{
			{Track: "step", Start: ms(1000), End: ms(1000), To: units.Millimeters(5)},
			{Track: "eased", Start: ms(500), End: ms(2500), To: units.Degrees(90), Ease: kinetograph.EaseInOut},
			{Track: "eased", Start: ms(3000), End: ms(3500), To: units.Degrees(10), Ease: kinetograph.EaseIn},
		},
	}
}

func TestChannelsShiftedWindow(t *testing.T) {
	script := testScript()
	global, err := script.Channels(0)
	require.NoError(t, err)

	const fps = 30
	// Windows that start before every move, inside the first eased segment
	// (after the step), inside the second, on a keyframe, and after
	// everything.
	for _, from := range []time.Duration{0, ms(1500), ms(3200), ms(2500), ms(5000)} {
		local, err := script.Channels(from)
		require.NoError(t, err)
		for _, track := range []string{"step", "eased", "held"} {
			g, err := global.Get(track)
			require.NoError(t, err)
			l, err := local.Get(track)
			require.NoError(t, err)
			for i := range 2 * fps {
				at := time.Duration(i) * time.Second / fps
				want, err := g.At(from + at)
				require.NoError(t, err)
				got, err := l.At(at)
				require.NoError(t, err)
				require.Truef(t, got.Equal(want, 1e-9), "track %s from %s at %s: got %s, want %s",
					track, from, at, got, want)
			}
		}
	}
}

func TestChannelsStepShowsAfterStart(t *testing.T) {
	ch, err := testScript().Channels(0)
	require.NoError(t, err)
	step, err := ch.Get("step")
	require.NoError(t, err)

	at, err := step.At(ms(1000))
	require.NoError(t, err)
	require.True(t, at.Equal(units.Millimeters(0), 0), "the step has not happened at its Start: %s", at)
	after, err := step.At(ms(1000) + time.Nanosecond)
	require.NoError(t, err)
	require.True(t, after.Equal(units.Millimeters(5), 0), "the step holds its value 1ns after Start: %s", after)
}

func TestRemainderEasingEnds(t *testing.T) {
	easings := []kinetograph.Easing{
		kinetograph.Linear, kinetograph.SmoothStep, kinetograph.EaseIn, kinetograph.EaseOut, kinetograph.EaseInOut,
	}
	for _, e := range easings {
		for _, u0 := range []float64{0.1, 1.0 / 3, 0.5, 0.7, 0.9999999} {
			r := remainder(e, u0)
			require.Equal(t, 1.0, r.Ease(1), "E'(1) for %T at u0 = %v", e, u0)
			require.Equal(t, 0.0, r.Ease(0), "E'(0) for %T at u0 = %v", e, u0)
		}
	}
}

func TestChannelsMoveOrder(t *testing.T) {
	sorted := testScript()
	shuffled := testScript()
	shuffled.Moves = []Move{shuffled.Moves[2], shuffled.Moves[0], shuffled.Moves[1]}

	a, err := sorted.Channels(ms(1500))
	require.NoError(t, err)
	b, err := shuffled.Channels(ms(1500))
	require.NoError(t, err)
	for _, track := range []string{"step", "eased", "held"} {
		ca, err := a.Get(track)
		require.NoError(t, err)
		cb, err := b.Get(track)
		require.NoError(t, err)
		for at := time.Duration(0); at < 3*time.Second; at += 50 * time.Millisecond {
			va, err := ca.At(at)
			require.NoError(t, err)
			vb, err := cb.At(at)
			require.NoError(t, err)
			require.True(t, va.Equal(vb, 0), "track %s at %s: %s != %s", track, at, va, vb)
		}
	}
}

func TestChannelsErrors(t *testing.T) {
	mm := units.Millimeters
	tracks := []Track{{Name: "a", Initial: mm(0)}}
	cases := []struct {
		name   string
		script Script
		target error
		names  []string // substrings the message must carry
	}{
		{
			name: "overlapping moves",
			script: Script{Tracks: tracks, Moves: []Move{
				{Track: "a", Start: ms(0), End: ms(1000), To: mm(1)},
				{Track: "a", Start: ms(500), End: ms(1500), To: mm(2)},
			}},
			target: errScript,
			names:  []string{`"a"`, "500ms-1.5s", "0s-1s"},
		},
		{
			name: "equal starts",
			script: Script{Tracks: tracks, Moves: []Move{
				{Track: "a", Start: ms(1000), End: ms(1000), To: mm(1)},
				{Track: "a", Start: ms(1000), End: ms(2000), To: mm(2)},
			}},
			target: errScript,
			names:  []string{`"a"`},
		},
		{
			name:   "negative start",
			script: Script{Tracks: tracks, Moves: []Move{{Track: "a", Start: -ms(1), End: ms(1000), To: mm(1)}}},
			target: errScript,
			names:  []string{`"a"`},
		},
		{
			name:   "end before start",
			script: Script{Tracks: tracks, Moves: []Move{{Track: "a", Start: ms(1000), End: ms(500), To: mm(1)}}},
			target: errScript,
			names:  []string{`"a"`},
		},
		{
			name:   "unknown track",
			script: Script{Tracks: tracks, Moves: []Move{{Track: "b", Start: 0, End: ms(500), To: mm(1)}}},
			target: errScript,
			names:  []string{`"b"`},
		},
		{
			name:   "duplicate track",
			script: Script{Tracks: append(tracks, Track{Name: "a", Initial: mm(1)})},
			target: errScript,
			names:  []string{`"a"`},
		},
		{
			name:   "kind mismatch",
			script: Script{Tracks: tracks, Moves: []Move{{Track: "a", Start: 0, End: ms(500), To: units.Degrees(1)}}},
			target: kinetograph.ErrKind,
			names:  []string{`"a"`},
		},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			_, err := c.script.Channels(0)
			require.ErrorIs(t, err, c.target)
			for _, n := range c.names {
				require.Contains(t, err.Error(), n)
			}
		})
	}

	ch, err := Script{Tracks: tracks}.Channels(0)
	require.NoError(t, err)
	_, err = ch.Get("missing")
	require.ErrorContains(t, err, `"missing"`)
}

func TestLandingScriptConverts(t *testing.T) {
	for _, s := range shotTable() {
		_, err := landingScript().Channels(s.From)
		require.NoError(t, err, "shot %s", s.Name)
	}
}
