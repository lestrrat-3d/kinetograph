package kinetograph_test

import (
	"math"
	"testing"
	"time"

	"github.com/lestrrat-3d/units"
	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph"
)

func mustChannel(t *testing.T, keys ...kinetograph.Keyframe) *kinetograph.Channel {
	t.Helper()
	c, err := kinetograph.NewChannel(keys...)
	require.NoError(t, err)
	return c
}

func radians(t *testing.T, c *kinetograph.Channel, at time.Duration) float64 {
	t.Helper()
	v, err := c.At(at)
	require.NoError(t, err)
	r, err := v.In(units.Radian)
	require.NoError(t, err)
	return r
}

func TestChannelAtKeyframes(t *testing.T) {
	keys := []kinetograph.Keyframe{
		{At: 100 * time.Millisecond, Value: units.Degrees(10)},
		{At: 400 * time.Millisecond, Value: units.Degrees(70), Ease: kinetograph.EaseInOut},
		{At: time.Second, Value: units.Degrees(-20), Ease: kinetograph.SmoothStep},
	}
	c := mustChannel(t, keys...)
	for _, k := range keys {
		got, err := c.At(k.At)
		require.NoError(t, err)
		require.True(t, got.Equal(k.Value, 0), "At(%s) = %s, want %s", k.At, got, k.Value)
	}
}

func TestChannelEasingFormulas(t *testing.T) {
	// One segment from 10 to 110 degrees over one second. u is read off the
	// time, and the expected eased position is written out from §5.1's formula,
	// not taken from the Easing under test.
	cases := []struct {
		name string
		ease kinetograph.Easing
		at   time.Duration
		want func(u float64) float64
	}{
		{"linear mid", kinetograph.Linear, 500 * time.Millisecond, func(u float64) float64 { return u }},
		{"linear quarter", kinetograph.Linear, 250 * time.Millisecond, func(u float64) float64 { return u }},
		{"nil is linear", nil, 250 * time.Millisecond, func(u float64) float64 { return u }},
		{"smoothstep mid", kinetograph.SmoothStep, 500 * time.Millisecond, func(u float64) float64 { return 3*u*u - 2*u*u*u }},
		{"smoothstep quarter", kinetograph.SmoothStep, 250 * time.Millisecond, func(u float64) float64 { return 3*u*u - 2*u*u*u }},
		{"easeinout mid", kinetograph.EaseInOut, 500 * time.Millisecond, func(float64) float64 { return 0.5 }},
		{"easeinout quarter", kinetograph.EaseInOut, 250 * time.Millisecond, func(u float64) float64 { return 4 * u * u * u }},
		{"easeinout three quarters", kinetograph.EaseInOut, 750 * time.Millisecond,
			func(u float64) float64 { return 1 - 4*(1-u)*(1-u)*(1-u) }},
		{"easein quarter", kinetograph.EaseIn, 250 * time.Millisecond, func(u float64) float64 { return u * u * u }},
		{"easeout quarter", kinetograph.EaseOut, 250 * time.Millisecond, func(u float64) float64 { return 1 - (1-u)*(1-u)*(1-u) }},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c := mustChannel(t,
				kinetograph.Keyframe{At: 0, Value: units.Radians(0.1)},
				kinetograph.Keyframe{At: time.Second, Value: units.Radians(1.1), Ease: tc.ease},
			)
			u := float64(tc.at) / float64(time.Second)
			want := 0.1 + 1.0*tc.want(u)
			require.InDelta(t, want, radians(t, c, tc.at), 1e-12)
		})
	}
}

func TestChannelMixedUnitsInterpolate(t *testing.T) {
	c := mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Degrees(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Radians(math.Pi)},
	)
	require.InDelta(t, math.Pi/2, radians(t, c, 500*time.Millisecond), 1e-12)
}

func TestChannelHoldsEnds(t *testing.T) {
	c := mustChannel(t,
		kinetograph.Keyframe{At: time.Second, Value: units.Millimeters(3)},
		kinetograph.Keyframe{At: 2 * time.Second, Value: units.Millimeters(9)},
	)
	for _, tc := range []struct {
		at   time.Duration
		want units.Value
	}{
		{-time.Hour, units.Millimeters(3)},
		{0, units.Millimeters(3)},
		{3 * time.Second, units.Millimeters(9)},
		{time.Hour, units.Millimeters(9)},
	} {
		got, err := c.At(tc.at)
		require.NoError(t, err)
		require.True(t, got.Equal(tc.want, 0), "At(%s) = %s, want %s", tc.at, got, tc.want)
	}
}

func TestChannelPicksSegmentAmongMany(t *testing.T) {
	c := mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Millimeters(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Millimeters(10)},
		kinetograph.Keyframe{At: 2 * time.Second, Value: units.Millimeters(10)},
		kinetograph.Keyframe{At: 4 * time.Second, Value: units.Millimeters(50)},
	)
	for _, tc := range []struct {
		at   time.Duration
		want float64
	}{
		{500 * time.Millisecond, 5},
		{1500 * time.Millisecond, 10},
		{3 * time.Second, 30},
	} {
		got, err := c.At(tc.at)
		require.NoError(t, err)
		mm, err := got.In(units.Millimeter)
		require.NoError(t, err)
		require.InDelta(t, tc.want, mm, 1e-12, "At(%s)", tc.at)
	}
}

func TestConstantChannel(t *testing.T) {
	c := kinetograph.Constant(units.Degrees(40))
	require.Equal(t, units.Angle, c.Kind())
	for _, at := range []time.Duration{-time.Second, 0, time.Hour} {
		got, err := c.At(at)
		require.NoError(t, err)
		require.True(t, got.Equal(units.Degrees(40), 0))
	}
}

func TestNewChannelErrors(t *testing.T) {
	nan := units.Millimeters(math.NaN())
	inf := units.Millimeters(math.Inf(1))
	cases := []struct {
		name string
		keys []kinetograph.Keyframe
		want error
	}{
		{"empty", nil, kinetograph.ErrNoKeyframes},
		{"equal times", []kinetograph.Keyframe{
			{At: time.Second, Value: units.Millimeters(1)}, {At: time.Second, Value: units.Millimeters(2)}},
			kinetograph.ErrKeyframeOrder},
		{"decreasing times", []kinetograph.Keyframe{
			{At: time.Second, Value: units.Millimeters(1)}, {At: 0, Value: units.Millimeters(2)}},
			kinetograph.ErrKeyframeOrder},
		{"mixed kinds", []kinetograph.Keyframe{
			{At: 0, Value: units.Millimeters(1)}, {At: time.Second, Value: units.Degrees(2)}},
			kinetograph.ErrKind},
		{"nan", []kinetograph.Keyframe{{At: 0, Value: nan}}, units.ErrNotFinite},
		{"inf", []kinetograph.Keyframe{{At: 0, Value: units.Millimeters(1)}, {At: time.Second, Value: inf}},
			units.ErrNotFinite},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			c, err := kinetograph.NewChannel(tc.keys...)
			require.ErrorIs(t, err, tc.want)
			require.Nil(t, c)
		})
	}
}

func TestChannelAtOverflow(t *testing.T) {
	c := mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Millimeters(-1.7e308)},
		kinetograph.Keyframe{At: time.Second, Value: units.Millimeters(1.7e308)},
	)
	_, err := c.At(500 * time.Millisecond)
	require.ErrorIs(t, err, units.ErrNotFinite)
}

func TestNewChannelCopiesKeys(t *testing.T) {
	keys := []kinetograph.Keyframe{
		{At: 0, Value: units.Millimeters(0)},
		{At: time.Second, Value: units.Millimeters(10)},
	}
	c := mustChannel(t, keys...)
	keys[1].Value = units.Millimeters(999)
	got, err := c.At(time.Second)
	require.NoError(t, err)
	require.True(t, got.Equal(units.Millimeters(10), 0))
}

func TestEveryEasingFixesEndpoints(t *testing.T) {
	for name, e := range map[string]kinetograph.Easing{
		"Linear":     kinetograph.Linear,
		"SmoothStep": kinetograph.SmoothStep,
		"EaseIn":     kinetograph.EaseIn,
		"EaseOut":    kinetograph.EaseOut,
		"EaseInOut":  kinetograph.EaseInOut,
	} {
		require.Equal(t, 0.0, e.Ease(0), name)
		require.Equal(t, 1.0, e.Ease(1), name)
	}
}

func TestChannelKeyframes(t *testing.T) {
	keys := []kinetograph.Keyframe{
		{At: 0, Value: units.Scalar(0)},
		{At: 300 * time.Millisecond, Value: units.Scalar(0.25)},
		{At: time.Second, Value: units.Scalar(1), Ease: kinetograph.SmoothStep},
	}
	c := mustChannel(t, keys...)
	got := c.Keyframes()
	require.Equal(t, []kinetograph.Keyframe{
		{At: 0, Value: units.Scalar(0), Ease: kinetograph.Linear},
		{At: 300 * time.Millisecond, Value: units.Scalar(0.25), Ease: kinetograph.Linear},
		{At: time.Second, Value: units.Scalar(1), Ease: kinetograph.SmoothStep},
	}, got)

	// The result is a copy: changing it leaves the channel alone.
	got[1].Value = units.Scalar(0.9)
	v, err := c.At(300 * time.Millisecond)
	require.NoError(t, err)
	require.True(t, v.Equal(units.Scalar(0.25), 0))

	require.Equal(t, []kinetograph.Keyframe{{At: 0, Value: units.Degrees(40), Ease: kinetograph.Linear}},
		kinetograph.Constant(units.Degrees(40)).Keyframes())
}
