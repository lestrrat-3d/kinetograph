package main

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"
	"time"
)

// The names of the two videos the commands write into the output directory.
const (
	mp4Name = "decad-landing.mp4"
	gifName = "decad-landing.gif"
)

// gifFilter scales the MP4 to 800 px wide at 15 fps and maps it to a
// 128-colour palette built from the whole clip.
const gifFilter = "fps=15,scale=800:-1:flags=lanczos,split[a][b];" +
	"[a]palettegen=max_colors=128:stats_mode=diff[p];" +
	"[b][p]paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle"

// command is one program invocation, split into lines of arguments for
// printing: the first line starts with the program name.
type command [][]string

// args is the command's argument vector, program name first.
func (c command) args() []string {
	var argv []string
	for _, line := range c {
		argv = append(argv, line...)
	}
	return argv
}

// String renders the command as one shell command, its lines joined by
// backslash continuations. A filter graph in double quotes is also broken
// after each ";" by a backslash and a newline, which the shell removes inside
// double quotes.
func (c command) String() string {
	lines := make([]string, len(c))
	for i, line := range c {
		quoted := make([]string, len(line))
		for j, arg := range line {
			quoted[j] = shellQuote(arg)
			if strings.HasPrefix(quoted[j], `"`) {
				quoted[j] = strings.ReplaceAll(quoted[j], ";", ";\\\n")
			}
		}
		lines[i] = strings.Join(quoted, " ")
	}
	return strings.Join(lines, " \\\n  ")
}

// mp4Command reads every shot's frames from dir and writes the MP4: one input
// per shot in table order, joined by the filter graph of filterGraph.
func mp4Command(shots []Shot, fps int, dir string) command {
	cmd := command{{"ffmpeg"}}
	rate := strconv.Itoa(fps)
	for _, s := range shots {
		cmd = append(cmd, []string{"-framerate", rate, "-i", filepath.Join(dir, s.Name+"_%06d.png")})
	}
	return append(cmd,
		[]string{"-filter_complex", filterGraph(shots)},
		[]string{
			"-map", "[v]", "-c:v", "libx264", "-crf", "18", "-preset", "slow", "-pix_fmt", "yuv420p",
			"-movflags", "+faststart", filepath.Join(dir, mp4Name),
		},
	)
}

// gifCommand converts the MP4 in dir to a looping GIF beside it.
func gifCommand(dir string) command {
	return command{
		{"ffmpeg", "-i", filepath.Join(dir, mp4Name)},
		{"-vf", gifFilter},
		{"-loop", "0", filepath.Join(dir, gifName)},
	}
}

// filterGraph joins the shots' inputs in table order. Every input passes
// through settb=AVTB first. A dissolve is an xfade whose offset is the next
// shot's From relative to the first shot's; a cut is a concat followed by
// settb=AVTB, because xfade refuses inputs whose time bases differ and
// ffmpeg gives a concat output a time base of its own.
func filterGraph(shots []Shot) string {
	if len(shots) == 1 {
		return "[0]settb=AVTB,format=yuv420p[v]"
	}
	steps := make([]string, 0, 2*len(shots))
	for i := range shots {
		steps = append(steps, fmt.Sprintf("[%d]settb=AVTB[i%d]", i, i))
	}
	prev := "i0"
	for k := 1; k < len(shots); k++ {
		out, tail := "v"+strconv.Itoa(k), ""
		if k == len(shots)-1 {
			out, tail = "v", ",format=yuv420p"
		}
		var join string
		if d := dissolve(shots[k-1], shots[k]); d > 0 {
			join = fmt.Sprintf("xfade=transition=fade:duration=%s:offset=%s",
				seconds(d), seconds(shots[k].From-shots[0].From))
		} else {
			join = "concat=n=2:v=1:a=0,settb=AVTB"
		}
		steps = append(steps, fmt.Sprintf("[%s][i%d]%s%s[%s]", prev, k, join, tail, out))
		prev = out
	}
	return strings.Join(steps, ";")
}

// seconds writes d in seconds with as few digits as represent it exactly.
func seconds(d time.Duration) string {
	return strconv.FormatFloat(d.Seconds(), 'f', -1, 64)
}

// shellQuote returns arg as one POSIX shell word: unchanged when it holds
// only characters the shell reads literally, in double quotes when it holds
// none of the characters double quotes still expand, and in single quotes
// otherwise.
func shellQuote(arg string) string {
	if arg != "" && strings.IndexFunc(arg, func(r rune) bool {
		return (r < 'a' || r > 'z') && (r < 'A' || r > 'Z') && (r < '0' || r > '9') && !strings.ContainsRune("_./%:+=,-", r)
	}) < 0 {
		return arg
	}
	if !strings.ContainsAny(arg, "\"$`\\!") {
		return `"` + arg + `"`
	}
	return "'" + strings.ReplaceAll(arg, "'", `'\''`) + "'"
}
