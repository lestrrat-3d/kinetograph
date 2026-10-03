package kinetograph

import (
	"context"
	"fmt"
	"math"
	"math/big"
	"math/bits"
	"time"
)

// Clip is a scene sampled at fps frames per second for duration.
type Clip struct {
	scene    *Scene
	fps      int
	duration time.Duration
	count    int
}

// NewClip returns ErrInvalidClip when fps < 1, duration <= 0 or the frame
// count does not fit an int, and ErrNoCamera or ErrEmptyScene for a scene that
// cannot be evaluated. scene MUST NOT be nil.
func NewClip(scene *Scene, fps int, duration time.Duration) (*Clip, error) {
	if fps < 1 {
		return nil, fmt.Errorf("%w: fps %d is below 1", ErrInvalidClip, fps)
	}
	if duration <= 0 {
		return nil, fmt.Errorf("%w: duration %s is not positive", ErrInvalidClip, duration)
	}
	if err := scene.validate(); err != nil {
		return nil, err
	}
	// ceil(duration * fps / 1s), exact in big integers.
	n := new(big.Int).Mul(big.NewInt(int64(duration)), big.NewInt(int64(fps)))
	second := big.NewInt(int64(time.Second))
	n.Add(n, second).Sub(n, big.NewInt(1)).Div(n, second)
	if !n.IsInt64() || n.Int64() > math.MaxInt {
		return nil, fmt.Errorf("%w: %s at %d fps has too many frames", ErrInvalidClip, duration, fps)
	}
	return &Clip{scene: scene, fps: fps, duration: duration, count: int(n.Int64())}, nil
}

// Scene returns the scene the clip samples.
func (c *Clip) Scene() *Scene { return c.scene }

// FPS returns the frame rate in frames per second.
func (c *Clip) FPS() int { return c.fps }

// Duration returns the clip's length.
func (c *Clip) Duration() time.Duration { return c.duration }

// FrameCount is the number of frame times t_i = i/fps with 0 <= t_i <
// duration: ceil(duration · fps / 1s). The interval is half-open, so two
// clips of 1 s at 24 fps concatenate to 48 distinct frames.
func (c *Clip) FrameCount() int { return c.count }

// FrameTime is t_i in exact integer nanoseconds: Duration(i) * Second /
// Duration(fps), rounded down. The product is taken in 128 bits, so it does
// not overflow. An i outside [0, FrameCount()) has no frame; FrameTime then
// returns 0 for a negative i and saturates at the largest Duration when the
// quotient does not fit.
func (c *Clip) FrameTime(i int) time.Duration {
	if i <= 0 {
		return 0
	}
	hi, lo := bits.Mul64(uint64(i), uint64(time.Second))
	fps := uint64(c.fps)
	if hi >= fps {
		return time.Duration(math.MaxInt64)
	}
	q, _ := bits.Div64(hi, lo, fps)
	if q > math.MaxInt64 {
		return time.Duration(math.MaxInt64)
	}
	return time.Duration(q)
}

// Frame evaluates frame i. It returns ErrNilContext for a nil ctx and
// ErrFrameRange when i is outside [0, FrameCount()).
func (c *Clip) Frame(ctx context.Context, i int) (*Frame, error) {
	if ctx == nil {
		return nil, ErrNilContext
	}
	if i < 0 || i >= c.count {
		return nil, fmt.Errorf("%w: frame %d of %d", ErrFrameRange, i, c.count)
	}
	f, err := c.scene.At(ctx, c.FrameTime(i))
	if err != nil {
		return nil, err
	}
	f.Index = i
	return f, nil
}
