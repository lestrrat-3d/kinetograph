// Command demo renders kinetograph's demo clip: a drilled plate, two pins that
// drop into its bolt holes, and a ring that rises off it and tumbles, seen
// from a camera orbiting a quarter turn. It writes a numbered PNG sequence and
// prints the ffmpeg command that assembles it.
//
// It lives in its own module so that the clip's content and its sketch
// dependency stay out of the kinetograph library. Run it from this directory:
//
//	go run . -out out
//	ffmpeg -framerate 24 -i out/frame_%06d.png -c:v libx264 -pix_fmt yuv420p demo.mp4
//
// Flags:
//
//   - -out <dir> is the directory the frames are written to (default "out").
//   - -width and -height set the frame size in pixels (default 960x720).
//     yuv420p needs both to be even.
//   - -fps sets the frame rate (default 24).
//   - -workers sets how many frames render at once (default: the CPU count).
//   - -smoke renders the first frame alone at 160x90 and prints no command.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"time"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func run(ctx context.Context) error {
	out := flag.String("out", "out", "directory to write the frames to")
	width := flag.Int("width", 960, "frame width in pixels")
	height := flag.Int("height", 720, "frame height in pixels")
	fps := flag.Int("fps", 24, "frames per second")
	workers := flag.Int("workers", runtime.NumCPU(), "frames rendered at once")
	smoke := flag.Bool("smoke", false, "render the first frame alone at 160x90")
	flag.Parse()

	length := clipDuration
	if *smoke {
		*width, *height = 160, 90
		length = time.Second / time.Duration(*fps)
	}

	scene, err := buildScene(ctx)
	if err != nil {
		return fmt.Errorf("build the scene: %w", err)
	}
	clip, err := kinetograph.NewClip(scene, *fps, length)
	if err != nil {
		return fmt.Errorf("build the clip: %w", err)
	}
	renderer, err := render.New(ctx, clip, clipStyle(*width, *height))
	if err != nil {
		return fmt.Errorf("build the renderer: %w", err)
	}
	seq, err := renderer.Sequence(ctx, *out, render.WithWorkers(*workers))
	if err != nil {
		return fmt.Errorf("render the frames: %w", err)
	}

	fmt.Fprintf(os.Stdout, "wrote %d frames to %s\n", seq.Frames, seq.Dir)
	if *smoke {
		return nil
	}
	fmt.Fprintf(os.Stdout, "ffmpeg -framerate %d -i %s -c:v libx264 -pix_fmt yuv420p demo.mp4\n",
		seq.FPS, filepath.Join(seq.Dir, seq.Pattern))
	return nil
}
