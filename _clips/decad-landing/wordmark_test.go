package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// TestWordLampSweeps checks the act C light: dark before 22.0 s, then
// brightening the letters while the lit pixels move left to right across the
// name.
func TestWordLampSweeps(t *testing.T) {
	ctx := t.Context()
	const from = 19500 * time.Millisecond
	ch, err := landingScript().Channels(from)
	require.NoError(t, err)
	take := func() *Take {
		tk, err := wordmarkTake(ctx, ch)
		require.NoError(t, err)
		return tk
	}
	counts, xs := lampSweep(t, take, wordLamp, 4500*time.Millisecond,
		ms(21900)-from, ms(22400)-from, ms(22800)-from, ms(23200)-from)
	require.Zero(t, counts[0], "the lamp is dark before 22.0 s")
	for i := 1; i < len(counts); i++ {
		require.Greater(t, counts[i], testWidth*testHeight/200, "lit pixels at sample %d", i)
		require.Greater(t, xs[i]-xs[i-1], 50.0, "the lit pixels move right from sample %d to %d", i-1, i)
	}
}
