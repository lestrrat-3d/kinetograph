package main

import (
	"testing"

	"github.com/stretchr/testify/require"
)

func TestFilterGraph(t *testing.T) {
	// A dissolve of 0.5 s, a cut, then a dissolve of 1 s.
	shots := []Shot{window("a", 0, 5000), window("b", 4500, 10000), window("c", 10000, 12000), window("d", 11000, 15000)}
	require.NoError(t, validateShots(shots))
	require.Equal(t,
		"[0]settb=AVTB[i0];[1]settb=AVTB[i1];[2]settb=AVTB[i2];[3]settb=AVTB[i3];"+
			"[i0][i1]xfade=transition=fade:duration=0.5:offset=4.5[v1];"+
			"[v1][i2]concat=n=2:v=1:a=0,settb=AVTB[v2];"+
			"[v2][i3]xfade=transition=fade:duration=1:offset=11,format=yuv420p[v]",
		filterGraph(shots))

	require.Equal(t, "[0]settb=AVTB,format=yuv420p[v]", filterGraph(shots[:1]))
}

func TestMP4Command(t *testing.T) {
	args := mp4Command(shotTable(), 30, "out").args()
	require.Equal(t, []string{
		"ffmpeg",
		"-framerate", "30", "-i", "out/build_%06d.png",
		"-framerate", "30", "-i", "out/shapes_%06d.png",
		"-framerate", "30", "-i", "out/wordmark_%06d.png",
		"-filter_complex",
		"[0]settb=AVTB[i0];[1]settb=AVTB[i1];[2]settb=AVTB[i2];" +
			"[i0][i1]xfade=transition=fade:duration=0.5:offset=12.5[v1];" +
			"[v1][i2]xfade=transition=fade:duration=0.5:offset=19.5,format=yuv420p[v]",
		"-map", "[v]", "-c:v", "libx264", "-crf", "18", "-preset", "slow", "-pix_fmt", "yuv420p",
		"-movflags", "+faststart", "out/decad-landing.mp4",
	}, args)
}

func TestShellQuote(t *testing.T) {
	require.Equal(t, "out/build_%06d.png", shellQuote("out/build_%06d.png"))
	require.Equal(t, `"[v]"`, shellQuote("[v]"))
	require.Equal(t, `"my out/x.png"`, shellQuote("my out/x.png"))
	require.Equal(t, `'$HOME'`, shellQuote("$HOME"))
	require.Equal(t, `"it's"`, shellQuote("it's"))
	require.Equal(t, `'it'\''s $x'`, shellQuote("it's $x"))
}
