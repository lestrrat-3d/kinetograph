// Command decad-landing renders the landing-page clip for decad's README: a
// 24 s clip in three acts on one global clock.
//
//   - build (0 to 13 s): a flange plate grows from a 2 mm slab, three drill
//     tools plunge into it while its holes follow their tips, its vertical
//     edges round over and its top cap loop takes a chamfer, then a pin drops
//     into the bore and the camera tilts up to show the clearance around it.
//   - shapes (12.5 to 20 s): a camera dollies past a shelf of six parts, each
//     turning once: a revolved ring, a swept duct, a lofted duct, a free-form
//     blade, a shelled tray and a surface-result dish.
//   - wordmark (19.5 to 24 s): the five letters of DECAD drop onto a shelled
//     backing plate between a peg and a dome.
//
// Each act is a shot with its own scene, rendered as its own numbered PNG
// sequence; shots that overlap on the clock dissolve into each other in the
// video. The program writes the frames and prints the two ffmpeg commands
// that assemble them, so from this directory
//
//	go run . > assemble.sh && sh assemble.sh
//
// writes out/decad-landing.mp4 and out/decad-landing.gif. With the default
// flags the commands are:
//
//	ffmpeg \
//	  -framerate 30 -i out/build_%06d.png \
//	  -framerate 30 -i out/shapes_%06d.png \
//	  -framerate 30 -i out/wordmark_%06d.png \
//	  -filter_complex "[0]settb=AVTB[i0];\
//	[1]settb=AVTB[i1];\
//	[2]settb=AVTB[i2];\
//	[i0][i1]xfade=transition=fade:duration=0.5:offset=12.5[v1];\
//	[v1][i2]xfade=transition=fade:duration=0.5:offset=19.5,format=yuv420p[v]" \
//	  -map "[v]" -c:v libx264 -crf 18 -preset slow -pix_fmt yuv420p -movflags +faststart out/decad-landing.mp4
//	ffmpeg -i out/decad-landing.mp4 \
//	  -vf "fps=15,scale=800:-1:flags=lanczos,split[a][b];\
//	[a]palettegen=max_colors=128:stats_mode=diff[p];\
//	[b][p]paletteuse=dither=bayer:bayer_scale=5:diff_mode=rectangle" \
//	  -loop 0 out/decad-landing.gif
//
// Every input passes through settb=AVTB before an xfade, because xfade
// refuses two inputs whose time bases differ. Progress goes to stderr; stdout
// carries only the commands.
//
// Flags:
//
//   - -out <dir> is where the frames go (default "out"). Shot s writes
//     <dir>/s_000000.png, <dir>/s_000001.png, and so on.
//   - -width and -height set the frame size (default 1280x720). Both must be
//     even, because both commands write yuv420p.
//   - -fps sets the frame rate (default 30). It must be even, so that every
//     shot boundary, a multiple of 0.5 s, falls on a whole frame.
//   - -workers sets how many frames render at once (default: the CPU count).
//   - -only <names> renders the named shots, comma separated, in table order,
//     and prints no command.
//   - -smoke renders the first frame of every shot at 160x90 and prints no
//     command.
//   - -probe renders nothing. It evaluates every frame of every shot through
//     one reshape cache per shot, so each distinct parameter tuple is built
//     once, tessellates every distinct body, and prints how many tuples it
//     built. It fails at the first frame decad refuses, naming the shot, the
//     frame and the parameter values.
package main

import (
	"context"
	"flag"
	"fmt"
	"os"
	"runtime"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lestrrat-3d/decad"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/render"
)

// The -smoke frame size.
const (
	smokeWidth  = 160
	smokeHeight = 90
)

func main() {
	if err := run(context.Background()); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

// options are the parsed flags.
type options struct {
	out           string
	width, height int
	fps           int
	workers       int
	only          string
	smoke         bool
	probe         bool
}

func run(ctx context.Context) error {
	var opts options
	flag.StringVar(&opts.out, "out", "out", "directory to write the frames to")
	flag.IntVar(&opts.width, "width", 1280, "frame width in pixels (even)")
	flag.IntVar(&opts.height, "height", 720, "frame height in pixels (even)")
	flag.IntVar(&opts.fps, "fps", 30, "frames per second (even)")
	flag.IntVar(&opts.workers, "workers", runtime.NumCPU(), "frames rendered at once")
	flag.StringVar(&opts.only, "only", "", "comma-separated shot names to render (default: all)")
	flag.BoolVar(&opts.smoke, "smoke", false, "render the first frame of every shot at 160x90")
	flag.BoolVar(&opts.probe, "probe", false, "build and tessellate every frame's bodies without rendering")
	flag.Parse()

	if opts.smoke {
		opts.width, opts.height = smokeWidth, smokeHeight
	}
	if err := validateFormat(opts.fps, opts.width, opts.height); err != nil {
		return err
	}
	if opts.workers < 1 {
		return fmt.Errorf("-workers %d: must be at least 1", opts.workers)
	}
	shots := shotTable()
	if err := validateShots(shots); err != nil {
		return err
	}
	selected, err := selectShots(shots, opts.only)
	if err != nil {
		return err
	}
	script := landingScript()

	if opts.probe {
		for _, shot := range selected {
			if err := probeShot(ctx, shot, script, opts); err != nil {
				return err
			}
		}
		return nil
	}

	for _, shot := range selected {
		if err := renderShot(ctx, shot, script, opts); err != nil {
			return err
		}
	}
	if opts.only != "" || opts.smoke {
		return nil
	}
	fmt.Fprintln(os.Stdout, mp4Command(shots, opts.fps, opts.out))
	fmt.Fprintln(os.Stdout, gifCommand(opts.out))
	return nil
}

// shotClip builds shot's take and its clip: the whole window, or its first
// frame alone when firstOnly is set.
func shotClip(ctx context.Context, shot Shot, script Script, fps int, firstOnly bool) (*Take, *kinetograph.Clip, error) {
	ch, err := script.Channels(shot.From)
	if err != nil {
		return nil, nil, fmt.Errorf("shot %s: %w", shot.Name, err)
	}
	take, err := shot.Build(ctx, ch)
	if err != nil {
		return nil, nil, fmt.Errorf("shot %s: %w", shot.Name, err)
	}
	length := shot.To - shot.From
	if firstOnly {
		length = time.Second / time.Duration(fps)
	}
	clip, err := kinetograph.NewClip(take.Scene, fps, length)
	if err != nil {
		return nil, nil, fmt.Errorf("shot %s: %w", shot.Name, err)
	}
	return take, clip, nil
}

// renderShot writes shot's frames to opts.out, named after the shot.
func renderShot(ctx context.Context, shot Shot, script Script, opts options) error {
	take, clip, err := shotClip(ctx, shot, script, opts.fps, opts.smoke)
	if err != nil {
		return err
	}
	style := take.Style
	style.Width, style.Height = opts.width, opts.height
	renderer, err := render.New(ctx, clip, style)
	if err != nil {
		return fmt.Errorf("shot %s: %w", shot.Name, err)
	}
	fmt.Fprintf(os.Stderr, "%s: rendering %d frames\n", shot.Name, clip.FrameCount())
	seq, err := renderer.Sequence(ctx, opts.out, render.WithWorkers(opts.workers), render.WithPrefix(shot.Name+"_"))
	if err != nil {
		return fmt.Errorf("shot %s: %w", shot.Name, err)
	}
	fmt.Fprintf(os.Stderr, "%s: wrote %d frames to %s\n", shot.Name, seq.Frames, seq.Dir)
	return nil
}

// probeShot evaluates every frame of shot through one BuildCache, then
// tessellates every distinct body the frames hold at the shot's chord. It
// prints the counts to stdout and returns the failure of the lowest frame
// index, naming the frame and, for a body, its part and parameter values.
func probeShot(ctx context.Context, shot Shot, script Script, opts options) error {
	take, clip, err := shotClip(ctx, shot, script, opts.fps, false)
	if err != nil {
		return err
	}
	cache := kinetograph.NewBuildCache()
	frames := make([]*kinetograph.Frame, clip.FrameCount())
	i, err := forEach(len(frames), opts.workers, func(i int) error {
		f, err := clip.FrameCached(ctx, i, cache)
		frames[i] = f
		return err
	})
	if err != nil {
		return fmt.Errorf("probe: shot %s frame %d (t = %s): %w", shot.Name, i, clip.FrameTime(i), err)
	}

	type probed struct {
		frame int
		pose  kinetograph.Pose
	}
	var bodies []probed
	seen := make(map[*decad.Body]struct{})
	tuples := 0
	for i, f := range frames {
		for _, pose := range f.Poses {
			if _, ok := seen[pose.Body]; ok {
				continue
			}
			seen[pose.Body] = struct{}{}
			bodies = append(bodies, probed{frame: i, pose: pose})
			if pose.Params != nil {
				tuples++
			}
		}
	}
	chord := take.Style.Chord
	i, err = forEach(len(bodies), opts.workers, func(i int) error {
		_, err := bodies[i].pose.Body.Tessellate(ctx, chord, decad.WithVerification(decad.VerifyNone))
		return err
	})
	if err != nil {
		b := bodies[i]
		return fmt.Errorf("probe: shot %s frame %d (t = %s): tessellate part %s at %s: %w",
			shot.Name, b.frame, clip.FrameTime(b.frame), b.pose.Name, formatParams(b.pose.Params), err)
	}
	fmt.Fprintf(os.Stdout, "%s: %d frames, %d parameter tuples built, %d bodies tessellated\n",
		shot.Name, len(frames), tuples, len(bodies))
	return nil
}

// forEach calls fn(i) for i in [0, n) on up to workers goroutines, handing
// out indices in increasing order. After a failure it hands out no new index,
// and it returns the failure with the lowest index, or -1 and nil.
func forEach(n, workers int, fn func(i int) error) (int, error) {
	errs := make([]error, n)
	var next atomic.Int64
	var failed atomic.Bool
	var wg sync.WaitGroup
	for range min(workers, n) {
		wg.Go(func() {
			for !failed.Load() {
				i := int(next.Add(1) - 1)
				if i >= n {
					return
				}
				if errs[i] = fn(i); errs[i] != nil {
					failed.Store(true)
				}
			}
		})
	}
	wg.Wait()
	for i, err := range errs {
		if err != nil {
			return i, err
		}
	}
	return -1, nil
}
