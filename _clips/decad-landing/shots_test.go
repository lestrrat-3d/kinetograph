package main

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

// window is a shot with no Build, for tests of the table rules.
func window(name string, from, to int) Shot {
	return Shot{Name: name, From: ms(from), To: ms(to)}
}

func TestValidateShots(t *testing.T) {
	require.NoError(t, validateShots(shotTable()), "the clip's own table")

	cut := []Shot{window("a", 0, 2000), window("b", 2000, 4000)}
	require.NoError(t, validateShots(cut))
	require.Equal(t, time.Duration(0), dissolve(cut[0], cut[1]))

	dissolving := []Shot{window("a", 0, 2000), window("b", 1500, 4000)}
	require.NoError(t, validateShots(dissolving))
	require.Equal(t, ms(500), dissolve(dissolving[0], dissolving[1]))

	err := validateShots([]Shot{window("a", 0, 2000), window("b", 2500, 4000)})
	require.ErrorContains(t, err, "gap")
	require.ErrorContains(t, err, `"a"`)
	require.ErrorContains(t, err, `"b"`)

	err = validateShots([]Shot{window("a", 0, 2000), window("b", 2000, 3700)})
	require.ErrorContains(t, err, `"b"`)
	require.ErrorContains(t, err, "grid")
}

func TestValidateFormat(t *testing.T) {
	require.NoError(t, validateFormat(30, 1280, 720))
	require.ErrorContains(t, validateFormat(25, 1280, 720), "-fps 25")
	require.ErrorContains(t, validateFormat(30, 1281, 720), "even")
	require.ErrorContains(t, validateFormat(30, 1280, 721), "even")
}

func TestSelectShots(t *testing.T) {
	shots := []Shot{window("a", 0, 2000), window("b", 1500, 4000), window("c", 4000, 6000)}

	all, err := selectShots(shots, "")
	require.NoError(t, err)
	require.Len(t, all, 3)

	picked, err := selectShots(shots, "c, a")
	require.NoError(t, err)
	require.Equal(t, []string{"a", "c"}, []string{picked[0].Name, picked[1].Name}, "table order, not flag order")

	_, err = selectShots(shots, "a,x")
	require.ErrorContains(t, err, `"x"`)
	require.ErrorContains(t, err, "a, b, c")
}
