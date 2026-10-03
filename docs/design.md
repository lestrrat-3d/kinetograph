# kinetograph design

kinetograph animates solids built with [decad](https://github.com/lestrrat-3d/decad) and renders every frame of
the animation to a numbered PNG with [solidlens](https://github.com/lestrrat-3d/solidlens). A video tool such as
ffmpeg assembles the PNGs into a clip; kinetograph writes no video itself.

This document is the contract the implementation is built from. §5 states the public API as Go signatures: §5.1 to
§5.6 are the initial pass, §5.7 and §5.8 are pass 2 (reshape). §9 states which pass adds what. A later pass extends
this document before it extends the code.

## 1. What kinetograph is

A caller describes a scene in Go: decad bodies attached to a tree of joints, one camera attached to the same tree,
and scalar channels (an angle, a slide distance, a field of view) that change with time through keyframes and
easing. kinetograph evaluates the scene at each frame time, poses every body and the camera with `r3`, and hands
the posed triangles to solidlens.

The first clip it produces is a landing-page video for decad: decad parts turning, sliding apart and assembling,
seen from a moving camera. The long-term shape is a toolkit that also re-shapes a body over time, by rebuilding a
decad body from parameters that change with the frame (§9, pass 2).

kinetograph is not a renderer, a video encoder, a physics engine or a CAD kernel. Rendering is solidlens's,
geometry is decad's, coordinate math is `r3`'s and quantities are `units`'s.

## 2. Layering

```
kinetograph/render   solidlens scenes, PNG sequence writer       (imports solidlens)
  |
kinetograph          channels, rig, camera, scene, clip         (imports decad, r3, units)
  |
kinetograph/internal/memo  run-once-per-key cache          (imports the standard library only)
  |
decad                3D bodies and Body.Tessellate              github.com/lestrrat-3d/decad
  |
r3                   Vec, Frame, Transform                       github.com/lestrrat-3d/r3
  |
units                typed quantities                            github.com/lestrrat-3d/units
```

The arrows point down and never back up. `render` is the only package that imports solidlens; the root package
imports decad, `r3` and `units` and knows nothing about pixels. `internal/memo` is imported by the root package and
by `render`, and imports neither. decad, solidlens, `r3` and `units` never import kinetograph.

## 3. Architecture

The four layers the design work agreed on survive, with one refinement each.

| Layer | Owns | Refinement |
|---|---|---|
| Timeline | `Channel`: keyframes of one `units.Kind`, easing between them, `At(t)` | Every animated quantity is a scalar channel (D1); a camera orbit or dolly is a joint (D5) |
| Rig | `Rig`/`Node`: a joint tree; a joint value becomes an `r3.Transform`, composed with the parent's | Bodies AND the camera attach to nodes; the tree is built parent to child, so no cycle can be written |
| Reshape | a body as a Go function of parameters, rebuilt when they change, cached by parameter values | pass 2 (§5.7): a cache lives in one render call, never in the `Renderer`; a failed rebuild stops the sequence (§6) |
| Output | `render`: one solidlens scene per frame, one PNG per frame | a rigid pose is applied to the tessellated vertices, never by re-placing the decad body (D2) |

Evaluation of one frame runs top to bottom:

1. `Clip.FrameTime(i)` gives the frame's time `t` as an exact `time.Duration` (§7).
2. Every `Channel` the rig reads evaluates `At(t)`: find the keyframe segment, map `t` to `u` in `[0, 1]`, ease
   `u`, and interpolate the two keyframe values with `units.Value` arithmetic.
3. Every `Node` builds its local transform from its channel value with `r3.RotationAround` or `r3.Translation`,
   and composes it with its parent's world transform with `Transform.Then`.
4. `Scene.At` collects one `Pose` per part (its body and its node's world transform) and the camera's world
   position, target, up and field of view into a `Frame`. For a parametric part (§5.7) it first evaluates the
   part's parameter channels at `t` and takes the body from the part's `Builder`, through a `BuildCache` that
   calls `Build` once per distinct parameter tuple.
5. `render.Renderer` maps each `Pose` to a solidlens `Model` whose vertices are the part's tessellated vertices
   under `Transform.Apply`, builds the solidlens `Camera`, adds the style's lights and background, and calls
   `solidlens.RenderPNG`. A rebuilt body is tessellated once per distinct body in the call that rendered it.

## 4. Decisions

### D1. Every animated quantity is a scalar channel; rotations come from an axis and an angle

A `Channel` holds keyframes of one `units.Kind`. A revolute joint reads an `Angle` channel and builds its transform
with `r3.RotationAround(center, axis, angle)`; a prismatic joint reads a `Length` channel and builds
`r3.Translation(dir.Scale(d))`. Interpolation is therefore scalar easing between two `units.Value`s, and no
orientation is ever interpolated: there is no quaternion, no slerp and no rotation matrix in kinetograph.

What this gives up: a body that tumbles about a changing axis needs two or more revolute joints in a chain, one per
axis. That is how a real mechanism moves, and it is the clip the landing page wants.

`r3` has every operation this needs: `RotationAround`, `Translation`, `Then`, `Apply`, `ApplyDir`,
`Vec.Normalize`, `Vec.Scale`. §11 records what would be convenient but is not needed.

### D2. A rigid pose is applied to the tessellated mesh, not to the decad body

Each part is tessellated once per `(body, chord tolerance)` with `Body.Tessellate(ctx, chord,
decad.WithVerification(decad.VerifyNone))`, exactly as decad's `_gallery` does: the mesh is drawn, never proven,
and `VerifyNone` skips the facet-contact audit whose work ceiling a fine chord can otherwise hit. Per frame, the
part's world transform is applied to the held vertices with `r3.Transform.Apply`; the triangle indices never
change.

`Body.Placed` retires its receiver and re-evaluates the payload, and `Body.PlacedCopy` re-evaluates and would
re-tessellate per frame. A rigid motion of the vertices is the same geometry up to float rounding, and costs one
`Apply` per vertex. decad's "never expose triangles as the representation" invariant (`docs/api-design.md` §3) binds
decad's own API; a consumer reading `Mesh.Vertices()` and `Mesh.Triangles()` as a `solidlens.TriangleSource` is the
use decad built that output for.

A parametric part (§5.7) is the one part whose body changes between frames, and only when its parameter values
change. Each body its `Builder` returns is tessellated once in the render call that needs it, and the part's rigid
pose then moves that mesh's vertices the same way.

A reflection (`Transform.IsReflection()`) would turn every triangle inside out. `Node.Fixed` refuses one with
`ErrReflection`; a revolute or prismatic joint cannot produce one.

### D3. Time is `time.Duration`; a frame rate is an integer frames per second

`units.Kind` has length, mass and angle dimensions and no time dimension (its doc: "there is no time, current or
temperature, because this is a geometry library"). Rather than ask `units` for a time dimension, kinetograph uses
the standard library's typed `time.Duration` for every time: keyframe times, clip duration, frame times. A frame
rate is an `int` count of frames per second. Neither is a bare `float64`, and frame times are computed in integer
nanoseconds (§7).

### D4. decad's no-bare-float64 rule applies to the public API

Every scalar quantity crossing kinetograph's public API is a `units.Value`: a keyframe value, a joint angle, a slide
distance, a field of view, a chord tolerance. A `units.Value` of the wrong `Kind` is `ErrKind`, never a coercion.

The carve-outs are decad's (`docs/api-design.md` §5.2) plus two of kinetograph's own, and nothing else:

| Bare value | Where | Why it is not a scalar quantity |
|---|---|---|
| `r3.Vec` | joint centers and axes, camera position/target/up | a coordinate in millimetres by convention, or a dimensionless direction |
| `u float64` in `Easing.Ease(u)` | the easing seam | a normalized position in a keyframe segment, decad's curve-parameter carve-out |
| `int` | frames per second, frame index, frame count, pixel width and height | counts |
| solidlens's own types | `render.Style` | `solidlens.Color`, `Material`, light `Intensity`, `Edges.Width` are the renderer's appearance values, typed by solidlens; they stay inside `render` |

solidlens's `Camera.FOV` is a `float64` in degrees. kinetograph's field of view is an `Angle` channel, and `render`
converts it with `Value.In(units.Degree)` at the one seam that hands solidlens a camera.

### D5. The camera is a rig attachment

A `Camera` is a node-local position, target and up vector plus a field-of-view channel, attached to a `Node` like a
part. Its world pose is the node's world transform applied to those vectors (`Apply` for the two points, `ApplyDir`
for up). A camera orbit is a revolute node whose axis passes through the target; a dolly is a prismatic node; a
crane is a chain of both. This is what makes D1 true for the camera with no camera-specific interpolation.
There is no dedicated orbit-camera type: an orbit is a revolute node, and a helper that hid that would be a second
way to say the same thing.

### D6. The renderer is isolated in `render`

The root package produces a `Frame`: poses as `r3.Transform`s and a camera pose as vectors and an angle. It is
testable without a renderer, and nothing in it names a color or a pixel. `render` turns a `Frame` into a
solidlens scene. Appearance (materials, back materials, edges, lights, background) is `render.Style`'s. In pass 1,
lights are static: they are listed in `Style` and do not move with the clip.

Pass 3 (§9) attaches lights to nodes. The root package then names a light only as a kind and a node-local position
or direction, which `Frame` carries as a world pose. A light's color and its intensity over time, and a part's
fade over time, are `render.Style`'s channels, evaluated in `render`.

### D7. Output is a numbered PNG sequence

`render.Renderer.Sequence` writes `frame_000000.png`, `frame_000001.png`, … into a directory and reports the
pattern, count and frame rate. The caller assembles the video:

```
ffmpeg -framerate 24 -i frame_%06d.png -c:v libx264 -pix_fmt yuv420p clip.mp4
```

kinetograph adds no Go video encoder. `yuv420p` needs an even width and height; kinetograph does not enforce that,
because it is a property of one codec, but the README states it beside the command.

### D8. A failing frame stops the sequence

A frame that cannot be evaluated or rendered — a channel arithmetic overflow, a camera field of view that reaches
solidlens's 179 degree limit at some time, a part whose rebuild fails (pass 2) — stops `Sequence` and returns a
`*render.FrameError` carrying the frame index, the frame time and the wrapped cause. No frame is skipped and no
later frame is written after the failing one is detected (§6).

### D9. Same input gives byte-identical PNGs on one toolchain and architecture

§7 states the rules that keep this true and the one limit (FMA contraction differs between amd64 and arm64).

### D10. `sketch` is a test-and-example-only dependency

decad bodies are built from `sketch` profiles, so kinetograph's tests and examples import
`github.com/lestrrat-3d/sketch` to build the bodies they animate, and `go.mod` requires it. No production package
imports it: the root package takes a finished `*decad.Body`, and `render` takes a `Frame`.

### D11. The landing-page clip is a nested module

The clip program lives in `_clips/decad-landing/` with its own `go.mod`, modeled on decad's `_gallery/` (§9,
pass 4). The `_` prefix keeps it out of the root module, its tests and its linter, so scene content never joins the
library.

`_clips/demo/` is a second clip program of the same shape. It renders a 4.5 s clip that uses all three joint kinds
and a camera orbit: a drilled plate on `Fixed`, two pins that drop into its bolt holes on `Prismatic` joints, and a
ring that rises on a `Prismatic` joint and turns half a turn on a `Revolute` joint under it.

## 5. Public API

Signatures are normative; doc comments on the implementation carry the detail. Every constructor validates what
it is handed and returns an error from §6's vocabulary rather than a value that would fail later. §5.1 to §5.6 are
the initial pass; §5.7 and §5.8 are pass 2, which adds declarations and changes no initial-pass signature.

### 5.1 `channel.go`: channels, keyframes and easing

```go
// Easing maps a normalized segment position u in [0, 1] to an eased position. Ease(0) is 0 and Ease(1) is 1; a
// value outside [0, 1] between them is allowed (overshoot).
type Easing interface {
    Ease(u float64) float64
}

// The provided easings. Linear is the default for a Keyframe whose Ease is nil.
var (
    Linear     Easing // u
    SmoothStep Easing // 3u² − 2u³
    EaseIn     Easing // u³
    EaseOut    Easing // 1 − (1 − u)³
    EaseInOut  Easing // cubic, symmetric about u = 1/2
)

// Keyframe is one value at one time. Ease governs the segment that ENDS at this keyframe; it is ignored on the
// first keyframe of a channel.
type Keyframe struct {
    At    time.Duration
    Value units.Value
    Ease  Easing
}

// Channel is an immutable scalar function of time: the keyframes' kind, held before the first keyframe and
// after the last, interpolated between neighbours as a + (b − a) · Ease(u).
type Channel struct { /* unexported */ }

// NewChannel returns a channel over keys. It returns ErrNoKeyframes for no keys, ErrKeyframeOrder when the times
// are not strictly increasing, ErrKind when two keyframes differ in Kind, and ErrNotFinite for a non-finite value.
func NewChannel(keys ...Keyframe) (*Channel, error)

// Constant returns a channel that is v at every time.
func Constant(v units.Value) *Channel

// Kind returns the kind every value of the channel carries.
func (c *Channel) Kind() units.Kind

// At returns the channel's value at t. It returns units.ErrNotFinite when the interpolation overflows.
func (c *Channel) At(t time.Duration) (units.Value, error)
```

Interpolation is `units.Value` arithmetic, so a mixed-kind operand is impossible by construction and an overflow
is an error rather than an infinity: `d, err := b.Sub(a)`; `v, err := a.Add(d.Scale(e))`.

### 5.2 `rig.go`: the joint tree

```go
// Rig is a tree of joints. The root node is the world frame.
type Rig struct { /* unexported */ }

func NewRig() *Rig
func (r *Rig) Root() *Node

// Node is one joint. A node is created only through its parent, so the tree has no cycles.
type Node struct { /* unexported */ }

// Fixed adds a child whose local transform is the constant t. It returns ErrReflection when t.IsReflection(), and
// ErrInvalidTransform when !t.IsValid() (the zero Transform{} among them).
func (n *Node) Fixed(t r3.Transform) (*Node, error)

// Revolute adds a child that rotates about the axis through center along axis by angle.At(t). It returns ErrKind
// unless angle.Kind() == units.Angle, and r3.ErrDegenerateAxis for a zero axis.
func (n *Node) Revolute(center, axis r3.Vec, angle *Channel) (*Node, error)

// Prismatic adds a child that slides along the unit direction of dir by distance.At(t). It returns ErrKind unless
// distance.Kind() == units.Length, and ErrDegenerateDirection when dir has no direction.
func (n *Node) Prismatic(dir r3.Vec, distance *Channel) (*Node, error)

// Local returns the joint's own transform at t.
func (n *Node) Local(t time.Duration) (r3.Transform, error)

// World returns the transform from this node's frame to the world frame at t: Local(t).Then(parent.World(t)),
// with the root's World the identity.
func (n *Node) World(t time.Duration) (r3.Transform, error)
```

`Prismatic` normalizes `dir` once at construction with `Vec.Normalize`, and `Local` is
`r3.Translation(unit.Scale(d))` where `d` is `distance.At(t)` expressed in millimetres (`Value.In(units.Millimeter)`).
`Revolute`'s `Local` is `r3.RotationAround(center, axis, angle)`; the axis is validated at construction by building
`r3.Rotation(axis, units.Radians(0))` and surfacing its error.

### 5.3 `camera.go` and `scene.go`: parts, camera, scene

```go
// Camera is a perspective camera in its node's frame. FOV is the vertical field of view, an Angle channel.
type Camera struct {
    Position r3.Vec
    Target   r3.Vec
    Up       r3.Vec
    FOV      *Channel
}

// Scene is the set of parts and the camera, each attached to a node of one rig. Build it completely before
// evaluating it; At is safe to call concurrently, AddPart, AddParametric and SetCamera are not safe beside At.
type Scene struct { /* unexported */ }

func NewScene(rig *Rig) *Scene

// AddPart attaches body to node under name. It returns ErrDuplicateName for a name already added, ErrForeignNode
// for a node of another rig, and ErrNilBody for a nil body.
func (s *Scene) AddPart(name string, node *Node, body *decad.Body) error

// SetCamera attaches cam to node, replacing any earlier camera. It returns ErrForeignNode for a node of another
// rig, ErrKind unless cam.FOV.Kind() == units.Angle, and ErrNilChannel for a nil FOV.
func (s *Scene) SetCamera(node *Node, cam Camera) error

// Pose is one part at one time.
type Pose struct {
    Name      string
    Body      *decad.Body
    Transform r3.Transform // part frame -> world
    Params    Params       // the values Body was built from; nil for a part AddPart attached (§5.7)
}

// CameraPose is the camera at one time, in world coordinates.
type CameraPose struct {
    Position r3.Vec
    Target   r3.Vec
    Up       r3.Vec
    FOV      units.Value
}

// Frame is the scene evaluated at one time. Poses are in AddPart order.
type Frame struct {
    Index  int
    Time   time.Duration
    Poses  []Pose
    Camera CameraPose
}

// At evaluates the scene at t. ctx is checked once on entry and returned as ctx.Err() when done, and is handed to
// every Builder (§5.7). It returns ErrNoCamera when SetCamera was never called and ErrEmptyScene when no part was
// added.
func (s *Scene) At(ctx context.Context, t time.Duration) (*Frame, error)
```

`Frame.Index` is `-1` from `Scene.At`; `Clip.Frame` fills it in. When ctx is done after any part or the camera
failed to evaluate, `At` returns `ctx.Err()` unwrapped instead of that failure.

### 5.4 `clip.go`: frame rate and duration

```go
// Clip is a scene sampled at fps frames per second for duration.
type Clip struct { /* unexported */ }

// NewClip returns ErrInvalidClip when fps < 1 or duration <= 0, and ErrNoCamera/ErrEmptyScene for a scene that
// cannot be evaluated.
func NewClip(scene *Scene, fps int, duration time.Duration) (*Clip, error)

func (c *Clip) Scene() *Scene
func (c *Clip) FPS() int
func (c *Clip) Duration() time.Duration

// FrameCount is the number of frame times t_i = i/fps with 0 <= t_i < duration: ceil(duration · fps / 1s).
func (c *Clip) FrameCount() int

// FrameTime is t_i in exact integer nanoseconds: Duration(i) * Second / Duration(fps), rounded down.
func (c *Clip) FrameTime(i int) time.Duration

// Frame evaluates frame i. It returns ErrFrameRange when i is outside [0, FrameCount()).
func (c *Clip) Frame(ctx context.Context, i int) (*Frame, error)
```

`FrameCount` uses the half-open interval so two clips of 1 s at 24 fps concatenate to 48 distinct frames.

### 5.5 `render`: solidlens scenes and the PNG sequence

```go
package render

// Appearance is how one part is drawn. Back, when non-nil, shades the inner side of an open mesh (a decad sheet
// body). Edges is solidlens's edge-line style; its zero value draws no lines.
type Appearance struct {
    Material solidlens.Material
    Back     *solidlens.Material
    Edges    solidlens.Edges
}

// Style is everything about a clip that is not geometry or motion.
type Style struct {
    Width, Height     int
    Chord             units.Value // tessellation tolerance, a Length
    Background        solidlens.Color
    Default           Appearance
    Parts             map[string]Appearance // by part name; absent names use Default
    DirectionalLights []solidlens.DirectionalLight
    PointLights       []solidlens.PointLight
}

// Renderer holds a clip, its style and every part's tessellation. It is immutable after New and safe to use from
// several goroutines.
type Renderer struct { /* unexported */ }

// New tessellates every part AddPart attached at style.Chord, reading the parts from Scene.Parts; it evaluates no
// frame and calls no Builder. It returns ErrStyle for a non-positive Width or Height or a Parts name that no part
// carries, and ErrKind for a Chord that is not a Length; decad's own tolerance errors pass through unchanged.
// Tessellation errors name the part.
func New(ctx context.Context, clip *kinetograph.Clip, style Style) (*Renderer, error)

// Frame renders frame i into a new image. It builds and tessellates each parametric part once for this call. It
// returns a *FrameError wrapping the cause.
func (r *Renderer) Frame(ctx context.Context, i int) (*image.RGBA, error)

// Sequence renders every frame of the clip to dir as PNG files and returns what a video tool needs to assemble
// them. It creates dir if needed. It returns the *FrameError of the lowest-index frame that failed, or ctx.Err().
func (r *Renderer) Sequence(ctx context.Context, dir string, opts ...SequenceOption) (*Sequence, error)

// Sequence describes a written frame sequence.
type Sequence struct {
    Dir     string
    Pattern string // the printf pattern of the file names, e.g. "frame_%06d.png"
    Frames  int
    FPS     int
}

// WithWorkers renders up to n frames concurrently (default 1). WithPrefix changes the file name prefix
// (default "frame_"). Both use github.com/lestrrat-go/option/v3.
func WithWorkers(n int) SequenceOption
func WithPrefix(prefix string) SequenceOption

// FrameError is a failure at one frame. Unwrap returns Err.
type FrameError struct {
    Index int
    Time  time.Duration
    Err   error
}
```

`Frame` builds a `solidlens.Scene` whose `Models` are in `Pose` order; each model's `Mesh` is a private
`TriangleSource` holding the part's triangle indices and its vertices under `Pose.Transform.Apply`. The camera is
`solidlens.Camera{Position, Target, Up, FOV: fov.In(units.Degree)}`.

Each frame file is written to a temporary name in `dir` and renamed into place once `RenderPNG` returns, so a
cancelled or failed run leaves no partial frame file.

A `Style.Parts` name may name a parametric part; its `Appearance` then draws every body that part's `Builder`
returns. How `Frame` and `Sequence` cache rebuilt bodies and their meshes is §5.7.

### 5.6 Executable example

`examples/kinetograph_sequence_example_test.go` builds a decad block with `sketch` and `decad` directly (an `Example`
has no `testing.TB`, which `decadtest.NewBlock` needs; tests use `decadtest.NewBlock`), attaches it to a
revolute joint turning 90° over one second, puts the camera on a second revolute joint orbiting the origin, renders
a 4-frame clip at 24 fps into a temporary directory, and prints the frame count, the file names and the sequence
pattern as its `// Output:` block. It is the end-to-end instance the initial pass is accepted on.

### 5.7 `reshape.go`: parametric parts (pass 2)

```go
// Params is the values of a parametric part's parameter channels at one time, by parameter name.
type Params map[string]units.Value

// Builder builds a part's body from parameters. Build MUST return equal bodies for equal params: it reads params
// and nothing else that changes (no clock, no randomness, no shared mutable state). Build MUST be safe to call
// from several goroutines at once. params is a new map on every call, and Build may keep it.
type Builder interface {
    Build(ctx context.Context, params Params) (*decad.Body, error)
}

// AddParametric attaches a part whose body at time t is b.Build(ctx, p), where p[name] is params[name].At(t) for
// every name in params. It returns ErrNilBuilder for a nil b, ErrForeignNode for a node of another rig,
// ErrNilChannel for a nil channel (naming the parameter), an error wrapping units.ErrUnnamedKind or
// units.ErrOverflowedKind for a keyframe value that has no text form, and ErrDuplicateName for a name already
// added. An empty params is allowed: the part then has one body per BuildCache.
func (s *Scene) AddParametric(name string, node *Node, b Builder, params map[string]*Channel) error

// BuildCache holds the bodies Builders returned, by part and parameter tuple. It is safe for concurrent use, and
// it keeps every body it built until the cache itself is dropped.
type BuildCache struct { /* unexported */ }

func NewBuildCache() *BuildCache

// AtCached evaluates the scene at t as At does, taking each parametric part's body from cache. cache MUST NOT be
// nil.
func (s *Scene) AtCached(ctx context.Context, t time.Duration, cache *BuildCache) (*Frame, error)

// FrameCached evaluates frame i as Frame does, through cache. cache MUST NOT be nil.
func (c *Clip) FrameCached(ctx context.Context, i int, cache *BuildCache) (*Frame, error)

// PartInfo is one part as AddPart or AddParametric attached it.
type PartInfo struct {
    Name       string
    Body       *decad.Body // the body AddPart attached; nil for a parametric part
    Parametric bool
}

// Parts returns the scene's parts in the order they were added. It evaluates nothing and calls no Builder.
func (s *Scene) Parts() []PartInfo
```

`Scene.At(ctx, t)` is `AtCached(ctx, t, NewBuildCache())`, and `Clip.Frame` is `FrameCached` with a new cache, so
each call of either builds every parametric part once. In the `Frame` they return, a parametric part's `Pose.Body`
is the body built for that time, `Pose.Transform` is its node's `World(t)`, and `Pose.Params` is a new map holding
the values the body was built from.

**Cache key.** A `BuildCache` entry is keyed by the part and by the text of its parameter values: each value's
`Value.MarshalText()`, in parameter-name order, joined by `"\n"`. `AddParametric` sorts the names once with
`slices.Sorted` and keeps them as a slice, so evaluation never iterates the `params` map. `MarshalText` writes only
printable ASCII, so a value's text holds no `"\n"` and the join is unambiguous; the names are not in the key because
one part's set of names is fixed. `MarshalText` round-trips exactly, so equal text means an equal unit and an equal
magnitude: a cache hit never returns a body built from different values. Equal quantities in different units
(`10 mm` and `1 cm`) have different text and are built twice.

**Once per tuple.** The first caller for a key calls `Build`; a caller that asks for the same key while that call
runs waits for it and receives its result. `Build` therefore runs once per distinct (part, tuple) per cache,
whatever the number of goroutines sharing the cache. A failed `Build` is kept: a later frame with the same tuple
returns the same error and `Build` is not called again. The one exception is a `Build` that fails while the
caller's ctx is done: that entry is dropped, and a waiting caller whose ctx is still live calls `Build` itself, so
one caller's cancellation never becomes another caller's error.

**Errors at a frame.** `AtCached` wraps every failure with the part's name and the time: a parameter channel's
`At` error, a `MarshalText` error, the `Builder`'s own error (`errors.Is` reaches it), and `ErrNilBody` when `Build`
returns a nil body and a nil error. When ctx is done after any failure, `AtCached` returns `ctx.Err()` unwrapped.

**Rendering.** A `Renderer` holds no cache and stays immutable. `render.New` reads the parts with `Scene.Parts`,
tessellates the bodies `AddPart` attached, and calls no `Builder`, so a `Build` failure at frame 0 is a
`*FrameError` from `Frame` or `Sequence`, never an error from `New`. `Renderer.Frame` makes a new `BuildCache` and a
new mesh cache for its one call. `Renderer.Sequence` makes one `BuildCache` and one mesh cache per call, shares them
between its workers, and drops them when it returns, so it holds every rebuilt body and mesh of the run until then.
The mesh cache is keyed by the `*decad.Body` the `BuildCache` returned and tessellates each body once with
`decad.VerifyNone`, with the same run-once-per-key rule.

`Sequence` maps a failure at frame k to `*FrameError{Index: k, Time: t_k, Err: cause}`: a `Build` error, or a
tessellation error naming the part. Every frame whose tuple failed fails with the same cause, and the lowest index
is the one reported (§6). Which worker calls `Build` for a tuple depends on scheduling; the bytes do not, because a
`Builder` returns equal bodies for equal `Params` and decad tessellates an equal payload to an equal mesh (§7).

A `Builder` should create its own `decad.New()` document on every call, as decad's `_gallery` does, so rebuilt bodies do
not accumulate in one document and concurrent calls share no document.

### 5.8 Executable example, pass 2

`examples/kinetograph_reshape_example_test.go` defines a `Builder` that extrudes a W × 20 mm rectangle 10 mm tall,
with W a `Length` channel that holds 10 mm from 0 to 250 ms and rises linearly to 30 mm at 750 ms. It renders the
4-frame clip at 4 fps (widths 10, 10, 20 and 30 mm) with one worker into a temporary directory. The `Builder` prints
each width it is asked to build, so the `// Output:` block shows three builds for four frames, followed by the frame
count and the file names. It is the end-to-end instance pass 2 is accepted on.

## 6. Error behaviour

Sentinels live in `errors.go`. Every error a constructor returns wraps one of them, so `errors.Is` branches; the
message names the offending argument.

| Condition | Error | Where |
|---|---|---|
| no keyframes | `ErrNoKeyframes` | `NewChannel` |
| keyframe times not strictly increasing | `ErrKeyframeOrder` | `NewChannel` |
| keyframes of two kinds; a channel of the wrong kind for its joint or the camera | `ErrKind` | `NewChannel`, `Revolute`, `Prismatic`, `SetCamera`, `render.New` (chord) |
| non-finite keyframe value | `units.ErrNotFinite` (wrapped) | `NewChannel` |
| interpolation overflows | `units.ErrNotFinite` (wrapped) | `Channel.At`, surfaced by `Node.World`, `Scene.At` |
| fixed transform is a reflection | `ErrReflection` | `Node.Fixed` |
| fixed transform is not a rigid motion (`!IsValid()`) | `ErrInvalidTransform` | `Node.Fixed` |
| zero or non-finite revolute axis | `r3.ErrDegenerateAxis` (passed through) | `Revolute` |
| zero or non-finite prismatic direction | `ErrDegenerateDirection` | `Prismatic` |
| transform composition overflows | `r3.ErrNonFinite` / `r3.ErrNotOrthonormal` (passed through) | `Node.World`, surfaced by `Scene.At` |
| part name already used | `ErrDuplicateName` | `AddPart`, `AddParametric` |
| node belongs to another rig | `ErrForeignNode` | `AddPart`, `AddParametric`, `SetCamera` |
| nil body, nil channel | `ErrNilBody`, `ErrNilChannel` | `AddPart`, `SetCamera`, `Revolute`, `Prismatic`, `AddParametric` (a nil parameter channel) |
| nil builder | `ErrNilBuilder` | `AddParametric` |
| parameter value with no text form | `units.ErrUnnamedKind` / `units.ErrOverflowedKind` (wrapped) | `AddParametric` (keyframe values), `Scene.At` (an evaluated value) |
| `Build` fails | the `Builder`'s error, wrapped with the part name and time | `Scene.At`, `Scene.AtCached`, `Clip.Frame`, `Clip.FrameCached` |
| `Build` returns a nil body and a nil error | `ErrNilBody` (wrapped with the part name and time) | `Scene.At`, `Scene.AtCached`, `Clip.Frame`, `Clip.FrameCached` |
| no camera, no parts | `ErrNoCamera`, `ErrEmptyScene` | `Scene.At`, `NewClip` |
| fps < 1 or duration <= 0 | `ErrInvalidClip` | `NewClip` |
| frame index outside the clip | `ErrFrameRange` | `Clip.Frame`, `Renderer.Frame` |
| nil context | `ErrNilContext` | every function taking one, before any work |
| style has a non-positive dimension or an unknown part name | `render.ErrStyle` | `render.New` |
| tessellation fails | decad's error, wrapped with the part name | `render.New`; for a rebuilt body, inside the frame's `FrameError` |
| anything at frame i (evaluation, `Build`, tessellation of a rebuilt body, solidlens refusal, file I/O) | `*render.FrameError{Index: i, Time: t_i, Err: cause}` | `Renderer.Frame`, `Renderer.Sequence` |
| cancelled | `ctx.Err()` unchanged, never wrapped in a `FrameError` | everywhere |

`Sequence` with several workers may see several frames fail before it stops; it returns the `FrameError` with the
lowest `Index`, so the reported frame does not depend on scheduling. A frame that failed has no file in `dir`;
frames with a lower index that succeeded keep theirs. A `BuildCache` keeps a failed `Build` (§5.7), so every frame
whose parameter tuple failed reports the same cause.

## 7. Determinism

The claim: the same scene, style and clip produce byte-identical PNG files on the same Go toolchain and the same
`GOARCH`. The rules that keep it:

| Rule | Why |
|---|---|
| Frame times are integer nanoseconds: `Duration(i) * Second / Duration(fps)` | no float accumulation across frames |
| `u` is `float64(t − t0) / float64(t1 − t0)` from the two keyframes' integer nanoseconds | one rounding, the same for every evaluation |
| Easings use multiplication and subtraction only, no `math.Pow` | identical results everywhere `float64` is IEEE |
| Interpolation is `units.Value` arithmetic | `units` rounds once and canonicalises zero |
| Transforms are `r3` constructors and `Then`; vertices are `Apply` | `r3` sums in a fixed order |
| Each part `AddPart` attached is tessellated once, in `render.New`; each rebuilt body once per render call; decad promises equal vertex and index order for equal payload, tolerance and level (`docs/tessellation-design.md` §1, Determinism row) | a part's base mesh changes only when its parameter tuple does |
| A parametric part's cache key is each parameter's `Value.MarshalText()` in name order; the names are sorted once with `slices.Sorted` in `AddParametric` | which body a frame gets never depends on map order |
| `Build` runs once per distinct tuple per `BuildCache`, and a `Builder` returns equal bodies for equal `Params` (§5.7) | which worker built a body cannot reach the bytes |
| Models are emitted in `AddPart` order; lights in `Style` order | solidlens draws models and edge lines in the order given, and its edge records are kept in first-encounter order |
| No map is iterated into any output; `Style.Parts` is only looked up by name | Go randomises map order |
| No `time.Now`, no randomness, no environment reads | nothing outside the inputs reaches the pixels |
| Workers render disjoint frames to separate files; the result struct is the same for every worker count | scheduling cannot reach the bytes |
| `png.Encode` with the default encoder | deterministic for one toolchain |

The limit: `r3.Rotation` calls `math.Sincos`, and Go may fuse `x*y + z` into an FMA on arm64 but not on amd64, so the
last bit of a rotated vertex can differ between architectures and a pixel on a silhouette can flip. decad records
the same limit for its bounds. Tests therefore never commit a PNG golden; §10 says what they compare instead.

## 8. Package layout

| Path | Owns |
|---|---|
| `doc.go` | Package doc: scope, layering (`kinetograph -> decad, r3, units`; `render -> solidlens`), D1–D9 by name, and reshape (§5.7). Pass 3: the root package names a light only as a pose. |
| `errors.go` | The sentinel vocabulary of §6. Pass 3 adds `ErrInvalidLight`. |
| `channel.go` | `Easing` and the five provided easings, `Keyframe`, `Channel`, `NewChannel`, `Constant`. Pass 3 adds `Keyframes`. |
| `rig.go` | `Rig`, `Node`, the three joint constructors, `Local`, `World`. |
| `camera.go` | `Camera`, `CameraPose`, and the node-local to world evaluation. |
| `light.go` (pass 3) | `LightKind`, `Light`, `LightPose`, and the node-local to world evaluation. |
| `scene.go` | `Scene`, `Pose`, `Frame`, `PartInfo`, `AddPart`, `SetCamera`, `Parts`, `At`, `AtCached`. Pass 3 adds `AddLight` and `Frame.Lights`. |
| `reshape.go` | `Params`, `Builder`, `AddParametric`, `BuildCache`, `NewBuildCache`, and the cache key encoding. |
| `clip.go` | `Clip`, `NewClip`, `FrameCount`, `FrameTime`, `Frame`, `FrameCached`. |
| `internal/memo/memo.go` | `memo.Map`: calls a function once per key, hands concurrent callers for that key the same result, and drops a result whose caller's ctx was done. Backs `BuildCache` and `render`'s mesh cache. |
| `render/style.go` | `Appearance`, `Style`, `ErrStyle`, style validation. Pass 3 adds `Appearance.Fade`, `LightAppearance`, `Style.Lights` and their validation. |
| `render/renderer.go` | `Renderer`, `New`, `Frame`; the posed `TriangleSource`; the per-call build and mesh caches; the solidlens scene assembly. Pass 3 adds the node lights, the fade and intensity evaluation and the hidden/opaque/fading grouping. |
| `render/fade.go` (pass 3) | The layer scenes `B` and `A_k` and the byte mix of §9 pass 3. |
| `render/sequence.go` | `Sequence`, `Renderer.Sequence`, `SequenceOption`, `FrameError`, atomic frame file writes. Pass 3 encodes the image `Frame` returns with `png.Encode`. |
| `examples/` | `Example_kinetograph_*` with verified `// Output:` blocks. Never `package main`. |
| `_clips/demo/` | The demo clip program, its own module (D11): `parts.go` builds the bodies, `scene.go` the rig, channels and style, `main.go` the flags and the `Sequence` call. |
| `docs/design.md` | This document. |
| `.github/workflows/ci.yml` | golangci-lint v2.12.2 and `go vet`; `go test -race ./...` on ubuntu, `go test ./...` on macOS and Windows; `go mod tidy` diff; govulncheck. Copied from decad's with the shard matrix removed. |
| `.golangci.yml` | decad's house config, copied. |

The landing-page clip program itself is not in the initial pass; it is pass 4's `_clips/decad-landing/` (D11).

## 9. Passes

### Pass 1, the initial pass

Everything in §5. The acceptance instance is §5.6's example: a real decad body, moved by a real revolute joint,
seen by a camera on a real revolute joint, rendered by solidlens to PNG files on disk, with frame count and file
names printed and verified by `go test`.

Why each piece is in:

- `Channel` with easing: the clip is nothing without motion over time.
- `Rig` with `Fixed`, `Revolute`, `Prismatic`: "turning" is a revolute joint and "sliding apart" is a prismatic
  one. Under D1 the rig is the only way a body moves at all, so it is the core and not an extra. The three joints
  together are one `switch` over three `r3` calls.
- The camera on the rig: the moving camera the clip wants comes free once the rig exists (D5).
- `Clip`: frame timing is where determinism starts (§7).
- `render` with `Sequence`: the PNG sequence is the deliverable; without it nothing is end to end.
- `Appearance.Back`: a decad sheet body renders two-sided through one optional field. Leaving it out would make
  sheets look wrong rather than save work.

Why each piece is out:

- Reshape: the landing-page motions are rigid. Reshape is pass 2.
- Animated lights: lights are static in `Style`; the clip does not need them to move.
- Vector channels: a moving camera target is a camera on a moving node (D5), so no `r3.Vec` is ever interpolated
  in pass 1.
- The clip program: its content is not decided, and it is its own module (D11).

### Pass 2, reshape

Everything in §5.7. The acceptance instance is §5.8's example: a real `Builder` extruding a real decad body whose
width follows a `Length` channel, rendered by solidlens to PNG files, with the builds and file names printed and
verified by `go test`.

Why each piece is in:

- `Builder` and `Params`: a body that changes shape is a Go function of parameters, and the parameters are scalar
  channels like every other animated quantity (D1).
- `BuildCache` with its text key: a held or repeated tuple must not rebuild, and several workers must not build
  one tuple twice. The key is exact (`MarshalText` round-trips), so caching never changes a pixel.
- `AtCached`, `FrameCached` and `Parts`: `render` needs to evaluate frames through a cache it owns per call, and to
  learn the parts without building any. The initial-pass `At` and `Frame` keep their signatures and build afresh on
  each call.
- `Pose.Params`: a caller reading a `Frame` sees the values a body was built from, not only the body.

### Pass 3, animated appearance

Pass 3 adds two things: lights that ride on rig nodes, and a fade per part. Both are scalar channels (D1), and
neither changes a pass-1 or pass-2 signature; every addition is a new function, a new type or a new struct field.

The split between the packages follows D6. The root package learns where a light is and which way it shines,
because that is a pose on the rig. Everything about how a light or a part looks stays in `render.Style`: a light's
color and its intensity over time, and a part's fade over time. `render` evaluates those channels at the frame's
`Time`, so the root package still names no color and no pixel.

solidlens cannot draw a translucent surface (§11), so a fade is not a material alpha. `render` draws a fading
part by rendering the frame with and without it and mixing the two images.

#### Root package: lights on nodes

```go
// LightKind says how a Light's node-local vectors are read.
type LightKind int

const (
    // PointLight shines outward in every direction from Light.Position.
    PointLight LightKind = iota + 1
    // DirectionalLight shines with parallel rays along Light.Direction, as from infinitely far away.
    DirectionalLight
)

// Light is a light source in its node's frame. It says where the light is and which way it shines. Its color
// and its intensity over time are render.Style's, bound by the name the light is added under.
type Light struct {
    Kind      LightKind
    Position  r3.Vec // PointLight: the light's position. The zero Vec for a DirectionalLight.
    Direction r3.Vec // DirectionalLight: the way the light travels, toward the scene. The zero Vec for a PointLight.
}

// LightPose is a light at one time, in world coordinates. A PointLight's Direction and a DirectionalLight's
// Position are the zero Vec; a DirectionalLight's Direction has unit length.
type LightPose struct {
    Name      string
    Kind      LightKind
    Position  r3.Vec
    Direction r3.Vec
}

// AddLight attaches light to node under name. Light names are their own namespace: a light may share a part's
// name. It returns ErrForeignNode for a nil node or a node of another rig, ErrDuplicateName for a name another
// light already uses, ErrInvalidLight for a Kind that is neither PointLight nor DirectionalLight, for a non-zero
// vector the Kind does not read, or for a non-finite PointLight Position, and ErrDegenerateDirection for a zero
// or non-finite DirectionalLight Direction. AddLight is not safe beside At.
func (s *Scene) AddLight(name string, node *Node, light Light) error

// Frame gains one field. Lights are in AddLight order; a scene with no light has an empty Lights.
type Frame struct {
    Index  int
    Time   time.Duration
    Poses  []Pose
    Camera CameraPose
    Lights []LightPose
}

// Keyframes returns a copy of the channel's keyframes in time order. A keyframe given a nil Ease carries Linear.
// Constant(v) has one keyframe, at time 0.
func (c *Channel) Keyframes() []Keyframe
```

`AddLight` normalizes a `DirectionalLight` direction once with `Vec.Normalize`, as `Prismatic` does. `Scene.At`
poses each light with its node's `World(t)`: `Apply` for a position, `ApplyDir` for the stored unit direction, the
same two calls the camera uses (D5). A light that circles a part is a light on a revolute node; a light fixed in
the world is a light on `rig.Root()`. There is no light-specific motion type.

`ErrInvalidLight` is a new sentinel in `errors.go`. `Keyframes` exists so that `render.New` can check a fade's and
an intensity's keyframe values (below) without the root package knowing what a fade is.

#### `render`: light appearance and fade

```go
// Appearance gains Fade. Material, Back and Edges keep their pass-1 meaning.
type Appearance struct {
    Material solidlens.Material
    Back     *solidlens.Material
    Edges    solidlens.Edges
    // Fade is the part's opacity over time, a Dimensionless channel whose keyframes lie in [0, 1]. At 0 the
    // part, its back side and its edge lines are left out of the frame. At 1 the part is drawn exactly as when
    // Fade is nil. Between them the part is mixed over whatever is behind it (see below). nil is 1 at every time.
    Fade *kinetograph.Channel
}

// LightAppearance is how one node light shines.
type LightAppearance struct {
    // Color goes to solidlens unchanged. solidlens scales the light by the luminance of Color,
    // 0.2126·R + 0.7152·G + 0.0722·B, and does not tint the surface (§11). The zero Color has luminance 0, so
    // a light with it adds nothing.
    Color solidlens.Color
    // Intensity is solidlens's light Intensity over time, a Dimensionless channel whose keyframes are >= 0.
    // A point light's contribution falls off as 1 / max(1, d²) with d in millimetres, so a point light 100 mm
    // from a face needs an intensity near 10⁴ to light it as strongly as a directional light of intensity 1.
    Intensity *kinetograph.Channel
}

// Style gains Lights. The pass-1 fields keep their meaning: DirectionalLights and PointLights are fixed in the
// world at a constant intensity.
type Style struct {
    Width, Height     int
    Chord             units.Value
    Background        solidlens.Color
    Default           Appearance
    Parts             map[string]Appearance
    DirectionalLights []solidlens.DirectionalLight
    PointLights       []solidlens.PointLight
    Lights            map[string]LightAppearance // by light name; every light of the scene needs an entry
}
```

`render.New` validates the new fields after the pass-1 checks, in this order, and reports the first failure:

1. Every light in frame 0's `Lights` must have a `Style.Lights` entry, and every `Style.Lights` name must belong to
   a light; either failure is `ErrStyle` naming the light. Names are checked in sorted order, so the reported name
   does not depend on map order.
2. Each `LightAppearance.Intensity`, in sorted light-name order: nil is `kinetograph.ErrNilChannel`, a kind other
   than `units.Dimensionless` is `kinetograph.ErrKind`, and a keyframe below 0 is `ErrStyle`.
3. `Default.Fade`, then each `Parts[name].Fade` in sorted name order: a kind other than `units.Dimensionless` is
   `kinetograph.ErrKind`, and a keyframe outside [0, 1] is `ErrStyle`. `Default.Fade` is checked even when every
   part has a `Parts` entry.

The keyframe checks catch a value that is out of range on purpose. Between keyframes an easing may still
overshoot (an `Easing` may return a value outside [0, 1]) and the arithmetic may round one ulp past a keyframe, so
at each frame `render` clamps the evaluated fade to [0, 1] and the evaluated intensity to at least 0. Both are read
as `float64` with `Value.In(units.One)`.

Per frame, the solidlens scene's `DirectionalLights` are `Style.DirectionalLights` followed by every
`DirectionalLight` pose in `Frame.Lights` order, each as `solidlens.DirectionalLight{Direction, Color, Intensity}`;
`PointLights` are built the same way from `Style.PointLights` and the `PointLight` poses. Each frame builds new
slices and never appends to the `Style`'s, because workers share the `Renderer`.

#### How `render` draws a fade

`render` evaluates every part's fade at the frame's `Time` and sorts the parts into three groups: hidden (fade 0),
opaque (fade 1, or no `Fade`) and fading (strictly between). It poses every non-hidden part's mesh once, as in pass
1, and builds its solidlens scenes from those models:

- `B`: the opaque and fading parts.
- `A_k`, one per fading part `k`: `B` without part `k`.

With no fading part, the frame is `B` alone: one `solidlens.Render` call, and the same bytes as pass 1 when no
`Fade` is set. With fading parts `k = 1 … n` in `AddPart` order and fades `f_k`, each byte of the result's `Pix`
is

```
out = clamp(round(B + Σ_k (1 − f_k) · (A_k − B)), 0, 255)
```

summed in `AddPart` order, with round-half-up as solidlens's own edge blending does. A frame with `n` fading parts
costs `n + 1` renders.

For one fading part this is `f · B + (1 − f) · A`: where the part is the nearest surface, `B` shows the part and
`A` shows what is behind it; everywhere else `A` and `B` agree and the pixel is unchanged. The part's edge lines
fade with it, and the edge lines of a part behind it show through, because both images carry their own edges. The
mix is of the 8-bit sRGB values solidlens writes, not of linear light: a red part at fade 0.5 over a white
background gives the bytes (255, 128, 128).

Where two fading parts overlap on screen, the result is exact for the nearer one and draws the farther one opaque
behind it: the pixel is `f_near · near + (1 − f_near) · far`, without the scene behind the farther part. Two fading
parts that do not overlap are each exact. An exact result needs depth-sorted blending inside solidlens (§11).

`Renderer.Frame` returns the composite. `Sequence` encodes the image `Frame` returns with `png.Encode`;
`solidlens.RenderPNG` is `Render` followed by the same `png.Encode`, so a frame without a fading part writes the
same bytes as pass 1.

#### Pass 2 parts

A fade is looked up by `Pose.Name` through `Style.Parts` and `Default`, so a part added with `AddParametric` fades
exactly as one added with `AddPart`. Every layer of a frame reuses that frame's posed meshes; for a parametric part
that is the mesh of the body built for the frame's parameter key (pass 2), and no layer calls `Build` again.

A fade does not change which bodies are built. A parametric part at fade 0 is still built and tessellated at that
frame, so a `Build` error fails the frame whatever the `Style` says, and the set of failing frames depends on the
`Scene` alone.

#### Errors

| Condition | Error | Where |
|---|---|---|
| light kind is neither `PointLight` nor `DirectionalLight`; the vector its kind does not read is non-zero; a non-finite point position | `ErrInvalidLight` | `AddLight` |
| directional light direction is zero or non-finite | `ErrDegenerateDirection` | `AddLight` |
| light name already used by another light | `ErrDuplicateName` | `AddLight` |
| nil node, node of another rig | `ErrForeignNode` | `AddLight` |
| the light's node cannot be posed at `t` | `r3` or `units` error, wrapped with the light name | `Scene.At`, surfaced as `*render.FrameError` |
| a scene light with no `Style.Lights` entry; a `Style.Lights` name no light carries | `render.ErrStyle` | `render.New` |
| nil `LightAppearance.Intensity` | `ErrNilChannel` | `render.New` |
| `Intensity` or `Fade` channel not `Dimensionless` | `ErrKind` | `render.New` |
| an `Intensity` keyframe below 0; a `Fade` keyframe outside [0, 1] | `render.ErrStyle`, naming the light or part (`Default` for the default appearance) | `render.New` |
| `Intensity` or `Fade` interpolation overflows | `units.ErrNotFinite` (wrapped) | `*render.FrameError` at that frame |

#### Determinism

§7's rules hold, with these additions:

| Rule | Why |
|---|---|
| Light poses are in `AddLight` order; solidlens lights are the `Style` lights, then the node lights in that order | solidlens sums light contributions in the order given |
| `Style.Lights` and `Style.Parts` are looked up by name; validation walks their names in sorted order | the reported error does not depend on map order |
| A fade's layers are mixed in `AddPart` order, and every product in the mix is wrapped in an explicit `float64(…)` conversion | the Go spec forbids fusing a product into an FMA across an explicit conversion, so the mix is the same on amd64 and arm64 for the same layer bytes |
| The fade and intensity are clamped after evaluation, never before | the clamp sees the one value `Channel.At` computes |

The layer images themselves keep §7's limit: a vertex can differ in its last bit between architectures.

#### Package layout

§8 marks the pass-3 rows and additions.

#### Tests

| Area | Test | Asserts |
|---|---|---|
| light | point light at node-local (10, 0, 0) on a revolute node about Z through the origin, at 90° | `Frame.Lights[0].Position` is (0, 10, 0) within 1e-12 |
| light | directional light with node-local direction (0, 0, 3) on a prismatic node | `Direction` is (0, 0, 1) exactly at every frame; a slide does not move a direction |
| light | directional light along +X on a revolute node about Z, at 90° | `Direction` is (0, 1, 0) within 1e-12 |
| light | two lights, one part | `Frame.Lights` names in `AddLight` order; `Frame.Poses` unchanged from a scene without lights |
| light | kind 0; a point light with a non-zero `Direction`; a zero directional `Direction`; a repeated light name; a foreign node | `ErrInvalidLight`, `ErrInvalidLight`, `ErrDegenerateDirection`, `ErrDuplicateName`, `ErrForeignNode` |
| channel | `Keyframes` on a three-key channel with one nil `Ease`; on `Constant` | the keys in order with `Linear` in place of nil; one key at time 0 |
| render | the block's camera-facing face (`Ambient` 0, red), a white directional light travelling +Y with `Intensity` `Constant(0.5)` on a revolute node about Z, frames at 0° and 180° | the image-centre pixel equals `solidlens.RGB(0.5, 0, 0).NRGBA()` at 0° and is black at 180° |
| render | the same light with `Intensity` ramping 0 → 1 over 1 s, frame 12 at 24 fps | the centre pixel equals `solidlens.RGB(0.5, 0, 0).NRGBA()` |
| render | a node point light against a `Style.PointLights` entry at the node light's world `Position` with the same color and intensity | the two frames are byte-identical PNG encodings; the same for a directional light |
| render | a `Style` directional light of intensity 0.25 and a node light of 0.25 along the same direction | the centre pixel equals `solidlens.RGB(0.5, 0, 0).NRGBA()` |
| render | a scene light missing from `Style.Lights`; an unknown `Style.Lights` name; nil `Intensity`; a Length `Intensity`; an `Intensity` keyframe of −1 | `ErrStyle`, `ErrStyle`, `ErrNilChannel`, `ErrKind`, `ErrStyle` |
| fade | `Fade` `Constant(0)` on the only part | no pixel differs from the background |
| fade | `Fade` `Constant(1)` against no `Fade` | byte-identical PNG encodings |
| fade | `Fade` `Constant(0.5)` on an edged block over the background | every byte of `Pix` equals `round(b + 0.5 · (a − b))` of the fade-1 image `a` and fade-0 image `b`, computed by the test from two other renderers |
| fade | a red block at fade 0.5 in front of a larger opaque blue block | where they overlap the pixel is (128, 0, 128, 255): the blue part shows through |
| fade | two fading blocks side by side, not overlapping, at 0.25 and 0.75 | each block's pixels equal its single-part fade formula |
| fade | two fading blocks, one in front of the other | every byte equals `B + Σ (1 − f_k)(A_k − B)` from layer images the test renders itself; this pins the documented overlap result |
| fade | a custom easing that returns 1.5 at u = 0.5 on a 0 → 1 fade, at that frame | byte-identical to the frame with no `Fade` (clamped to 1) |
| fade | an Angle `Fade`; a keyframe of 1.5 on `Default.Fade`; −0.1 on a `Parts` fade | `ErrKind`, `ErrStyle` naming `Default`, `ErrStyle` naming the part |
| fade | render a frame with a part at fade 0.5 twice; `Sequence` of 6 frames with 1 and 3 workers | byte-identical encodings; the files of both runs are equal |
| fade | a parametric part (pass 2) at fade 0.5 | the formula row above holds for it |
| fade | a parametric part whose `Build` fails at frame 3, with fade 0 at frame 3 | `*FrameError` with `Index == 3` |
| examples | `Example_kinetograph_appearance` | the `// Output:` block, verified by `go test ./examples/` |

`examples/kinetograph_appearance_example_test.go` is the end-to-end instance pass 3 is accepted on: a block fading
in from 0 to 1 over one second and a point light on a revolute node circling it, rendered as a 4-frame clip at
4 fps into a temporary directory. It prints each frame's light position to one decimal, the file names and the
sequence pattern.

#### Why each piece is in, and what is out

- A light is a node attachment, as the camera is (D5). A moving light is a light on a moving node, so no position
  or direction is interpolated (D1).
- A light's intensity is a `Dimensionless` channel, so a light that brightens or dims needs no other type.
- A fade mixes layer renders. It needs no solidlens change, a test can recompute its result from images it
  renders itself, and it is exact for one fading part and for several that do not overlap on screen.
- An animated light color is out. solidlens reads only a light color's luminance (§11), so an animated color
  would draw the same pixels as an animated intensity.
- Animated material colors and an animated background are out. Pass 3 does not specify them, and adding one
  later is a new `render.Style` channel field, which changes no signature.

### Pass 4, the landing-page clip

A nested module `_clips/decad-landing/` with its own `go.mod`, modeled on decad's `_gallery`: the scene content,
the style, and the ffmpeg invocation in its `main.go` doc comment. It joins neither the library's module nor its
tests (D11).

## 10. Test plan

Every test asserts a computed result: a transform component, a `units.Value`, a pixel coordinate, a file count.
Tests use `testify/require` in an external `_test` package with `t.Context()`, and build decad bodies with
`decadtest.NewBlock` (decad's own fixture kit, standard `testing` only). A test `Builder` builds its block with
`sketch` and `decad` directly and returns errors, because `render` calls it on worker goroutines, where a
`testing.TB` must not fail the test. No test commits a PNG golden (§7).

| Area | Test | Asserts |
|---|---|---|
| channel | `At` at each keyframe time | the keyframe value exactly (`Value.Equal(..., 0)`) |
| channel | `At` at segment midpoints with `Linear`, `SmoothStep`, `EaseInOut` | the eased value to 1e-12 of the base unit, from the formula in §5.1 |
| channel | before the first and after the last keyframe | the end value, held |
| channel | unordered, empty, mixed-kind, non-finite keyframes | the §6 sentinel for each |
| channel | every provided `Easing` | `Ease(0) == 0`, `Ease(1) == 1` |
| rig | revolute 90° about Z through the origin, point (1, 0, 0) | world point (0, 1, 0) within 1e-12 |
| rig | prismatic along (0, 0, 2) by 5 mm | translation (0, 0, 5) exactly |
| rig | grandchild of a revolute under a prismatic | `World(t)` equals `child.Local(t).Then(parent.World(t))` computed independently with `r3`, within 1e-12 |
| rig | `Fixed` with a reflection, with `Transform{}` | `ErrReflection`, `ErrInvalidTransform` |
| rig | revolute with a Length channel, prismatic with an Angle channel | `ErrKind` |
| camera | camera on a revolute node about Z through its target, at 180° | world position mirrored through the target within 1e-12; target unchanged |
| scene | two parts, one camera | `Frame.Poses` in `AddPart` order, each `Transform` equal to its node's `World(t)` |
| scene | duplicate name, foreign node, no camera, no parts | the §6 sentinel for each |
| clip | 1 s at 24 fps; 1 s at 30 fps; 100 ms at 24 fps | `FrameCount` 24, 30, 3; `FrameTime(23)` is exactly 958333333 ns |
| render | a 10 mm block at the origin, camera on −Y looking at the origin, 64×48 | the centroid of non-background pixels is within 1 px of the image centre |
| render | the same block on a prismatic joint sliding +X between frames 0 and 12 | the centroid's x increases by at least 2 px, y unchanged within 1 px |
| render | the block offset from a revolute Z axis, frames at 0° and 180° | the two centroids are mirrored about the image centre within 1 px |
| render | a sheet body with `Appearance.Back` set | at least one pixel carries the back colour and one the front colour |
| render | decad-posed reference (below) against kinetograph's vertex-transform path | centroids agree within 0.5 px; at least 99.5 % of pixels are identical |
| render | render frame 7 twice in one process | byte-identical PNG encodings |
| render | `Sequence` of a 6-frame clip with 1 and with 3 workers | 6 files named by `Pattern`, each file's bytes equal to `Frame(i)` encoded, `Sequence.Frames == 6` |
| render | a camera whose FOV channel is 179.5° at frame 4 only and 40° at every other frame | `Sequence` returns a `*FrameError` with `Index == 4`, `Time == FrameTime(4)`, files 0–3 present, no file 4 |
| render | a context that reports cancelled once frame 2's file exists, one worker | `ctx.Err()` returned unwrapped; files 0–2 present, no other file, no `.tmp` file left in `dir` |
| render | `Style` with an unknown part name, zero width, an Angle chord | `ErrStyle`, `ErrStyle`, `ErrKind` |
| reshape | `AddParametric` with a nil builder, a foreign node, a nil channel, a length-times-angle keyframe value, a duplicate name | `ErrNilBuilder`, `ErrForeignNode`, `ErrNilChannel`, `units.ErrUnnamedKind`, `ErrDuplicateName`; no `Build` call |
| reshape | a parametric block on a prismatic node, width 10 to 30 mm over 1 s, `Scene.At` at 500 ms twice | `Pose.Params` holds 20 mm and 10 mm exactly; the body's measured bounds are ±10 × ±5 × 0–10 mm; `Pose.Transform` equals the node's `World(t)`; each `At` call builds once |
| reshape | `AtCached` through one cache from 16 goroutines at four times sharing one tuple, then at two times with a new width and a new depth | `Build` called once and every `Pose.Body` the same pointer; each new tuple adds exactly one call; `FrameCached` reuses the cached body |
| reshape | width and depth swap values between two times (10 × 20 mm, then 20 × 10 mm), one cache | `Build` called twice; the two bodies' measured bounds are ±5 × ±10 and ±10 × ±5 mm |
| reshape | `Build` returns an error; `Build` returns a nil body | `errors.Is` reaches the builder's error; `ErrNilBody`; the message names the part; a later time with the same tuple fails without another `Build` |
| reshape | a `Builder` that cancels ctx and fails | `ctx.Err()` returned unwrapped; the next `AtCached` with a live ctx builds again |
| reshape | `Scene.Parts` on a scene with both kinds of part | names and order as added; `Body` set for the `AddPart` part, nil and `Parametric` for the other; no `Build` call |
| memo | `Map.Get` from 8 goroutines on one key, released only once 7 are parked on the entry | one call; every caller gets the same result |
| memo | a caller parked on a call that then fails under its own cancelled ctx | the parked caller calls its own function once and gets its result |
| memo | a failing call; a call failing under a cancelled ctx; a waiter with a cancelled ctx; a panicking call | the failure kept; the cancelled failure dropped and rebuilt; `ctx.Err()`; the entry dropped |
| render | a parametric block whose width steps from 10 to 20 mm at frame 3 of 6 | from frame 0 to frame 5 the blob's pixel width grows by at least 4 px and stays centred within 1 px; frame 5's PNG bytes equal the same 20 mm block attached with `AddPart` |
| render | `Sequence` of a parametric clip with 2 distinct tuples over 6 frames, with 1 and with 3 workers | `Build` called exactly 2 times in each run; `render.New` called it 0 times; the 6 files are byte-identical between the two runs |
| render | a `Builder` that fails for the tuple first reached at frame 3, with 1 and with 3 workers | `*FrameError` with `Index == 3`, `Time == FrameTime(3)`, `errors.Is` reaches the builder's error; exactly files 0–2 present, no `.tmp` file |
| render | 3 workers; a `Builder` refusing frame 4's tuple at once and frame 3's only after that refusal | refusals in that order; `*FrameError` with `Index == 3`; exactly files 0–2 present |
| render | a parametric part with a zero chord | `render.New` succeeds; `Frame(2)` returns a `*FrameError` with `Index == 2` naming the part's tessellation; one `Build` call |
| render | a `Builder` that fails at every frame | `render.New` succeeds; `Frame(0)` returns a `*FrameError` with `Index == 0` |
| render | `Style.Parts` naming a parametric part | accepted; that part draws in its own colour |
| examples | `Example_kinetograph_sequence` | the `// Output:` block, verified by `go test ./examples/` |
| examples | `Example_kinetograph_reshape` | the `// Output:` block, verified by `go test ./examples/` |

The blob-centroid assertions read what the renderer drew rather than re-deriving solidlens's projection, so a
change to solidlens's camera model fails them without kinetograph having copied that model.

The decad-posed reference is the real producer through the real consumer: the test poses the block with
`Body.PlacedCopy(t)` in decad, tessellates that body and renders it with solidlens directly, then renders the
same block through kinetograph at the same frame time. decad re-evaluates the payload under the motion, so the
two vertex sets agree only to rounding, and the comparison is pixel-wise with the slack above rather than
byte-wise.

## 11. Dependency gaps

No capability the initial pass needs is missing from decad, solidlens, `r3` or `units`. Three additions would
simplify or speed up kinetograph without changing its behaviour; none is a blocker and none is planned for.

| Module | Addition | What it would replace |
|---|---|---|
| `r3` | `func (v Vec) Lerp(o Vec, t float64) Vec` | nothing; no pass interpolates a vector |
| solidlens | a per-`Model` `r3.Transform` applied at render time | kinetograph's per-frame vertex copy under `Transform.Apply` |
| solidlens | `Camera.FOV` as a `units.Value` | the `In(units.Degree)` conversion at the camera seam |

Pass 3 meets three limits in solidlens. They were read from the pinned solidlens source (`render.go`, `scene.go`,
`edges.go`) and confirmed by rendering a probe scene; none blocks pass 3, which works around each one.

- **No translucent surface.** `Material.Color.A` reaches the pixel unblended. The rasterizer writes the shaded color,
  alpha included, over the pixel and writes the depth buffer. It does not sort triangles or blend with what is
  already drawn. A red square at alpha 0.5 in front of a blue one writes the pixel (128, 0, 0, 128): the blue
  is gone and the PNG pixel itself is half transparent. A fade by material alpha would therefore punch a hole in
  the image rather than show the scene behind the part, so pass 3 mixes two renders instead.
- **Light color does not tint.** solidlens multiplies a light's intensity by the luminance of its `Color`
  (0.2126·R + 0.7152·G + 0.0722·B) and shades the material color with that one number. A red light at intensity
  1 on a white surface draws grey (127, 127, 127). Pass 3 therefore does not animate light color.
- **Zero edge color is black.** `Edges.Color` equal to `Color{}` draws opaque black lines. Fading an edge color
  to zero alpha would turn the lines black at the end of the fade, and edge lines are drawn only after every
  surface, so pass 3 fades a part's edges by leaving the part out of a layer instead.

Two solidlens additions would let pass 3 do less work or be exact:

| Module | Addition | What it would replace |
|---|---|---|
| solidlens | a per-`Model` opacity: opaque models drawn first, then translucent triangles sorted far to near, blended without writing depth, with their edge lines blended at the same opacity | the `n + 1` renders and the byte mix per fading frame; the result would also be exact where two fading parts overlap |
| solidlens | shading each color channel by the light's matching channel | nothing in pass 3; an animated light color would become meaningful |

`units` has no time dimension. D3 chooses `time.Duration` so that no `units` change is needed.

decad cannot tessellate a `Document.Patch` sheet today (`unsupported payload patchPayload`), so a `Patch` body
cannot be rendered. The sheet render test uses an open tube from `Extrude` with `decad.WithSurfaceResult()` instead.

## 12. Where each settled choice lives

Every design choice is stated once, in the section that owns it. This section only points.

| Choice | Owner |
|---|---|
| the landing-page clip is a `_clips/decad-landing/` nested module | §4 D11, §9 pass 4 |
| the camera is a rig attachment and there is no orbit-camera type | §4 D5 |
| `sketch` is a test-and-example-only dependency | §4 D10 |
| lights are static in pass 1 and move in pass 3 | §4 D6, §9 |
| a light's color and intensity, and a part's fade, are `render.Style` channels bound by name | §4 D6, §9 pass 3 |
| a fade mixes renders with and without the part, not material alpha | §9 pass 3, §11 |
| time is `time.Duration`, a frame rate an `int` | §4 D3, §7 |
| reshape is pass 2 | §3, §9 |
| a reshape cache lives in one render call, never in the `Renderer` | §5.7 |
| the reshape cache key is each value's `MarshalText()` in sorted-name order | §5.7, §7 |
