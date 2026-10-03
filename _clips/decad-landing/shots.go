package main

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

// shotGrid is the step every shot boundary lies on. With an even frame rate a
// multiple of it is a whole number of frames, so two overlapping shots render
// the overlap at the same global times.
const shotGrid = 500 * time.Millisecond

// Take is what a shot renders: its scene and its style. Build leaves the
// style's Width and Height zero; the program sets them from its flags.
type Take struct {
	Scene *kinetograph.Scene
	Style render.Style
}

// Shot is a window [From, To) on the global clock with its own scene. Build
// receives the script's channels shifted so that From is local time 0.
type Shot struct {
	Name     string
	From, To time.Duration
	Build    func(ctx context.Context, ch *Channels) (*Take, error)
}

// shotTable is the clip's shots in playing order. The boundary between two
// consecutive shots is a cut when the next starts where the previous ends,
// and a dissolve over the overlap when it starts earlier.
func shotTable() []Shot {
	return []Shot{
		{Name: "build", From: 0, To: 13 * time.Second, Build: buildTake},
		{Name: "shapes", From: 12500 * time.Millisecond, To: 20 * time.Second, Build: shapesTake},
		{Name: "wordmark", From: 19500 * time.Millisecond, To: 24 * time.Second, Build: wordmarkTake},
	}
}

// validateShots checks the table: every From and To on the 0.5 s grid, every
// window non-empty, no two shots with one name, and no gap between two
// consecutive shots.
func validateShots(shots []Shot) error {
	seen := make(map[string]struct{}, len(shots))
	for i, s := range shots {
		if _, ok := seen[s.Name]; ok {
			return fmt.Errorf("two shots named %q", s.Name)
		}
		seen[s.Name] = struct{}{}
		if s.From < 0 || s.From%shotGrid != 0 || s.To%shotGrid != 0 {
			return fmt.Errorf("shot %q: window [%s, %s) is not on the %s grid", s.Name, s.From, s.To, shotGrid)
		}
		if s.To <= s.From {
			return fmt.Errorf("shot %q: window [%s, %s) is empty", s.Name, s.From, s.To)
		}
		if i == 0 {
			continue
		}
		prev := shots[i-1]
		if s.From > prev.To {
			return fmt.Errorf("gap between shot %q (ends %s) and shot %q (starts %s)", prev.Name, prev.To, s.Name, s.From)
		}
		if s.From < prev.From {
			return fmt.Errorf("shot %q starts at %s, before shot %q at %s", s.Name, s.From, prev.Name, prev.From)
		}
	}
	return nil
}

// dissolve is the length of the dissolve from prev into next: 0 for a cut.
func dissolve(prev, next Shot) time.Duration {
	return prev.To - next.From
}

// validateFormat refuses a frame rate below 2 or odd, and a frame size that
// is not positive or not even: the boundaries need whole frames, and both
// ffmpeg commands write yuv420p.
func validateFormat(fps, width, height int) error {
	if fps < 2 || fps%2 != 0 {
		return fmt.Errorf("-fps %d: the frame rate must be even and at least 2", fps)
	}
	if width < 2 || width%2 != 0 || height < 2 || height%2 != 0 {
		return fmt.Errorf("-width %d -height %d: both must be even and positive", width, height)
	}
	return nil
}

// selectShots returns the shots only names, comma separated, in table order.
// An empty only selects every shot. A name that names no shot is an error
// listing the shot names.
func selectShots(shots []Shot, only string) ([]Shot, error) {
	if only == "" {
		return shots, nil
	}
	names := make([]string, len(shots))
	for i, s := range shots {
		names[i] = s.Name
	}
	wanted := make(map[string]struct{})
	for n := range strings.SplitSeq(only, ",") {
		n = strings.TrimSpace(n)
		found := false
		for _, s := range shots {
			if s.Name == n {
				found = true
				break
			}
		}
		if !found {
			return nil, fmt.Errorf("-only: no shot named %q; the shots are %s", n, strings.Join(names, ", "))
		}
		wanted[n] = struct{}{}
	}
	selected := make([]Shot, 0, len(wanted))
	for _, s := range shots {
		if _, ok := wanted[s.Name]; ok {
			selected = append(selected, s)
		}
	}
	return selected, nil
}
