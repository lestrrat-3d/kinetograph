package main

import (
	"context"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

// shot is one GIF the README shows: the scene and style that make its frames,
// how they are sampled and rendered, and how ffmpeg encodes them.
type shot struct {
	// name is the shot's identity for -only and the frame directory's name.
	name string
	// rel is the GIF's path beneath the images directory.
	rel string
	// width and height are the rendered frame size in pixels. ffmpeg scales the
	// frames down to gifWidth, keeping the aspect ratio.
	width, height int
	gifWidth      int
	fps           int
	length        time.Duration
	// colors is the size of the palette ffmpeg builds for the GIF.
	colors int
	build  func(context.Context) (*kinetograph.Scene, render.Style, error)
}

// shots is every GIF the README shows: the hero first, then the "What it
// does" table in reading order.
func shots() []shot {
	table := func(name string, build func(context.Context) (*kinetograph.Scene, render.Style, error)) shot {
		return shot{
			name:     name,
			rel:      "features/" + name + ".gif",
			width:    640,
			height:   480,
			gifWidth: 320,
			fps:      15,
			length:   tableLength,
			colors:   64,
			build:    build,
		}
	}
	return []shot{
		{
			name:     "hero",
			rel:      "hero.gif",
			width:    1280,
			height:   960,
			gifWidth: 640,
			fps:      15,
			length:   heroLength,
			colors:   128,
			build:    heroShot,
		},
		table("revolute", revoluteShot),
		table("prismatic", prismaticShot),
		table("orbit", orbitShot),
		table("light", lightShot),
		table("fade", fadeShot),
		table("reshape", reshapeShot),
	}
}

// selectShots filters all to the comma-separated names in only, keeping the
// table order whatever the order given. An empty only keeps every shot. A name
// that names no shot is an error listing the shot names.
func selectShots(all []shot, only string) ([]shot, error) {
	if only == "" {
		return all, nil
	}
	wanted := make(map[string]struct{})
	for n := range strings.SplitSeq(only, ",") {
		wanted[strings.TrimSpace(n)] = struct{}{}
	}
	names := make([]string, 0, len(all))
	selected := make([]shot, 0, len(wanted))
	for _, s := range all {
		names = append(names, s.name)
		if _, ok := wanted[s.name]; ok {
			selected = append(selected, s)
			delete(wanted, s.name)
		}
	}
	if len(wanted) > 0 {
		unknown := make([]string, 0, len(wanted))
		for n := range wanted {
			unknown = append(unknown, n)
		}
		slices.Sort(unknown)
		return nil, fmt.Errorf("unknown shot %q; valid names are %s",
			strings.Join(unknown, ","), strings.Join(names, ", "))
	}
	return selected, nil
}
