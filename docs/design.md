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
testable without a renderer, and nothing in it names a color, a light or a pixel. `render` turns a `Frame` into a
solidlens scene. Appearance (materials, back materials, edges, lights, background) is `render.Style`'s. Lights are
static: they are listed in `Style` and do not move with the clip. Pass 3 (§9) adds lights attached to nodes.

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
| `doc.go` | Package doc: scope, layering (`kinetograph -> decad, r3, units`; `render -> solidlens`), D1–D9 by name, and reshape (§5.7). |
| `errors.go` | The sentinel vocabulary of §6. |
| `channel.go` | `Easing` and the five provided easings, `Keyframe`, `Channel`, `NewChannel`, `Constant`. |
| `rig.go` | `Rig`, `Node`, the three joint constructors, `Local`, `World`. |
| `camera.go` | `Camera`, `CameraPose`, and the node-local to world evaluation. |
| `scene.go` | `Scene`, `Pose`, `Frame`, `PartInfo`, `AddPart`, `SetCamera`, `Parts`, `At`, `AtCached`. |
| `reshape.go` | `Params`, `Builder`, `AddParametric`, `BuildCache`, `NewBuildCache`, and the cache key encoding. |
| `clip.go` | `Clip`, `NewClip`, `FrameCount`, `FrameTime`, `Frame`, `FrameCached`. |
| `internal/memo/memo.go` | `memo.Map`: calls a function once per key, hands concurrent callers for that key the same result, and drops a result whose caller's ctx was done. Backs `BuildCache` and `render`'s mesh cache. |
| `render/style.go` | `Appearance`, `Style`, `ErrStyle`, style validation. |
| `render/renderer.go` | `Renderer`, `New`, `Frame`; the posed `TriangleSource`; the per-call build and mesh caches; the solidlens scene assembly. |
| `render/sequence.go` | `Sequence`, `Renderer.Sequence`, `SequenceOption`, `FrameError`, atomic frame file writes. |
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

Lights attached to nodes (a `Light` struct beside `Camera` in the root package, with its color and intensity still
solidlens's inside `render`); a `Dimensionless` channel per part for a fade, applied as material alpha in `render`.
Decided then, documented here first.

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
| reshape | `Build` returns an error; `Build` returns a nil body | `errors.Is` reaches the builder's error; `ErrNilBody`; the message names the part; a later time with the same tuple fails without another `Build` |
| reshape | a `Builder` that cancels ctx and fails | `ctx.Err()` returned unwrapped; the next `AtCached` with a live ctx builds again |
| reshape | `Scene.Parts` on a scene with both kinds of part | names and order as added; `Body` set for the `AddPart` part, nil and `Parametric` for the other; no `Build` call |
| memo | `Map.Get` from 8 goroutines on one key; a failing call; a call failing under a cancelled ctx; a waiter with a cancelled ctx; a panicking call | one call and one shared result; the failure kept; the cancelled failure dropped and rebuilt; `ctx.Err()`; the entry dropped |
| render | a parametric block whose width steps from 10 to 20 mm at frame 3 of 6 | from frame 0 to frame 5 the blob's pixel width grows by at least 4 px and stays centred within 1 px; frame 5's PNG bytes equal the same 20 mm block attached with `AddPart` |
| render | `Sequence` of a parametric clip with 2 distinct tuples over 6 frames, with 1 and with 3 workers | `Build` called exactly 2 times in each run; `render.New` called it 0 times; the 6 files are byte-identical between the two runs |
| render | a `Builder` that fails for the tuple first reached at frame 3, with 1 and with 3 workers | `*FrameError` with `Index == 3`, `Time == FrameTime(3)`, `errors.Is` reaches the builder's error; exactly files 0–2 present, no `.tmp` file |
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
| `r3` | `func (v Vec) Lerp(o Vec, t float64) Vec` | nothing in pass 1; pass 3 would compose it as `a.Add(o.Sub(a).Scale(t))` |
| solidlens | a per-`Model` `r3.Transform` applied at render time | kinetograph's per-frame vertex copy under `Transform.Apply` |
| solidlens | `Camera.FOV` as a `units.Value` | the `In(units.Degree)` conversion at the camera seam |

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
| time is `time.Duration`, a frame rate an `int` | §4 D3, §7 |
| reshape is pass 2 | §3, §9 |
| a reshape cache lives in one render call, never in the `Renderer` | §5.7 |
| the reshape cache key is each value's `MarshalText()` in sorted-name order | §5.7, §7 |
