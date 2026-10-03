package render

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"image"
	"image/png"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/lestrrat-go/option/v3"

	"github.com/lestrrat-3d/kinetograph"
)

// Sequence describes a written frame sequence.
type Sequence struct {
	Dir     string
	Pattern string // the printf pattern of the file names, e.g. "frame_%06d.png"
	Frames  int
	FPS     int
}

// FrameError is a failure at one frame. Unwrap returns Err.
type FrameError struct {
	Index int
	Time  time.Duration
	Err   error
}

func (e *FrameError) Error() string {
	return fmt.Sprintf("render: frame %d at %s: %v", e.Index, e.Time, e.Err)
}

func (e *FrameError) Unwrap() error { return e.Err }

// SequenceOption configures Renderer.Sequence.
type SequenceOption interface {
	option.Interface
	sequenceOption()
}

type sequenceOption struct{ option.Interface }

func (sequenceOption) sequenceOption() {}

type (
	identWorkers struct{}
	identPrefix  struct{}
)

// WithWorkers renders up to n frames concurrently. The default, and the value
// used for any n below 1, is 1.
func WithWorkers(n int) SequenceOption {
	return sequenceOption{option.New(identWorkers{}, n)}
}

// WithPrefix changes the file name prefix. The default is "frame_".
func WithPrefix(prefix string) SequenceOption {
	return sequenceOption{option.New(identPrefix{}, prefix)}
}

type sequenceConfig struct {
	workers int
	prefix  string
}

func foldSequenceOptions(opts []SequenceOption) sequenceConfig {
	cfg := sequenceConfig{workers: 1, prefix: "frame_"}
	for _, o := range opts {
		switch o.Ident().(type) {
		case identWorkers:
			if n, ok := option.Get[int](o); ok && n > 1 {
				cfg.workers = n
				continue
			}
			cfg.workers = 1
		case identPrefix:
			if p, ok := option.Get[string](o); ok {
				cfg.prefix = p
			}
		}
	}
	return cfg
}

// Sequence renders every frame of the clip to dir as PNG files named
// prefix + the six-digit frame index + ".png", and returns what a video tool
// needs to assemble them. It creates dir if needed. Each file is written to a
// temporary name in dir and renamed into place once the frame is complete, so
// a failed or cancelled run leaves no partial frame file. Each frame is the
// image Frame returns, fades and node lights included, encoded with
// png.Encode; a frame with no fading part therefore holds the bytes
// solidlens.RenderPNG writes for its one scene.
//
// The workers share one kinetograph.BuildCache and one mesh cache for this
// call, so each parametric part is built once per distinct parameter tuple
// and each rebuilt body is tessellated once, whatever the worker count. Both
// caches hold every rebuilt body and mesh until Sequence returns.
//
// It returns kinetograph.ErrNilContext for a nil ctx, the *FrameError of the
// lowest-index frame that failed, or ctx.Err() unchanged when ctx is done.
// Frames below the failing index that finished keep their files.
func (r *Renderer) Sequence(ctx context.Context, dir string, opts ...SequenceOption) (*Sequence, error) {
	if ctx == nil {
		return nil, kinetograph.ErrNilContext
	}
	cfg := foldSequenceOptions(opts)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return nil, fmt.Errorf("render: creating %s: %w", dir, err)
	}

	count := r.clip.FrameCount()
	rn := newRun()
	var (
		next    atomic.Int64
		failed  atomic.Bool
		mu      sync.Mutex
		lowest  *FrameError
		wg      sync.WaitGroup
		workers = min(cfg.workers, count)
	)
	record := func(fe *FrameError) {
		mu.Lock()
		defer mu.Unlock()
		if lowest == nil || fe.Index < lowest.Index {
			lowest = fe
		}
		failed.Store(true)
	}
	for range workers {
		wg.Go(func() {
			// Frames are claimed in increasing order, so once any frame has
			// failed no worker claims a later one, while every frame below the
			// failure was already claimed and runs to its end: the lowest
			// failing index never depends on scheduling.
			for ctx.Err() == nil && !failed.Load() {
				i := int(next.Add(1) - 1)
				if i >= count {
					return
				}
				if err := r.writeFrame(ctx, rn, dir, cfg.prefix, i); err != nil {
					if fe := (*FrameError)(nil); errors.As(err, &fe) {
						record(fe)
					}
					return
				}
			}
		})
	}
	wg.Wait()

	if lowest != nil {
		return nil, lowest
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	return &Sequence{
		Dir:     dir,
		Pattern: strings.ReplaceAll(cfg.prefix, "%", "%%") + "%06d.png",
		Frames:  count,
		FPS:     r.clip.FPS(),
	}, nil
}

// writeFrame renders frame i to its file. It returns a *FrameError for any
// failure that is not a cancelled ctx, and ctx.Err() for that one.
func (r *Renderer) writeFrame(ctx context.Context, rn *run, dir, prefix string, i int) error {
	img, err := r.frameImage(ctx, rn, i)
	if err != nil {
		return r.frameError(ctx, i, err)
	}
	tmp, err := os.CreateTemp(dir, prefix+"*.tmp")
	if err != nil {
		return r.frameError(ctx, i, err)
	}
	tmpName := tmp.Name()
	if err := encodeTo(tmp, img); err != nil {
		_ = os.Remove(tmpName)
		return r.frameError(ctx, i, err)
	}
	final := filepath.Join(dir, fmt.Sprintf("%s%06d.png", prefix, i))
	if err := os.Rename(tmpName, final); err != nil {
		_ = os.Remove(tmpName)
		return r.frameError(ctx, i, err)
	}
	return nil
}

// encodeTo writes img as PNG into f with png.Encode, the encoder
// solidlens.RenderPNG uses, and closes f.
func encodeTo(f *os.File, img image.Image) error {
	w := bufio.NewWriter(f)
	if err := png.Encode(w, img); err != nil {
		_ = f.Close()
		return fmt.Errorf("render: write PNG: %w", err)
	}
	if err := w.Flush(); err != nil {
		_ = f.Close()
		return err
	}
	return f.Close()
}
