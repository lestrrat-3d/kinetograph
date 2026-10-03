package main

import (
	"slices"
	"testing"

	"github.com/stretchr/testify/require"
)

// Every shot ends in the pose it starts in, so its GIF loops without a jump:
// each part's transform and parameters, the camera, each light, and each
// part's fade are the same at the clip's end as at time 0.
func TestShotsLoop(t *testing.T) {
	for _, s := range shots() {
		t.Run(s.name, func(t *testing.T) {
			scene, style, err := s.build(t.Context())
			require.NoError(t, err)
			first, err := scene.At(t.Context(), 0)
			require.NoError(t, err)
			last, err := scene.At(t.Context(), s.length)
			require.NoError(t, err)

			require.Len(t, last.Poses, len(first.Poses))
			for i, p := range first.Poses {
				q := last.Poses[i]
				require.Equal(t, p.Name, q.Name)
				require.True(t, p.Transform.Equal(q.Transform, 1e-9), "%s transform", p.Name)
				require.Len(t, q.Params, len(p.Params))
				for name, v := range p.Params {
					require.True(t, v.Equal(q.Params[name], 0), "%s %s", p.Name, name)
				}
			}
			require.True(t, first.Camera.Position.Equal(last.Camera.Position, 1e-9), "camera position")
			require.True(t, first.Camera.Target.Equal(last.Camera.Target, 1e-9), "camera target")
			require.True(t, first.Camera.Up.Equal(last.Camera.Up, 1e-9), "camera up")
			require.Len(t, last.Lights, len(first.Lights))
			for i, l := range first.Lights {
				require.True(t, l.Position.Equal(last.Lights[i].Position, 1e-9), "%s position", l.Name)
				require.True(t, l.Direction.Equal(last.Lights[i].Direction, 1e-9), "%s direction", l.Name)
			}
			for name, a := range style.Parts {
				if a.Fade == nil {
					continue
				}
				from, err := a.Fade.At(0)
				require.NoError(t, err)
				to, err := a.Fade.At(s.length)
				require.NoError(t, err)
				require.True(t, from.Equal(to, 0), "%s fade", name)
			}
		})
	}
}

func TestSelectShots(t *testing.T) {
	all := shots()

	t.Run("empty keeps every shot", func(t *testing.T) {
		got, err := selectShots(all, "")
		require.NoError(t, err)
		require.Len(t, got, len(all))
	})
	t.Run("table order whatever the order given", func(t *testing.T) {
		got, err := selectShots(all, "reshape, hero,orbit")
		require.NoError(t, err)
		names := make([]string, 0, len(got))
		for _, s := range got {
			names = append(names, s.name)
		}
		require.Equal(t, []string{"hero", "orbit", "reshape"}, names)
	})
	t.Run("unknown name", func(t *testing.T) {
		_, err := selectShots(all, "orbit,spin")
		require.ErrorContains(t, err, `unknown shot "spin"`)
		require.ErrorContains(t, err, "hero, revolute")
	})
}

func TestScript(t *testing.T) {
	all := shots()
	i := slices.IndexFunc(all, func(s shot) bool { return s.name == "hero" })
	j := slices.IndexFunc(all, func(s shot) bool { return s.name == "fade" })
	selected := []shot{all[i], all[j]}
	got := script(selected, []string{"out/hero/frame_%06d.png", "out/fade/frame_%06d.png"}, "/repo/docs/images")
	want := "set -e\n" +
		"mkdir -p '/repo/docs/images'\n" +
		"mkdir -p '/repo/docs/images/features'\n" +
		"ffmpeg -y -loglevel error -framerate 15 -i 'out/hero/frame_%06d.png' " +
		"-vf 'scale=640:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=128:stats_mode=diff[p];" +
		"[b][p]paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle' -loop 0 '/repo/docs/images/hero.gif'\n" +
		"ffmpeg -y -loglevel error -framerate 15 -i 'out/fade/frame_%06d.png' " +
		"-vf 'scale=320:-1:flags=lanczos,split[a][b];[a]palettegen=max_colors=64:stats_mode=diff[p];" +
		"[b][p]paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle' -loop 0 '/repo/docs/images/features/fade.gif'\n"
	require.Equal(t, want, got)
}

func TestShellQuote(t *testing.T) {
	require.Equal(t, `'it'\''s here'`, shellQuote("it's here"))
}
