package main

import (
	"fmt"
	"path/filepath"
	"strings"
)

// gifCommand is the ffmpeg command that encodes the PNG frames matching
// pattern, at fps frames per second, into a looping GIF at dst. ffmpeg scales
// the frames to the shot's GIF width with a Lanczos filter, builds one
// palette of the shot's colour count from every frame, and maps the frames to
// it with a Bayer dither.
func gifCommand(s shot, pattern, dst string) string {
	filter := fmt.Sprintf(
		"scale=%d:-1:flags=lanczos,split[a][b];"+
			"[a]palettegen=max_colors=%d:stats_mode=diff[p];"+
			"[b][p]paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle",
		s.gifWidth, s.colors)
	return fmt.Sprintf("ffmpeg -y -loglevel error -framerate %d -i %s -vf %s -loop 0 %s",
		s.fps, shellQuote(pattern), shellQuote(filter), shellQuote(dst))
}

// script is the shell script that turns every rendered shot into its GIF
// under images: it stops at the first failing command, creates each GIF's
// directory, then runs one ffmpeg command per shot. patterns holds each
// shot's frame file pattern, in the order of selected.
func script(selected []shot, patterns []string, images string) string {
	var b strings.Builder
	b.WriteString("set -e\n")
	dirs := make(map[string]struct{})
	for _, s := range selected {
		dir := filepath.Dir(filepath.Join(images, filepath.FromSlash(s.rel)))
		if _, ok := dirs[dir]; ok {
			continue
		}
		dirs[dir] = struct{}{}
		fmt.Fprintf(&b, "mkdir -p %s\n", shellQuote(dir))
	}
	for i, s := range selected {
		dst := filepath.Join(images, filepath.FromSlash(s.rel))
		b.WriteString(gifCommand(s, patterns[i], dst))
		b.WriteString("\n")
	}
	return b.String()
}

// shellQuote wraps s in single quotes for a POSIX shell. Each single quote
// inside s closes the quoted string, is written as a backslash-escaped quote,
// and reopens it.
func shellQuote(s string) string {
	return "'" + strings.ReplaceAll(s, "'", `'\''`) + "'"
}
