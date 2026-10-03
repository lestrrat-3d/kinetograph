// Command gallery renders the frames of every animated image in kinetograph's
// README and prints the ffmpeg commands that encode them into GIFs under the
// repository's docs/images. kinetograph writes no video, so the GIFs come from
// ffmpeg.
//
// It lives in its own module, as decad's _gallery does, so that the gallery's
// scenes and its sketch dependency stay out of the kinetograph library. Run it
// from this directory:
//
//	go run . > assemble.sh && sh assemble.sh
//
// The program writes each shot's frames to <out>/<shot>/frame_000000.png,
// frame_000001.png, and so on. Progress goes to stderr; stdout carries only
// the shell script, which creates the image directories and runs one ffmpeg
// command per shot.
//
// Flags:
//
//   - -out <dir> is the directory the frames are written under (default "out").
//   - -images <dir> is where the printed commands write the GIFs (default: the
//     repository's docs/images, found from this source file's own path).
//   - -only <names> renders just the named shots, comma separated, in table
//     order. A name that names no shot is an error listing the shot names.
//   - -workers sets how many frames render at once (default: the CPU count).
//   - -smoke renders the first frame of every shot at 160x120 and prints no
//     script.
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
	out := flag.String("out", "out", "directory to write the frames under")
	images := flag.String("images", "", "directory the printed commands write the GIFs to (default: the repository's docs/images)")
	only := flag.String("only", "", "comma-separated shot names to render (default: all)")
	workers := flag.Int("workers", runtime.NumCPU(), "frames rendered at once")
	smoke := flag.Bool("smoke", false, "render the first frame of every shot at 160x120 and print no script")
	flag.Parse()

	selected, err := selectShots(shots(), *only)
	if err != nil {
		return err
	}
	imagesDir, err := imagesRoot(*images)
	if err != nil {
		return err
	}

	patterns := make([]string, 0, len(selected))
	for _, s := range selected {
		pattern, err := renderShot(ctx, s, filepath.Join(*out, s.name), *workers, *smoke)
		if err != nil {
			return fmt.Errorf("%s: %w", s.name, err)
		}
		patterns = append(patterns, pattern)
	}
	if *smoke {
		return nil
	}
	fmt.Fprint(os.Stdout, script(selected, patterns, imagesDir))
	return nil
}

// renderShot renders s into dir and returns the path pattern of its frame
// files. With smoke set it renders the first frame alone at 160x120.
func renderShot(ctx context.Context, s shot, dir string, workers int, smoke bool) (string, error) {
	width, height, length := s.width, s.height, s.length
	if smoke {
		width, height = 160, 120
		length = time.Second / time.Duration(s.fps)
	}
	scene, style, err := s.build(ctx)
	if err != nil {
		return "", fmt.Errorf("build the scene: %w", err)
	}
	style.Width, style.Height = width, height
	clip, err := kinetograph.NewClip(scene, s.fps, length)
	if err != nil {
		return "", fmt.Errorf("build the clip: %w", err)
	}
	renderer, err := render.New(ctx, clip, style)
	if err != nil {
		return "", fmt.Errorf("build the renderer: %w", err)
	}
	seq, err := renderer.Sequence(ctx, dir, render.WithWorkers(workers))
	if err != nil {
		return "", fmt.Errorf("render the frames: %w", err)
	}
	fmt.Fprintf(os.Stderr, "%s: wrote %d frames to %s\n", s.name, seq.Frames, seq.Dir)
	return filepath.Join(seq.Dir, seq.Pattern), nil
}

// imagesRoot is the directory every shot's rel is resolved against. With dir
// empty it is the repository's docs/images, resolved via runtime.Caller so it
// ignores the working directory: this module lives at <repo>/_gallery, so the
// repository root is the parent of this source file's own directory.
func imagesRoot(dir string) (string, error) {
	if dir != "" {
		return dir, nil
	}
	_, self, _, ok := runtime.Caller(0)
	if !ok || !filepath.IsAbs(self) {
		return "", fmt.Errorf("cannot locate this command's source file; build without -trimpath or pass -images")
	}
	return filepath.Join(filepath.Dir(filepath.Dir(self)), "docs", "images"), nil
}
