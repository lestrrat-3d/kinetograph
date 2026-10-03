package kinetograph_test

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/lestrrat-3d/decad"
	"github.com/lestrrat-3d/decad/decadtest"
	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/sketch"
	"github.com/lestrrat-3d/units"
	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph"
)

// blockBody extrudes the rectangle (x0, y0)-(x1, y1) on a fresh sketch by
// height into a fresh decad document, returning errors rather than failing a
// test so a Builder can call it from any goroutine.
func blockBody(ctx context.Context, x0, y0, x1, y1 float64, height units.Value) (*decad.Body, error) {
	w := sketch.NewWorld()
	s, err := w.CreateSketch(w.XY())
	if err != nil {
		return nil, err
	}
	rect := s.CreateRectangle(x0, y0, x1, y1)
	s.Fix(rect.A)
	if _, err := s.Solve(ctx); err != nil {
		return nil, err
	}
	return decad.New().Extrude(s, s.Profiles()[0], decad.Distance{D: height, Dir: decad.Along})
}

// blockBuilder builds a block centred on the Z axis, params["width"] along X
// and params["depth"] along Y, 10 mm tall. It records every call's params.
type blockBuilder struct {
	mu    sync.Mutex
	calls []kinetograph.Params
	// result, when set, replaces the block as Build's return.
	result func(ctx context.Context) (*decad.Body, error)
}

func (b *blockBuilder) Build(ctx context.Context, p kinetograph.Params) (*decad.Body, error) {
	b.mu.Lock()
	b.calls = append(b.calls, p)
	b.mu.Unlock()
	if b.result != nil {
		return b.result(ctx)
	}
	w, err := p["width"].In(units.Millimeter)
	if err != nil {
		return nil, err
	}
	d, err := p["depth"].In(units.Millimeter)
	if err != nil {
		return nil, err
	}
	return blockBody(ctx, -w/2, -d/2, w/2, d/2, units.Millimeters(10))
}

func (b *blockBuilder) callCount() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.calls)
}

// blockParams is a width channel and a constant 10 mm depth.
func blockParams(width *kinetograph.Channel) map[string]*kinetograph.Channel {
	return map[string]*kinetograph.Channel{
		"width": width,
		"depth": kinetograph.Constant(units.Millimeters(10)),
	}
}

// parametricScene attaches b to node as the part "block", with the camera on
// the root.
func parametricScene(t *testing.T, rig *kinetograph.Rig, node *kinetograph.Node, b kinetograph.Builder,
	params map[string]*kinetograph.Channel) *kinetograph.Scene {
	t.Helper()
	scene := kinetograph.NewScene(rig)
	require.NoError(t, scene.AddParametric("block", node, b, params))
	require.NoError(t, scene.SetCamera(rig.Root(), defaultCamera()))
	return scene
}

func TestAddParametricRefusals(t *testing.T) {
	rig := kinetograph.NewRig()
	other := kinetograph.NewRig()
	b := &blockBuilder{}
	width := kinetograph.Constant(units.Millimeters(10))
	scene := kinetograph.NewScene(rig)

	require.ErrorIs(t, scene.AddParametric("p", rig.Root(), nil, blockParams(width)), kinetograph.ErrNilBuilder)
	require.ErrorIs(t, scene.AddParametric("p", other.Root(), b, blockParams(width)), kinetograph.ErrForeignNode)
	require.ErrorIs(t, scene.AddParametric("p", nil, b, blockParams(width)), kinetograph.ErrForeignNode)

	err := scene.AddParametric("p", rig.Root(), b, blockParams(nil))
	require.ErrorIs(t, err, kinetograph.ErrNilChannel)
	require.ErrorContains(t, err, `"width"`)

	// A length times an angle is a kind with no name and so no text form: it
	// could never be a cache key.
	unnamed, err := units.Millimeters(1).Mul(units.Degrees(1))
	require.NoError(t, err)
	err = scene.AddParametric("p", rig.Root(), b, map[string]*kinetograph.Channel{"odd": kinetograph.Constant(unnamed)})
	require.ErrorIs(t, err, units.ErrUnnamedKind)
	require.ErrorContains(t, err, `"odd"`)

	require.NoError(t, scene.AddPart("rigid", rig.Root(), newBlock(t)))
	require.ErrorIs(t, scene.AddParametric("rigid", rig.Root(), b, blockParams(width)), kinetograph.ErrDuplicateName)
	require.NoError(t, scene.AddParametric("p", rig.Root(), b, blockParams(width)))
	require.ErrorIs(t, scene.AddParametric("p", rig.Root(), b, blockParams(width)), kinetograph.ErrDuplicateName)
	require.ErrorIs(t, scene.AddPart("p", rig.Root(), newBlock(t)), kinetograph.ErrDuplicateName)
	require.Equal(t, 0, b.callCount(), "attaching a part builds nothing")
}

func TestSceneAtBuildsParametricPart(t *testing.T) {
	rig := kinetograph.NewRig()
	slide, err := rig.Root().Prismatic(r3.Vec{X: 1}, mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Millimeters(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Millimeters(40)},
	))
	require.NoError(t, err)
	width := mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Millimeters(10)},
		kinetograph.Keyframe{At: time.Second, Value: units.Millimeters(30)},
	)
	b := &blockBuilder{}
	params := blockParams(width)
	scene := parametricScene(t, rig, slide, b, params)
	// The scene copied the map: a later change to it does not reach the part.
	params["width"] = nil

	at := 500 * time.Millisecond
	f, err := scene.At(t.Context(), at)
	require.NoError(t, err)
	require.Len(t, f.Poses, 1)
	pose := f.Poses[0]
	require.Equal(t, "block", pose.Name)
	require.Len(t, pose.Params, 2)
	require.True(t, pose.Params["width"].Equal(units.Millimeters(20), 0), "width %s", pose.Params["width"])
	require.True(t, pose.Params["depth"].Equal(units.Millimeters(10), 0), "depth %s", pose.Params["depth"])
	// The body was built from those values: 20 x 10 x 10 mm about the Z axis.
	decadtest.MeasuresBounds(t, pose.Body, r3.Vec{X: -10, Y: -5}, r3.Vec{X: 10, Y: 5, Z: 10})
	want, err := slide.World(at)
	require.NoError(t, err)
	require.True(t, pose.Transform.Equal(want, 0))

	require.Equal(t, 1, b.callCount())
	require.True(t, b.calls[0]["width"].Equal(units.Millimeters(20), 0))

	// At has no cache of its own: a second call at the same time builds again.
	again, err := scene.At(t.Context(), at)
	require.NoError(t, err)
	require.Equal(t, 2, b.callCount())
	require.NotSame(t, pose.Body, again.Poses[0].Body)
}

func TestAtCachedBuildsOncePerTuple(t *testing.T) {
	rig := kinetograph.NewRig()
	// 10 mm held until 500 ms, then 10 to 20 mm by 1 s.
	width := mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Millimeters(10)},
		kinetograph.Keyframe{At: 500 * time.Millisecond, Value: units.Millimeters(10)},
		kinetograph.Keyframe{At: time.Second, Value: units.Millimeters(20)},
	)
	depth := mustChannel(t,
		kinetograph.Keyframe{At: time.Second, Value: units.Millimeters(10)},
		kinetograph.Keyframe{At: 2 * time.Second, Value: units.Millimeters(30)},
	)
	b := &blockBuilder{}
	scene := parametricScene(t, rig, rig.Root(), b, map[string]*kinetograph.Channel{"width": width, "depth": depth})
	cache := kinetograph.NewBuildCache()

	// Sixteen goroutines over four times that share one tuple (10 mm, 10 mm).
	times := []time.Duration{0, 100 * time.Millisecond, 400 * time.Millisecond, 500 * time.Millisecond}
	bodies := make([]*decad.Body, 16)
	errs := make([]error, 16)
	var wg sync.WaitGroup
	for g := range bodies {
		wg.Go(func() {
			f, err := scene.AtCached(t.Context(), times[g%len(times)], cache)
			errs[g] = err
			if err == nil {
				bodies[g] = f.Poses[0].Body
			}
		})
	}
	wg.Wait()
	for g := range bodies {
		require.NoError(t, errs[g])
		require.Same(t, bodies[0], bodies[g], "goroutine %d", g)
	}
	require.Equal(t, 1, b.callCount())

	// A new width is a new tuple, and so is a new depth with the width held.
	at1, err := scene.AtCached(t.Context(), time.Second, cache)
	require.NoError(t, err)
	require.Equal(t, 2, b.callCount())
	at2, err := scene.AtCached(t.Context(), 2*time.Second, cache)
	require.NoError(t, err)
	require.Equal(t, 3, b.callCount())
	require.NotSame(t, at1.Poses[0].Body, at2.Poses[0].Body)
	decadtest.MeasuresBounds(t, at2.Poses[0].Body, r3.Vec{X: -10, Y: -15}, r3.Vec{X: 10, Y: 15, Z: 10})

	// Clip.FrameCached reaches the same cache.
	clip, err := kinetograph.NewClip(scene, 4, time.Second)
	require.NoError(t, err)
	f, err := clip.FrameCached(t.Context(), 1, cache) // 250 ms, the first tuple
	require.NoError(t, err)
	require.Equal(t, 1, f.Index)
	require.Same(t, bodies[0], f.Poses[0].Body)
	require.Equal(t, 3, b.callCount())
}

func TestSceneAtBuildFailures(t *testing.T) {
	errBuild := errors.New("cannot build")
	for _, tc := range []struct {
		name   string
		result func(ctx context.Context) (*decad.Body, error)
		want   error
	}{
		{"builder error", func(context.Context) (*decad.Body, error) { return nil, errBuild }, errBuild},
		{"nil body", func(context.Context) (*decad.Body, error) {
			return nil, nil //nolint:nilnil // a Builder breaking its contract is the case under test
		}, kinetograph.ErrNilBody},
	} {
		t.Run(tc.name, func(t *testing.T) {
			rig := kinetograph.NewRig()
			b := &blockBuilder{result: tc.result}
			scene := parametricScene(t, rig, rig.Root(), b, blockParams(kinetograph.Constant(units.Millimeters(10))))
			cache := kinetograph.NewBuildCache()

			_, err := scene.AtCached(t.Context(), 0, cache)
			require.ErrorIs(t, err, tc.want)
			require.ErrorContains(t, err, `"block"`)

			// The cache keeps the failure: the same tuple fails again without
			// another Build.
			_, err = scene.AtCached(t.Context(), time.Second, cache)
			require.ErrorIs(t, err, tc.want)
			require.ErrorContains(t, err, "at 1s")
			require.Equal(t, 1, b.callCount())
		})
	}
}

func TestSceneAtBuildCancelled(t *testing.T) {
	rig := kinetograph.NewRig()
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	b := &blockBuilder{result: func(ctx context.Context) (*decad.Body, error) {
		cancel()
		return nil, ctx.Err()
	}}
	scene := parametricScene(t, rig, rig.Root(), b, blockParams(kinetograph.Constant(units.Millimeters(10))))
	cache := kinetograph.NewBuildCache()

	_, err := scene.AtCached(ctx, 0, cache)
	require.Equal(t, context.Canceled, err, "ctx.Err() comes back unwrapped")

	// The cancelled build was not kept: a live context builds again.
	b.result = nil
	f, err := scene.AtCached(t.Context(), 0, cache)
	require.NoError(t, err)
	require.NotNil(t, f.Poses[0].Body)
	require.Equal(t, 2, b.callCount())
}

func TestSceneParts(t *testing.T) {
	rig := kinetograph.NewRig()
	b := &blockBuilder{}
	body := newBlock(t)
	scene := kinetograph.NewScene(rig)
	require.NoError(t, scene.AddParametric("grows", rig.Root(), b, blockParams(kinetograph.Constant(units.Millimeters(10)))))
	require.NoError(t, scene.AddPart("still", rig.Root(), body))

	require.Equal(t, []kinetograph.PartInfo{
		{Name: "grows", Parametric: true},
		{Name: "still", Body: body},
	}, scene.Parts())
	require.Equal(t, 0, b.callCount())
}
