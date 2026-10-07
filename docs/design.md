# kinetograph design

kinetograph animates solids built with [decad](https://github.com/lestrrat-3d/decad) and renders every frame of
the animation to a numbered PNG with [solidlens](https://github.com/lestrrat-3d/solidlens). A video tool such as
ffmpeg assembles the PNGs into a clip; kinetograph writes no video itself.

This document is the contract the implementation is built from. §5 states the public API as Go signatures: §5.1 to
§5.6 are the initial pass, §5.7 and §5.8 are pass 2 (reshape), §5.2's `TransformTrack` and `Node.Driven` are
pass 5 (driven nodes), §5.9 is pass 6 (decad linkages), and §5.11 is pass 7 (linkage schedules). §9 states which pass adds what. A later pass extends this
document before it extends the code.

## 1. What kinetograph is

A caller describes a scene in Go: decad bodies attached to a tree of joints, one camera attached to the same tree,
and scalar channels (an angle, a slide distance, a field of view) that change with time through keyframes and
easing. A joint can also take its transform at each time from the caller (a driven node, D12). kinetograph evaluates the scene at each frame time, poses every body and the camera with `r3`, and hands
the posed triangles to solidlens.

Its first use is decad's landing-page clip: decad parts turning, sliding apart, assembling and being built feature
by feature, seen from a moving camera. The program that renders it is the `clip` subcommand of decad's `_gallery/`
module, which imports kinetograph (§9, pass 4). A part can also change shape over time: kinetograph rebuilds its decad body from
parameters that change with the frame (§9, pass 2). A decad `Linkage` can be filmed along a `Drive`: each link's
node takes the pose decad's `Linkage.PoseAt` returns, the same pose `Document.VerifyLinkage` checks (§9, pass 6).

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
| Timeline | `Channel`: keyframes of one `units.Kind`, easing between them, `At(t)` | Every quantity kinetograph interpolates is a scalar channel (D1); a driven node's transform comes from the caller (D12); a camera orbit or dolly is a joint (D5) |
| Rig | `Rig`/`Node`: a joint tree; a joint value becomes an `r3.Transform`, composed with the parent's | Bodies AND the camera attach to nodes; the tree is built parent to child, so no cycle can be written |
| Reshape | a body as a Go function of parameters, rebuilt when they change, cached by parameter values | pass 2 (§5.7): a cache lives in one render call, never in the `Renderer`; a failed rebuild stops the sequence (§6) |
| Output | `render`: one solidlens scene per frame, one PNG per frame | a rigid pose is applied to the tessellated vertices, never by re-placing the decad body (D2) |

Evaluation of one frame runs top to bottom:

1. `Clip.FrameTime(i)` gives the frame's time `t` as an exact `time.Duration` (§7).
2. Every `Channel` the rig reads evaluates `At(t)`: find the keyframe segment, map `t` to `u` in `[0, 1]`, ease
   `u`, and interpolate the two keyframe values with `units.Value` arithmetic. Every `TransformTrack` the rig reads
   returns its transform for `t` (D12).
3. Every `Node` builds its local transform from its channel value with `r3.RotationAround` or `r3.Translation`, or
   takes a driven node's from its track, and composes it with its parent's world transform with `Transform.Then`.
4. `Scene.At` collects one `Pose` per part (its body and its node's world transform) and the camera's world
   position, target, up and field of view into a `Frame`. For a parametric part (§5.7) it first evaluates the
   part's parameter channels at `t` and takes the body from the part's `Builder`, through a `BuildCache` that
   calls `Build` once per distinct parameter tuple.
5. `render.Renderer` maps each `Pose` to a solidlens `Model` whose vertices are the part's tessellated vertices
   under `Transform.Apply`, builds the solidlens `Camera`, adds the style's lights, the node lights and the
   background, calls `solidlens.Render` (once per layer when a part is fading, §9 pass 3) and encodes the image
   with `png.Encode`. A rebuilt body is tessellated once per distinct body in the call that rendered it.

## 4. Decisions

### D1. Every quantity kinetograph interpolates is a scalar channel; rotations come from an axis and an angle

A `Channel` holds keyframes of one `units.Kind`. A revolute joint reads an `Angle` channel and builds its transform
with `r3.RotationAround(center, axis, angle)`; a prismatic joint reads a `Length` channel and builds
`r3.Translation(dir.Scale(d))`. Interpolation is therefore scalar easing between two `units.Value`s, and no
orientation is ever interpolated: there is no quaternion, no slerp and no rotation matrix in kinetograph.

What this gives up: a body that tumbles about a changing axis needs two or more revolute joints in a chain, one per
axis. That is how a real mechanism moves.

A driven node (D12) is the one animated quantity that is not a channel. kinetograph interpolates nothing for it: the
caller supplies the transform at every time.

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
`ErrReflection`, and `Node.Local` refuses one a driven node's track returns (D12); a revolute or prismatic joint
cannot produce one.

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

### D11. A program that renders clips is a nested module

A program in this repository that renders clips lives in its own `_`-prefixed directory with its own `go.mod`,
modeled on decad's `_gallery/`. The `_` prefix keeps it out of the root module, its tests and its linter, so scene
content never joins the library. There are two:

- `_clips/demo/` renders a 4.5 s clip that uses all three joint kinds and a camera orbit: a drilled plate on
  `Fixed`, two pins that drop into its bolt holes on `Prismatic` joints, and a ring that rises on a `Prismatic`
  joint and turns half a turn on a `Revolute` joint under it. `-smoke` renders the first frame alone at 160×90,
  for CI.
- `_gallery/` renders the PNG frames of every GIF in the README and prints the ffmpeg commands that encode them
  into `docs/images/`, because kinetograph has no video encoder (D7). Each GIF's scene ends in the pose it starts
  in, and its test checks that, so the GIF loops without a jump.

decad's landing-page clip is not in this repository: it is the `clip` subcommand of decad's `_gallery/` module
(§9, pass 4).

#### Nested modules in CI

The root workflow runs `./...`, which skips `_`-prefixed directories; decad's CI does the same for `_gallery`. A
clip module breaks without any change of its own in two ways: a root API change, and a root dependency bump. The
second was checked: with `_clips/demo`'s `go.mod` requiring an older decad than the root does, `go build ./...`
stops with `go: updates to go.mod needed`.

The `clips` job in `.github/workflows/ci.yml` runs on ubuntu with a matrix over `_clips/demo` and `_gallery`. In
the module directory it runs `go mod tidy` with a diff check, `go vet ./...`, `go test ./...` and
`go run . -smoke -out "$RUNNER_TEMP/frames"`. `dependabot.yml` has a `gomod` entry for each of the two directories
beside the root's.

- A pull request that breaks a nested module fails this job. `-smoke` catches decad refusing a body a shot starts
  with; a build alone does not.
- A pull request that changes the root API must also update both nested modules. Each one's `go.mod` and `go.sum`
  take their own dependabot pull requests.
- On a GitHub ubuntu runner the job takes about 26 s for `_clips/demo`, build steps included.

### D12. A driven node draws the transform its caller supplies

`Node.Driven(track)` adds a node whose local transform at `t` is `track.At(t)`. kinetograph calls `At` at every
time it evaluates the node and uses the result as given: it never interpolates between two results, never holds a
result for a later time, and keeps no result. The node is for poses no scalar channel describes, such as the
certified poses decad's `dynamics.Timeline` returns: a pose blended between two certified poses has no certificate
behind it (decad `docs/multibody-dynamics-design.md` §11).

- `Local` checks every result as `Fixed` checks its argument: `!IsValid()` is `ErrInvalidTransform` and
  `IsReflection()` is `ErrReflection`, each naming `t`. D2's rule that no joint produces a reflection therefore
  holds for a driven node.
- An error from `At` fails the frame at that time, wrapped with the time by `Local` and with the part, camera or
  light name by `Scene.At` (§6). kinetograph never draws another pose in place of a failed one.
- `At` MUST return the same transform for the same `t`, and MUST be safe to call from several goroutines at once.
  `Scene.At` calls it once for each part, the camera and each light whose node is the driven node or lies under
  it, so one frame can ask for one `t` several times; `Sequence` workers evaluate frames concurrently; a caller
  may evaluate a frame before rendering it. D9 holds only under this contract.
- kinetograph caches no `At` result, within a frame or across frames. A track whose `At` is costly caches on its
  own side.

`TransformTrack` is kinetograph's type. kinetograph imports no decad package that exists only for dynamics, and
decad's `_gallery` adapts a `dynamics.Timeline` to the interface; `r3.Transform` is the one type the two share.

### D13. A linkage track asks decad for every pose

`LinkageTrack` (pass 6, §5.9) and `ScheduleTrack` (pass 7, §5.11) are the two `TransformTrack`s kinetograph ships.
`LinkageTrack` films a decad `Linkage` moving
along a `Drive`, the motion `Document.VerifyLinkage` checks for collisions. decad builds every pose it checks with
`Linkage.PoseAt(drive, s)`, where `s` is the `Dimensionless` drive fraction. `LinkageTrack.At(t)` calls `PoseAt` at
`s = fraction.At(t)` for every `t` and returns one link's pose as given, so a frame draws exactly the pose decad
checked at that `s`. A pose blended between two `PoseAt` results is a pose decad never checked, so D12's rule
against blending carries over unchanged.

- The time-to-`s` map is a kinetograph `Channel` (D1). Holds, easing and repeats are keyframes, and the channel's
  hold before its first keyframe and after its last is the drive's hold at its ends.
- `PoseAt` returns world poses, so a track's node sits directly under the rig's root, where `Local` is `World`.
  `Scene.AddLinkage` builds those nodes.
- The `Linkage`, `Link` and `Drive` types live in decad's root package, which kinetograph already imports. The
  `Drive` is passed to `PoseAt` unchanged, waypoints (`JointSweep.Via`) included: the track never reads its shape.
- A drive that moves a closed loop (decad `Linkage.Close`) states one joint of the loop, and decad reads every
  other loop joint from certified enclosures it asks `sketch` for. `Linkage.PoseAt` builds a `decad.Schedule` for
  such a drive on every call and discards it. `ScheduleTrack.At` calls `Schedule.PoseAt` on one schedule the
  caller built once. `VerifyLinkage` reads every pose of a looped drive through a schedule of the same inputs,
  and decad returns equal poses bit for bit from two schedules of the same inputs, so both tracks draw the pose
  decad checked. The schedule caches the enclosures it asked for; that cache is decad's, and kinetograph still
  caches no `At` result (D12).

## 5. Public API

Signatures are normative; doc comments on the implementation carry the detail. Every constructor validates what
it is handed and returns an error from §6's vocabulary rather than a value that would fail later. §5.1 to §5.6 are
the initial pass; §5.7 and §5.8 are pass 2, which adds declarations and changes no initial-pass signature; §5.9 is
pass 6, which does the same.

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

// TransformTrack supplies a driven node's local transform at a time (pass 5, D12). At MUST return the same
// transform for the same t, and MUST be safe to call from several goroutines at once.
type TransformTrack interface {
    // At returns the transform at t. kinetograph uses the result as the node's Local(t); it never blends two
    // results.
    At(t time.Duration) (r3.Transform, error)
}

// Driven adds a child whose Local(t) is track.At(t) (pass 5, D12). It returns ErrNilTrack for a nil track.
func (n *Node) Driven(track TransformTrack) (*Node, error)

// Local returns the joint's own transform at t. For a driven node it returns At's error wrapped with t,
// ErrInvalidTransform when the result is not a rigid motion, and ErrReflection when it mirrors.
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

Pass 3 (§9) adds `Appearance.Fade`, `LightAppearance` and `Style.Lights`, and their checks in `New`.

`Frame` builds a `solidlens.Scene` whose `Models` are in `Pose` order; each model's `Mesh` is a private
`TriangleSource` holding the part's triangle indices and its vertices under `Pose.Transform.Apply`. The camera is
`solidlens.Camera{Position, Target, Up, FOV: fov.In(units.Degree)}`.

Each frame file is written to a temporary name in `dir` and renamed into place once `png.Encode` returns, so a
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

### 5.9 `linkage.go`: decad linkages (pass 6)

```go
// LinkageTrack is the TransformTrack of one link of a decad linkage moving along a drive (D13). It holds no
// mutable state.
type LinkageTrack struct { /* unexported */ }

// NewLinkageTrack returns the track of link under drive, reading the drive fraction s from fraction. It returns
// ErrNilLinkage for a nil linkage, ErrForeignLink unless link is one of linkage.Links() (nil and the ground link
// are not), ErrNilChannel for a nil fraction, ErrKind unless fraction.Kind() == units.Dimensionless, and decad's
// own error, wrapped, when linkage.PoseAt(drive, 0) refuses the drive.
func NewLinkageTrack(linkage *decad.Linkage, drive decad.Drive, link *decad.Link, fraction *Channel) (*LinkageTrack, error)

// At returns the link's pose from linkage.PoseAt(drive, s) with s = fraction.At(t). It returns the channel's error
// wrapped, and PoseAt's error wrapped with s.
func (k *LinkageTrack) At(t time.Duration) (r3.Transform, error)

// AddLinkage adds one driven node per link of linkage under the rig's root, each driven by a LinkageTrack over
// drive and fraction, and attaches every body of every link to its link's node as a part named names[body]. It
// returns the nodes in Linkage.Links() order. It returns NewLinkageTrack's errors, ErrUnnamedBody for a link body
// with no entry in names, and ErrDuplicateName for a name another part, or another body of the linkage, already
// uses. A failed AddLinkage adds no part.
func (s *Scene) AddLinkage(linkage *decad.Linkage, drive decad.Drive, fraction *Channel,
    names map[*decad.Body]string) ([]*Node, error)
```

**Construction.** `NewLinkageTrack` finds the link's position in `linkage.Links()` once and keeps it. decad gives a
link its position when the link is created and only appends links, so the position never changes. It copies
`drive`, each sweep's `Via` included, so a caller that changes its slice afterwards does not change the track. It
calls `PoseAt(drive, units.Scalar(0))` once to run decad's checks of the drive (a sweep naming a link of another
linkage, a waypoint of the wrong kind, a waypoint outside a joint's limits) and keeps nothing from the result.

**Evaluation.** `At` evaluates `fraction.At(t)` and passes the value to `PoseAt` as it is: kinetograph does not
clamp it and does not read its unit. `PoseAt` accepts any finite `s`, and extends the drive's first and last
segments outside [0, 1], so a channel that leaves [0, 1] shows the linkage past the drive's ends. `At` keeps no
result (D12). `PoseAt` writes nothing and composes one transform per joint on the link's path, so `At` is safe
from several goroutines and returns the same transform for the same `t`. The linkage itself is read on every call:
a caller MUST NOT add links to it, or change a link's bodies, while a scene that reads it is evaluated.

**Exact fractions.** A frame shows exactly the `s` that `VerifyLinkage` checked when `fraction.At(t)` returns that
value bit for bit at the frame's time. Take a clip at 64 fps, where frame `i` falls at exactly `i·15625000 ns`, and
a `Linear` fraction from `units.Scalar(0)` at time 0 to `units.Scalar(1)` at frame `n`'s time. `Channel.At`
computes `u` as the quotient of two whole numbers of nanoseconds and returns `0 + 1·u`, so frame `i` reads `i/n`
correctly rounded, which is `i/n` exactly when `n` is a power of two. `VerifyLinkage` at resolution `1/n` with `n`
a power of two checks fractions on that same grid. A frame rate that does not divide one second into whole
nanoseconds (24 fps does not) puts frame times between grid points.

**AddLinkage.** It builds every track and node and checks every name before it adds the first part. Parts go in
`Linkage.Links()` order and, within a link, in `Link.Bodies()` order; `names` is only looked up, never iterated
(§7). An entry for a body that no link holds is ignored, so one map may name the static bodies the caller attaches
with `AddPart`. A caller attaches a static body, a second copy of a link body (a tint from a collision on, say), a
light or the camera to the root or to a returned node with the existing calls.

### 5.10 Executable example, pass 6

`examples/kinetograph_linkage_example_test.go` builds a two-link arm: an upper arm turning 0° to 90° about Z at
the origin, and a forearm turning 0° to −90° about Z at the elbow, (40, 0, 0) at the zero pose. It films the drive
with `AddLinkage` and a `Linear` fraction from 0 at 0 s to 1 at 1 s, as a 6-frame clip at 4 fps whose last frame,
at 1.25 s, holds `s = 1`. It renders the clip into a temporary directory and prints each frame's `s`, the forearm
tip's world position and the file names. The two turns cancel, so the forearm keeps its orientation and its tip,
(80, 0, 0) at the zero pose, sweeps a quarter circle of radius 40 mm about (40, 0, 0), from (80, 0, 0) to
(40, 40, 0). It is the end-to-end instance pass 6 is accepted on.

### 5.11 `linkage.go`: linkage schedules (pass 7)

```go
// ScheduleTrack is the TransformTrack of one link of a decad linkage moving along a decad.Schedule (D13). It
// holds no mutable state of its own.
type ScheduleTrack struct { /* unexported */ }

// NewScheduleTrack returns the track of link under schedule, reading the drive fraction s from fraction. It
// returns ErrNilSchedule for a nil schedule, ErrForeignLink unless link is one of schedule.Linkage().Links() (nil
// and the ground link are not), ErrNilChannel for a nil fraction, and ErrKind unless
// fraction.Kind() == units.Dimensionless.
func NewScheduleTrack(schedule *decad.Schedule, link *decad.Link, fraction *Channel) (*ScheduleTrack, error)

// At returns the link's pose from schedule.PoseAt(context.Background(), s) with s = fraction.At(t). It returns
// the channel's error wrapped, and PoseAt's error wrapped with s.
func (k *ScheduleTrack) At(t time.Duration) (r3.Transform, error)

// AddSchedule is AddLinkage for a schedule: one driven node per link of schedule.Linkage() under the rig's root,
// each driven by a ScheduleTrack over schedule and fraction, and every link body attached as AddLinkage attaches
// it. It returns ErrNilSchedule, NewScheduleTrack's errors for fraction, and AddLinkage's errors for names.
func (s *Scene) AddSchedule(schedule *decad.Schedule, fraction *Channel,
    names map[*decad.Body]string) ([]*Node, error)
```

**Construction.** `NewScheduleTrack` finds the link's position in `schedule.Linkage().Links()` once and keeps it,
as `NewLinkageTrack` does (§5.9). `Linkage.Schedule` has already checked the drive, so `NewScheduleTrack` and
`AddSchedule` call no `PoseAt`, and copy nothing: `Schedule.Drive` returns a copy, and the schedule keeps its own.

**Evaluation.** `At` evaluates `fraction.At(t)` and passes the value to `Schedule.PoseAt` as it is, under
`context.Background()`, because `TransformTrack.At` takes no context. `At` keeps no result (D12). `Schedule.PoseAt`
is safe for concurrent callers, so `At` is safe from several goroutines and returns the same transform for the
same `t`. For a tree linkage `Schedule.PoseAt` is `Linkage.PoseAt`, and an `s` outside [0, 1] extends the drive
as §5.9 says. For a drive that moves a loop, decad refuses an `s` outside [0, 1] with `ErrUnsupported`, and
refuses an `s` it cannot certify the loop at (a four-bar driven past its fold) with `ErrUnsupported` wrapping
`sketch`'s error. A `Linear` fraction from 0 to 1 holds its ends outside its keyframes and never leaves [0, 1].
§5.9's exact-fraction rule holds unchanged.

**AddSchedule.** It adds nodes and parts exactly as `AddLinkage` does (§5.9), over `schedule.Linkage()`.

### 5.12 Executable example, pass 7

`examples/kinetograph_schedule_example_test.go` builds decad's crank-rocker: ground 100 mm, a crank of 30 mm
turning 0° to 90° about Z at the origin, a coupler of 80 mm hung from the crank, and a follower of 70 mm turning
about Z at (100, 0, 0), closed onto the coupler at the pin above the ground line. It builds one `Schedule` for the
crank's drive, films it with `AddSchedule` and a `Linear` fraction from 0 at 0 s to 1 at 1 s, as a 5-frame clip at
4 fps, and renders it into a temporary directory. It prints each frame's `s`, the pin's world position and the
follower's angle from the ground line, which match the four-bar's two-circle construction (110.3° at `s = 0`,
113.3° at `s = 1`), and the file names. It is the end-to-end instance pass 7 is accepted on.

## 6. Error behaviour

Sentinels live in `errors.go`. Every error a constructor returns wraps one of them, so `errors.Is` branches; the
message names the offending argument.

| Condition | Error | Where |
|---|---|---|
| no keyframes | `ErrNoKeyframes` | `NewChannel` |
| keyframe times not strictly increasing | `ErrKeyframeOrder` | `NewChannel` |
| keyframes of two kinds; a channel of the wrong kind for its joint or the camera | `ErrKind` | `NewChannel`, `Revolute`, `Prismatic`, `SetCamera`, `render.New` (chord) |
| pass 3: a light `Intensity` or a part `Fade` channel that is not `Dimensionless` | `ErrKind` | `render.New` |
| non-finite keyframe value | `units.ErrNotFinite` (wrapped) | `NewChannel` |
| interpolation overflows | `units.ErrNotFinite` (wrapped) | `Channel.At`, surfaced by `Node.World`, `Scene.At`; pass 3: an `Intensity` or `Fade` channel, surfaced as a `*render.FrameError` at that frame |
| fixed transform is a reflection; pass 5: a driven node's track returns one | `ErrReflection` | `Node.Fixed`; pass 5: `Node.Local` (wrapped with the time), surfaced by `Node.World`, `Scene.At` |
| fixed transform is not a rigid motion (`!IsValid()`); pass 5: a driven node's track returns one | `ErrInvalidTransform` | `Node.Fixed`; pass 5: `Node.Local` (wrapped with the time), surfaced by `Node.World`, `Scene.At` |
| pass 5: a driven node's `TransformTrack.At` fails | the track's error, wrapped with the time by `Node.Local` and with the part, camera or light name by `Scene.At` | `Node.Local`, `Node.World`, `Scene.At`, `Scene.AtCached`, `Clip.Frame`, `Clip.FrameCached`, surfaced as a `*render.FrameError` |
| pass 5: nil track | `ErrNilTrack` | `Node.Driven` |
| pass 6: nil linkage | `ErrNilLinkage` | `NewLinkageTrack`, `AddLinkage` |
| pass 6: a link that is not one of `linkage.Links()`: nil, the ground link, a link of another linkage | `ErrForeignLink` | `NewLinkageTrack` |
| pass 6: a nil fraction; a fraction that is not `Dimensionless` | `ErrNilChannel`; `ErrKind` | `NewLinkageTrack`, `AddLinkage` |
| pass 6: `PoseAt` refuses the drive | decad's error, wrapped (`errors.Is` reaches decad's sentinel) | `NewLinkageTrack`, `AddLinkage` |
| pass 7: nil schedule | `ErrNilSchedule` | `NewScheduleTrack`, `AddSchedule` |
| pass 7: a link that is not one of `schedule.Linkage().Links()` | `ErrForeignLink` | `NewScheduleTrack` |
| pass 7: a nil fraction; a fraction that is not `Dimensionless` | `ErrNilChannel`; `ErrKind` | `NewScheduleTrack`, `AddSchedule` |
| pass 7: a link body with no entry in `names`; two link bodies given one name, or a name a part already uses | `ErrUnnamedBody`; `ErrDuplicateName` | `AddSchedule` |
| pass 7: the fraction channel or `Schedule.PoseAt` fails at `t` (an `s` outside [0, 1] or past a fold on a looped drive among them) | the error, wrapped by `ScheduleTrack.At` (with `s` for `PoseAt`), then as any track error (pass 5 row) | `ScheduleTrack.At`, then as pass 5 |
| pass 6: a link body with no entry in `names` | `ErrUnnamedBody` | `AddLinkage` |
| pass 6: the fraction channel or `PoseAt` fails at `t` | the error, wrapped by `LinkageTrack.At` (with `s` for `PoseAt`), then as any track error (pass 5 row) | `LinkageTrack.At`, then as pass 5 |
| zero or non-finite revolute axis | `r3.ErrDegenerateAxis` (passed through) | `Revolute` |
| zero or non-finite prismatic direction; pass 3: zero or non-finite directional light direction | `ErrDegenerateDirection` | `Prismatic`, `AddLight` |
| pass 3: light kind neither `PointLight` nor `DirectionalLight`; a non-zero vector the kind does not read; a non-finite point light position | `ErrInvalidLight` | `AddLight` |
| transform composition overflows | `r3.ErrNonFinite` / `r3.ErrNotOrthonormal` (passed through) | `Node.World`, surfaced by `Scene.At` |
| part name already used; pass 3: light name already used by another light; pass 6: two link bodies given one name | `ErrDuplicateName` | `AddPart`, `AddParametric`, `AddLight`, `AddLinkage` |
| node belongs to another rig | `ErrForeignNode` | `AddPart`, `AddParametric`, `SetCamera`, `AddLight` |
| nil body, nil channel | `ErrNilBody`, `ErrNilChannel` | `AddPart`, `SetCamera`, `Revolute`, `Prismatic`, `AddParametric` (a nil parameter channel); pass 3: `render.New` for a nil light `Intensity` |
| nil builder | `ErrNilBuilder` | `AddParametric` |
| parameter value with no text form | `units.ErrUnnamedKind` / `units.ErrOverflowedKind` (wrapped) | `AddParametric` (keyframe values), `Scene.At` (an evaluated value) |
| `Build` fails | the `Builder`'s error, wrapped with the part name and time | `Scene.At`, `Scene.AtCached`, `Clip.Frame`, `Clip.FrameCached` |
| `Build` returns a nil body and a nil error | `ErrNilBody` (wrapped with the part name and time) | `Scene.At`, `Scene.AtCached`, `Clip.Frame`, `Clip.FrameCached` |
| no camera, no parts | `ErrNoCamera`, `ErrEmptyScene` | `Scene.At`, `NewClip` |
| fps < 1 or duration <= 0 | `ErrInvalidClip` | `NewClip` |
| frame index outside the clip | `ErrFrameRange` | `Clip.Frame`, `Renderer.Frame` |
| nil context | `ErrNilContext` | every function taking one, before any work |
| style has a non-positive dimension or an unknown part name | `render.ErrStyle` | `render.New` |
| pass 3: a scene light with no `Style.Lights` entry; a `Style.Lights` name no light carries; a light with the zero `Color`; an `Intensity` keyframe below 0; a `Fade` keyframe outside [0, 1] | `render.ErrStyle`, naming the light or part (`Default` for the default appearance) | `render.New` |
| tessellation fails | decad's error, wrapped with the part name | `render.New`; for a rebuilt body, inside the frame's `FrameError` |
| pass 3: a light's node cannot be posed at `t` | `r3` or `units` error, wrapped with the light name | `Scene.At`, `Scene.AtCached`, surfaced as a `*render.FrameError` |
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
| A driven node's `TransformTrack.At` returns the same transform for the same `t` (D12); kinetograph passes `FrameTime(i)` to it unchanged | how often, and on which goroutine, `At` is called cannot reach the bytes |
| A `LinkageTrack` passes `fraction.At(t)` to `PoseAt` unchanged; `AddLinkage` adds parts in `Linkage.Links()` then `Link.Bodies()` order and only looks `names` up | a linkage frame depends on `t` and the inputs alone, and its parts are in the same order on every run |
| A `ScheduleTrack` passes `fraction.At(t)` to `Schedule.PoseAt` unchanged, and decad returns equal poses for equal `s` from one schedule or two of the same inputs; `AddSchedule` adds parts as `AddLinkage` does | a looped linkage's frame depends on `t` and the inputs alone, whichever worker renders it |
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
| `doc.go` | Package doc: scope, layering (`kinetograph -> decad, r3, units`; `render -> solidlens`), D1–D9 by name, and reshape (§5.7). Pass 3: the root package names a light only as a pose. Pass 5: a driven node (D12). Pass 6: a decad linkage (D13). Pass 7: a decad schedule (D13). |
| `errors.go` | The sentinel vocabulary of §6. Pass 3 adds `ErrInvalidLight` and widens `ErrDuplicateName` and `ErrDegenerateDirection` to `AddLight`. Pass 5 adds `ErrNilTrack` and widens `ErrReflection` and `ErrInvalidTransform` to a driven node's `Local`. Pass 6 adds `ErrNilLinkage`, `ErrForeignLink` and `ErrUnnamedBody` and widens `ErrDuplicateName` to `AddLinkage`. Pass 7 adds `ErrNilSchedule` and widens `ErrForeignLink`, `ErrUnnamedBody` and `ErrDuplicateName` to `NewScheduleTrack` and `AddSchedule`. |
| `channel.go` | `Easing` and the five provided easings, `Keyframe`, `Channel`, `NewChannel`, `Constant`. Pass 3 adds `Keyframes`. |
| `rig.go` | `Rig`, `Node`, the three joint constructors, `Local`, `World`. Pass 5 adds `TransformTrack` and `Driven`. |
| `camera.go` | `Camera`, `CameraPose`, and the node-local to world evaluation. |
| `light.go` (pass 3) | `LightKind`, `Light`, `LightPose`, and the node-local to world evaluation. |
| `scene.go` | `Scene`, `Pose`, `Frame`, `PartInfo`, `AddPart`, `SetCamera`, `Parts`, `At`, `AtCached`. Pass 3 adds `AddLight`, `LightInfo`, `Lights` and `Frame.Lights`. |
| `reshape.go` | `Params`, `Builder`, `AddParametric`, `BuildCache`, `NewBuildCache`, and the cache key encoding. |
| `clip.go` | `Clip`, `NewClip`, `FrameCount`, `FrameTime`, `Frame`, `FrameCached`. |
| `linkage.go` (pass 6, pass 7) | `LinkageTrack`, `NewLinkageTrack`, `Scene.AddLinkage`; pass 7 adds `ScheduleTrack`, `NewScheduleTrack`, `Scene.AddSchedule`. |
| `internal/memo/memo.go` | `memo.Map`: calls a function once per key, hands concurrent callers for that key the same result, and drops a result whose caller's ctx was done. Backs `BuildCache` and `render`'s mesh cache. |
| `render/style.go` | `Appearance`, `Style`, `ErrStyle`, style validation. Pass 3 adds `Appearance.Fade`, `LightAppearance`, `Style.Lights` and their validation. |
| `render/renderer.go` | `Renderer`, `New`, `Frame`; the posed `TriangleSource`; the per-call build and mesh caches; the solidlens scene assembly. Pass 3 adds the node lights, the fade and intensity evaluation and the hidden/opaque/fading grouping. |
| `render/fade.go` (pass 3) | The near-to-far order of fading parts, the layer scenes `S_j` and the composite of §9 pass 3. |
| `render/sequence.go` | `Sequence`, `Renderer.Sequence`, `SequenceOption`, `FrameError`, atomic frame file writes. Pass 3 encodes the image `Frame` returns with `png.Encode`. |
| `examples/` | `Example_kinetograph_*` with verified `// Output:` blocks. Never `package main`. |
| `_clips/demo/` | The demo clip program, its own module (D11): `parts.go` builds the bodies, `scene.go` the rig, channels and style, `main.go` the flags and the `Sequence` call. |
| `docs/design.md` | This document. |
| `_gallery/` | The README's GIF program, its own module (D11): `hero.go` and `features.go` build each GIF's scene and style, `parts.go` the bodies, `style.go` the palette, lights and table camera, `shots.go` the shot table and `-only`, `assemble.go` the ffmpeg script, `main.go` the flags and the `Sequence` calls. |
| `docs/images/` | The README's GIFs, written by the `_gallery/` program's ffmpeg script. |
| `.github/workflows/ci.yml` | golangci-lint v2.12.2 and `go vet`; `go test -race ./...` on ubuntu, `go test ./...` on macOS and Windows; `go mod tidy` diff; govulncheck. Copied from decad's with the shard matrix removed. The `clips` job checks each `_clips/*` module and `_gallery/` (D11, "Nested modules in CI"). |
| `.golangci.yml` | decad's house config, copied. |


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

- Reshape: it is pass 2.
- Animated lights: in pass 1, lights are static in `Style`; the pass-1 clip does not need them to move. Pass 3
  adds them.
- Vector channels: a moving camera target is a camera on a moving node (D5), so no `r3.Vec` is ever interpolated
  in pass 1.
- A program that renders clips: it is its own module (D11).

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
part by rendering the frame in layers, with and without each fading part, and mixing the layers from the farthest
fading part to the nearest.

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
// or non-finite DirectionalLight Direction. AddLight is not safe beside At or AtCached.
func (s *Scene) AddLight(name string, node *Node, light Light) error

// LightInfo is one light as AddLight attached it.
type LightInfo struct {
    Name string
    Kind LightKind
}

// Lights returns the scene's lights in AddLight order. Like Parts, it evaluates nothing and calls no Builder.
func (s *Scene) Lights() []LightInfo

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

`AddLight` normalizes a `DirectionalLight` direction once with `Vec.Normalize`, as `Prismatic` does. `AtCached` (and
so `Scene.At`, `Clip.Frame` and `Clip.FrameCached`) poses each light with its node's `World(t)`: `Apply` for a
position, `ApplyDir` for the stored unit direction, the same two calls the camera uses (D5). A light that circles a
part is a light on a revolute node; a light fixed in the world is a light on `rig.Root()`. There is no
light-specific motion type.

`ErrInvalidLight` is a new sentinel in `errors.go`. `Lights` exists so that `render.New` can match `Style.Lights`
against the scene's lights without evaluating a frame, as it reads the parts with `Parts` (§5.7). `Keyframes` exists
so that `render.New` can check a fade's and an intensity's keyframe values (below) without the root package knowing
what a fade is.

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
    // 0.2126·R + 0.7152·G + 0.0722·B, and does not tint the surface (§11). The zero Color is refused by New:
    // its luminance is 0, so the light could never add anything, and a light is dimmed through Intensity.
    Color solidlens.Color
    // Intensity is solidlens's light Intensity over time, a Dimensionless channel whose keyframes are >= 0.
    // A point light's contribution falls off as 1 / max(1, d²) with d in mesh units, which are millimetres in
    // kinetograph, so a point light 100 mm from a face needs an intensity near 10⁴ to light it as strongly as a
    // directional light of intensity 1.
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

1. Every light `Scene.Lights` returns must have a `Style.Lights` entry, and every `Style.Lights` name must belong to
   a light; either failure is `ErrStyle` naming the light. Names are checked in sorted order, so the reported name
   does not depend on map order.
2. Each `LightAppearance`, in sorted light-name order: a `Color` equal to `solidlens.Color{}` is `ErrStyle`; a nil
   `Intensity` is `kinetograph.ErrNilChannel`, a kind other than `units.Dimensionless` is `kinetograph.ErrKind`,
   and a keyframe below 0 is `ErrStyle`.
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
opaque (fade 1, or no `Fade`) and fading (strictly between). It poses every non-hidden part's mesh once, as §5.5
does. With no fading part, the frame is one `solidlens.Render` call over the opaque parts, which is the scene §5.5
builds when no part is hidden.

With `n` fading parts, `render` orders them near to far. A part's sort key is the view depth of the centre of its
posed vertices' axis-aligned bounds: `centre.Sub(camera.Position).Dot(forward)`, where `forward` is
`camera.Target.Sub(camera.Position)` under `Vec.Normalize`. The nearest part is `1` and the farthest is `n`; equal
keys keep the order the parts were added in (`Scene.Parts` order, which counts `AddPart` and `AddParametric`
alike). The layer scenes reuse the posed meshes:

- `S_j`, for `j = 1 … n`: the opaque parts and the fading parts `j … n`.
- `S_{n+1}`: the opaque parts alone.

The composite is built from the far end, pixel by pixel, with `f_j` the fade of part `j`:

```
R_{n+1} = S_{n+1}
R_j     = f_j · S_j + (1 − f_j) · R_{j+1}   where pixel S_j differs from pixel S_{j+1} in any of its four bytes
R_j     = R_{j+1}                           everywhere else
out     = clamp(round(R_1), 0, 255)
```

A pixel where `S_j` and `S_{j+1}` agree is one where part `j` draws nothing visible. Mixing `S_j` in there would
pull the pixel back to the unfaded scene behind it and undo the farther parts' fades, so the pixel keeps `R_{j+1}`.
`R` is carried in `float64` from layer to layer and rounded half up once, at the end, as solidlens's own edge
blending rounds.

For one fading part this is `f · S_1 + (1 − f) · S_2`: where the part is the nearest surface, `S_1` shows the part
and `S_2` shows what is behind it. The part's edge lines fade with it, and the edge lines of a part behind it show
through, because each layer carries its own edges. The mix is of the 8-bit sRGB values solidlens writes, not of
linear light: a red part at fade 0.5 over a white background gives the bytes (255, 128, 128).

Where a fading part lies in front of another fading part, the farther part's own fade reaches the pixel through
`R_{j+1}`, so the result changes continuously with every `f`: a farther part fading in from 0 changes the pixel by
at most `ceil(255 · f)` per channel. The result is approximate in three places:

- The order is per part, not per pixel. Parts that pass through each other, or a part whose bounds centre is
  nearer although its surface is behind another part at some pixel, are layered in the wrong order there.
- A part drawn in the same bytes as the layer behind it, at a pixel, counts as drawing nothing at that pixel, and
  the farther parts' fades show there unmixed with it.
- Where the edge lines of two fading parts cover the same pixel, solidlens has already blended each line by its
  antialiasing coverage, and mixing those blended bytes is not the blend of the two partly covered lines.

An exact result needs depth-sorted blending inside solidlens (§11).

The layers are rendered one at a time, far to near (`S_{n+1}` first), on the goroutine that renders the frame;
`Sequence` already renders several frames at once. `render` holds one `float64` accumulator for `R`, the previous
layer's image and the current layer's image, so a frame's memory does not grow with `n`. A frame with `n` fading
parts costs `n + 1` renders; pass 3 accepts that cost. `ctx` is checked before each layer, and a done `ctx` returns
`ctx.Err()` unwrapped, as every other cancellation in §6 does.

`Renderer.Frame` returns the composite. `Sequence` renders each frame through the same grouping and composite,
with the `BuildCache` and mesh cache its workers share (§5.7), and encodes the result with `png.Encode`;
`solidlens.RenderPNG` is `Render` followed by the same `png.Encode`, so a frame without a fading part writes the
same bytes as a `RenderPNG` call on its one scene.

#### Pass 2 parts

A fade is looked up by `Pose.Name` through `Style.Parts` and `Default`, so a part added with `AddParametric` fades
exactly as one added with `AddPart`; `render.New` checks `Style.Parts` names against `Scene.Parts`, which lists
both. A frame is evaluated once, with `FrameCached` and the call's `BuildCache` (§5.7), before any layer is
rendered. Every layer reuses that frame's posed meshes: for a parametric part, the mesh the call's mesh cache holds
for the `*decad.Body` the `BuildCache` returned. No layer evaluates the frame again, calls `Build` or tessellates.

A fade does not change which bodies are built. `AtCached` builds every parametric part before `render` reads any
fade, so a part at fade 0 is still built and tessellated at that frame. A `Build` error therefore fails the frame
whatever the `Style` says, and the set of failing frames depends on the `Scene` alone.

#### Errors

§6 lists pass 3's error conditions, marked "pass 3". Two existing sentinels widen: `ErrDuplicateName`'s message
becomes "kinetograph: name already used" and its doc names `AddLight` beside `AddPart`, and
`ErrDegenerateDirection`'s doc names `AddLight` beside `Node.Prismatic`.

#### Determinism

§7's rules hold, with these additions:

| Rule | Why |
|---|---|
| Light poses are in `AddLight` order; solidlens lights are the `Style` lights, then the node lights in that order | solidlens sums light contributions in the order given |
| `Style.Lights` and `Style.Parts` are looked up by name; validation walks their names in sorted order | the reported error does not depend on map order |
| Fading parts are layered by the view depth of their posed bounds centre, computed with `r3` (`Sub`, `Dot`, `Normalize`), ties broken by `Scene.Parts` order | the layer order depends only on the frame's poses and camera |
| Every product in the mix is wrapped in an explicit `float64(…)` conversion, and `R` is rounded once, at the end | the Go spec forbids fusing a product into an FMA across an explicit conversion, so the mix is the same on amd64 and arm64 for the same layer bytes |
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
| light | `Scene.Lights` on a scene with a point and a directional light | names and kinds in `AddLight` order |
| light | a scene light with no `Style.Lights` entry and a parametric part whose `Builder` counts its calls | `render.New` returns `ErrStyle` and the count is 0 |
| light | two lights, one part | `Frame.Lights` names in `AddLight` order; `Frame.Poses` unchanged from a scene without lights |
| light | kind 0; a point light with a non-zero `Direction`; a zero directional `Direction`; a repeated light name; a foreign node | `ErrInvalidLight`, `ErrInvalidLight`, `ErrDegenerateDirection`, `ErrDuplicateName`, `ErrForeignNode` |
| channel | `Keyframes` on a three-key channel with one nil `Ease`; on `Constant` | the keys in order with `Linear` in place of nil; one key at time 0 |
| render | the block's camera-facing face (`Ambient` 0, red), a white directional light travelling +Y with `Intensity` `Constant(0.5)` on a revolute node about Z, frames at 0° and 180° | the image-centre pixel equals `solidlens.RGB(0.5, 0, 0).NRGBA()` at 0° and is black at 180° |
| render | the same light with `Intensity` a `Linear` ramp from 0 to 1 over 1 s, in a 1 s clip at 24 fps, frame 12 (t = 0.5 s) | the centre pixel equals `solidlens.RGB(0.5, 0, 0).NRGBA()` |
| render | a node point light against a `Style.PointLights` entry at the node light's world `Position` with the same color and intensity | the two frames are byte-identical PNG encodings; the same for a directional light |
| render | a `Style` directional light of intensity 0.25 and a node light of 0.25 along the same direction | the centre pixel equals `solidlens.RGB(0.5, 0, 0).NRGBA()` |
| render | a scene light missing from `Style.Lights`; an unknown `Style.Lights` name; the zero `Color`; nil `Intensity`; a Length `Intensity`; an `Intensity` keyframe of −1 | `ErrStyle`, `ErrStyle`, `ErrStyle`, `ErrNilChannel`, `ErrKind`, `ErrStyle` |
| render | an `Intensity` from 0 to `math.MaxFloat64` over 1 s whose easing returns 2 at u = 0.5 and u elsewhere, in a 1 s clip at 24 fps | `Sequence` returns a `*FrameError` with `Index == 12` wrapping `units.ErrNotFinite`; files 0–11 present, no file 12 |
| fade | `Fade` `Constant(0)` on the only part | no pixel differs from the background |
| fade | `Fade` `Constant(1)` against no `Fade` | byte-identical PNG encodings |
| fade | `Fade` `Constant(0.5)` on an edged block over the background | every byte of `Pix` equals `round(b + 0.5 · (a − b))` where `a` and `b` differ and `b` elsewhere, with `a` the fade-1 image and `b` the fade-0 image, rendered by the test through two other renderers |
| fade | flat materials (`Ambient` 1) and no lights: a red block at fade 0.5 in front of a larger opaque blue block | where they overlap the pixel is (128, 0, 128, 255): the blue part shows through |
| fade | two fading blocks without edges side by side, not overlapping, at 0.25 and 0.75 | each block's interior pixels equal its single-part fade formula |
| fade | a red block at fade 0.5 in front of a fading blue block, frames with the blue fade at 0, 0.001, 0.999 and 1 | at an overlap pixel, the 0.001 frame is within 1 of the 0 frame and the 0.999 frame within 1 of the 1 frame on every channel; the 1 frame shows (128, 0, 128, 255) |
| fade | the same two blocks with the red fade at 0, 0.001, 0.999 and 1 and the blue at 0.5 | the same continuity at an overlap pixel |
| fade | the two overlapping fading blocks added near-then-far and far-then-near | byte-identical frames |
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
- A fade mixes layer renders. It needs no solidlens change, a single fading part's result can be recomputed by a
  test from two images it renders itself, and the result changes continuously with every fade, including where
  fading parts overlap.
- An animated light color is out. solidlens reads only a light color's luminance (§11), so an animated color
  would draw the same pixels as an animated intensity.
- Animated material colors and an animated background are out. Pass 3 does not specify them, and adding one
  later is a new `render.Style` channel field, which changes no signature.

### Pass 4, the landing-page clip

decad's landing-page clip is the `clip` subcommand of the `_gallery/` module in
[github.com/lestrrat-3d/decad](https://github.com/lestrrat-3d/decad), which imports kinetograph. In a decad
checkout, `cd _gallery && go run . clip > assemble.sh && sh assemble.sh` renders it. It is built with
passes 1, 2 and 3 and calls kinetograph's public API only.

kinetograph has no type for shots, scripts or transitions. The clip program defines its own: each shot is its own
`Scene`, `Clip` and `Sequence` call on one global clock, and ffmpeg dissolves between shots. Moving any of those
types into kinetograph would be a new public API and needs its own §5 entry first; a second clip program needing
them is the reason to consider it.

§11 lists the solidlens and decad limits a kinetograph caller meets, the clip's among them.

### Pass 5, driven nodes

Pass 5 adds `TransformTrack` and `Node.Driven` (§5.2, D12) and the sentinel `ErrNilTrack`. It changes no earlier
signature. Its first user is decad's `_gallery` module, which films the certified poses of a `dynamics.Timeline`: one
driven node per body under the root, each with a track that samples the timeline at the frame time.

A driven node is a fourth joint kind beside `Fixed`, `Revolute` and `Prismatic`, and nothing else changes:

- `World` composes `Local(t).Then(parent.World(t))` as for every joint, so a driven node may sit under any node and
  any node may sit under it.
- Parts, the camera and lights pose through `Node.World`, so a driven node moves any of them with no scene change.
- `render` never reads a node's kind, and moves the tessellated vertices with `Transform.Apply` (D2), so a driven pose
  reaches the pixels as a revolute pose does.

A track that fails at a frame time fails that frame (§6). `Sequence` returns the `*FrameError` of the lowest failing
frame, as D8 says. With several workers, a frame after the failing one may already be in flight and be written, so a
track that should stop the render at its end MUST fail at every time from its end on, as a timeline that has ended
does. Then no later frame can render.

The acceptance instance is `examples/kinetograph_driven_example_test.go`: a block on a driven node whose track
computes a hop from `t` and refuses any time from 1 s on, rendered as a 4-frame clip at 4 fps. It prints each frame's
translation, shows that the scene fails at 1 s, and prints the file names.

Why each piece is in:

- `TransformTrack` as an interface: the caller holds the state that computes a pose (a timeline, a recording), and
  kinetograph holds only the track.
- The check of every `At` result: a track can return a reflection or a non-rigid transform, and `Fixed` already
  refuses both.
- `ErrNilTrack`: no existing sentinel names a track, and `ErrNilChannel` would name an argument the caller never
  passed.

Why each piece is out:

- A cache of `At` results inside one evaluation: a frame with several attachments on one driven node calls `At`
  once for each. decad's gallery puts one part on each driven node, and a track can cache on its own side.
- A vector or transform channel: a driven node interpolates nothing, so D1 stays the rule for every quantity
  kinetograph interpolates.

### Pass 6, decad linkages

Pass 6 adds `LinkageTrack`, `NewLinkageTrack`, `Scene.AddLinkage` (§5.9, D13) and the sentinels `ErrNilLinkage`,
`ErrForeignLink` and `ErrUnnamedBody`. It changes no earlier signature. It requires decad at `0070f0b2` or later,
the first commit whose drives pass through waypoints (`JointSweep.Via`). Its first user is decad's `_gallery`
module, which films a two-link arm that `VerifyLinkage` checks and marks the frame of its first collision.

The acceptance instance is §5.10's example.

Why each piece is in:

- `LinkageTrack`: the adapter from `PoseAt` to `TransformTrack` is the same for every caller, and its
  bit-for-bit agreement with `PoseAt` is a test kinetograph can own.
- The fraction as a `Channel`: holds, easing and repeats of the drive are keyframes, as every other animated
  quantity is (D1), and a caller who needs frame `i` to show a checked `s` exactly gets that from a `Linear`
  channel (§5.9).
- `AddLinkage`: a linkage's nodes are one driven node per link under the root, and its parts are the links'
  bodies. One call builds both, so a caller cannot put a link's body on another link's node.
- `ErrForeignLink` and `ErrUnnamedBody`: no existing sentinel names a link or a body without a name.

Why each piece is out:

- A collision tint, a marker at the first collision, or any other use of a `LinkageReport`: those are scene
  choices, made with `AddPart`, `render.Style` and `Appearance.Fade` (pass 3) by the caller.
- A parent node for `AddLinkage`: `PoseAt` returns world poses, and the static bodies of the document sit in the
  world frame too. A caller who wants the whole mechanism under a moving node calls `Node.Driven` with
  `NewLinkageTrack` there, and attaches the static bodies to the same node.

### Pass 7, linkage schedules

Pass 7 adds `ScheduleTrack`, `NewScheduleTrack`, `Scene.AddSchedule` (§5.11, D13) and the sentinel
`ErrNilSchedule`. It changes no earlier signature. It requires decad with `Schedule.Linkage()`, the accessor that
maps a link to its position in a schedule's poses. Its first user is decad's `_gallery` module, which films the
crank-rocker of decad's `docs/linkage-check-design.md` §15.10 (scene 7) with a track of its own today.

The acceptance instance is §5.12's example.

Why each piece is in:

- `ScheduleTrack`: `Linkage.PoseAt` on a looped drive builds the loop's scene, its zero pose and the drive's
  decomposition on every call, which costs about 0.1 s to 0.4 s per call on the crank-rocker, against one
  `Schedule` built once. Every frame asks one pose per link.
- `AddSchedule`: the nodes and parts of a schedule are those of its linkage, so one call builds both, as
  `AddLinkage` does.
- `ErrNilSchedule`: `ErrNilLinkage` would name an argument the caller never passed.

Why each piece is out:

- `AddLinkage` choosing between a drive and a schedule: one Go signature cannot take either without an `any`
  argument, and a sibling call keeps both signatures typed.
- A context on `ScheduleTrack`: `TransformTrack.At` takes none (D12). A caller that must cancel a render cancels
  the `Sequence` context, and the frame in flight finishes its `PoseAt` call.

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
| driven | a driven node under the root, track keyed at 0, 250 ms and 500 ms with three rotation-plus-translation transforms | `Local(t)` and `World(t)` equal each table transform with `Equal(..., 0)`; a time between entries is asked of the track and fails |
| driven | a `Fixed` child and a `Revolute` child under a driven node, and a driven node under a `Prismatic` node | `World(t)` equals `child.Local(t).Then(parent.World(t))` computed independently with `r3`, within 1e-12 |
| driven | `Driven(nil)`; a track returning `Transform{}`; a track returning a reflection; a failing track | `ErrNilTrack`; `ErrInvalidTransform` and `ErrReflection` from `Local(t)` and from a child's `World(t)`, each message naming `t`; the track's error from `Local` and `World` |
| driven | a 6-frame clip at 24 fps with a part on a driven node whose track records every `t` | every recorded `t` is some `FrameTime(i)`; each `Pose.Transform` equals the table entry with `Equal(..., 0)` |
| driven | a track failing with a sentinel at `FrameTime(3)` only, under a part, the camera and a light | `Clip.Frame(2)` and `Frame(4)` succeed; `Frame(3)` fails, `errors.Is` reaches the sentinel and the message names the part, camera or light and 125ms |
| driven | a track that cancels ctx and then fails | `Scene.AtCached` returns `ctx.Err()` unwrapped |
| driven | `Sequence` of 6 frames whose track fails from frame 3 on, with 1 and with 3 workers | `*render.FrameError` with `Index == 3` and `Time == FrameTime(3)`, `errors.Is` reaches the sentinel; exactly files 0–2 |
| driven | a block on a driven node whose track returns `T`, against the same block on `Fixed(T)` | the two PNG encodings of frame 0 are byte-identical |
| driven | a block on a driven node translating 12 mm along +X between frames 0 and 5 | the blob centroid's x increases by at least 2 px, y unchanged within 1 px |
| driven | `Sequence` of a driven clip with 1 and with 3 workers, under `go test -race` in CI | the files are byte-identical between the two runs, and the six frames differ |
| examples | `Example_kinetograph_driven` | the `// Output:` block, verified by `go test ./examples/` |
| linkage | the two-link arm, a `Linear` fraction from 0 at 0 s to 1 at 4 s, every link, frame times `i·15625000 ns` for `i` in 0, 1, 85, 86, 171, 256, 300 and −1 | `fraction.At` is `i/256` exactly (0 at −1, 1 at 300); `At` equals `PoseAt(drive, s).Poses[k]` bit for bit, with `k` the link's position in `Links()` |
| linkage | the same arm under a `SmoothStep` fraction, and under a drive with one `Via` waypoint per sweep | at 9 times, `At` equals `PoseAt(drive, fraction.At(t))` bit for bit; under a `Linear` fraction at the waypoint's `s = 1/2`, the forearm tip is (40·cos 60° + 40·cos 30°, 40·sin 60° + 40·sin 30°, 0) within 1e-9 |
| linkage | the forearm tip under the `Linear` fraction at s = 1/4 | (40·cos 22.5° + 40, 40·sin 22.5°, 0) within 1e-9 |
| linkage | `NewLinkageTrack` with a nil linkage, a nil link, the ground, a link of another linkage, a nil fraction, an Angle fraction, a sweep whose `From` is a Length on a revolute joint | `ErrNilLinkage`, `ErrForeignLink` ×3, `ErrNilChannel`, `ErrKind`, decad's `ErrUnitKind` |
| linkage | change the caller's drive slice (a `To` and a `Via` value) after `NewLinkageTrack` | `At` still equals `PoseAt` of the original drive bit for bit |
| linkage | a fraction from 0 to `math.MaxFloat64` whose easing returns 2 at u = 1/2 | `At` at the midpoint fails, `errors.Is` reaches `units.ErrNotFinite` |
| linkage | 8 goroutines calling `At` at the same 16 times, under `go test -race` in CI | every result bit-identical to `PoseAt` at that time |
| linkage | `AddLinkage` with one body on the upper arm and two on the forearm, a static body by `AddPart` | the returned nodes' `Local(t)` equals `PoseAt` bit for bit, in `Links()` order; `Frame.Poses` names in `Links()` then `Bodies()` order, then the static part; each link part's `Transform` equals `PoseAt` bit for bit; the static part's is the identity |
| linkage | `AddLinkage` with a nil linkage, a missing name, two bodies sharing a name, a name an existing part uses, a nil fraction, a refused drive | `ErrNilLinkage`, `ErrUnnamedBody`, `ErrDuplicateName` ×2, `ErrNilChannel`, decad's error; `Scene.Parts()` unchanged after each |
| linkage | the arm through `render.Sequence` with 1 and 3 workers | the files are byte-identical between the two runs, and the first and last frames differ |
| examples | `Example_kinetograph_linkage` | the `// Output:` block, verified by `go test ./examples/` |
| schedule | the crank-rocker (decad's scene 7) through `AddSchedule`, a `Linear` fraction from 0 at 0 s to 1 at 4 s, frames `i` in 0, 1, 36, 110, 256 at 64 fps | `fraction.At` is `i/256` exactly; every node's `Local` and every part's `Transform` equal `Schedule.PoseAt(i/256).Poses[k]` bit for bit; at `s = 1` the crank carries (30, 0, 0) to (0, 30, 0) within 1e-12, and the follower carries the pin to the two-circle construction's pin at 90° within 1e-8 |
| schedule | the same frames, every link, through `LinkageTrack` (one `Linkage.PoseAt` per call) and `ScheduleTrack`; the two-link arm's waypoint drive the same way at 8 frames from −1 to 300 | the two tracks' transforms equal bit for bit |
| schedule | decad's non-Grashof four-bar (crank 50, coupler 60, follower 50) driven 0° to 90°, past its fold at s ≈ 0.9745 | `At` at s = 1/2 equals `Schedule.PoseAt` bit for bit; `At` at s = 1 returns the zero transform and an error that `errors.Is` reaches `decad.ErrUnsupported` and `sketch.ErrNotCertified` through, naming `s = 1` |
| schedule | `NewScheduleTrack` with a nil schedule, a nil link, the ground, a link of another linkage, a nil fraction, an Angle fraction; a fraction that overflows at 500 ms | `ErrNilSchedule`, `ErrForeignLink` ×3, `ErrNilChannel`, `ErrKind`; `units.ErrNotFinite` from `At` |
| schedule | 8 goroutines calling one crank-rocker `ScheduleTrack` at 16 times each, under `go test -race` in CI | every result bit-identical to `Schedule.PoseAt` at that time |
| schedule | `AddSchedule` with a nil schedule, a nil fraction, a missing name, two bodies sharing a name, a name an existing part uses | `ErrNilSchedule`, `ErrNilChannel`, `ErrUnnamedBody`, `ErrDuplicateName` ×2; `Scene.Parts()` unchanged after each |
| examples | `Example_kinetograph_schedule` | the `// Output:` block, verified by `go test ./examples/` |

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
  the image rather than show the scene behind the part, so pass 3 mixes layer renders instead.
- **Light color does not tint.** solidlens multiplies a light's intensity by the luminance of its `Color`
  (0.2126·R + 0.7152·G + 0.0722·B) and shades the material color with that one number. A red light at intensity
  1 on a white surface draws grey (127, 127, 127). Pass 3 therefore does not animate light color.
- **Zero edge color is black.** `Edges.Color` equal to `Color{}` draws opaque black lines. Fading an edge color
  to zero alpha would turn the lines black at the end of the fade, and edge lines are drawn only after every
  surface, so pass 3 fades a part's edges by leaving the part out of a layer instead.

Two solidlens additions would let pass 3 do less work or be exact:

| Module | Addition | What it would replace |
|---|---|---|
| solidlens | a per-`Model` opacity: opaque models drawn first, then translucent triangles sorted far to near, blended without writing depth, with their edge lines blended at the same opacity | the `n + 1` renders and the composite per fading frame; the blend order would be per triangle rather than per part |
| solidlens | shading each color channel by the light's matching channel | nothing in pass 3; an animated light color would become meaningful |

solidlens has two more limits, read from the pinned `render.go` and seen in the frames of decad's landing-page
clip (§9 pass 4). Neither blocks a pass.

- **Depth is interpolated linearly in screen space.** The rasterizer interpolates each triangle's camera-space
  depth with its screen-space barycentric weights, not perspective-correctly. Inside a large triangle the depth
  is off by enough that a surface just behind it can win: a cylinder standing through a plate whose top face is
  two triangles across shows a crescent of its wall below that face. Where two surfaces meet, the drawn line
  moves by a pixel or two between two tessellations of the same geometry.
- **Each triangle is shaded flat.** One color per triangle, so under a point light two triangulations of one flat
  face differ by a level or two over large triangles, and a point light just above a large flat face lights its
  few triangles as visible wedges.

`units` has no time dimension. D3 chooses `time.Duration` so that no `units` change is needed.

decad cannot tessellate a `Document.Patch` sheet today (`unsupported payload patchPayload`), so a `Patch` body
cannot be rendered. The sheet render test uses an open tube from `Extrude` with `decad.WithSurfaceResult()` instead.
A spatial `Sweep` body builds, and `Tessellate` refuses it (`tessellation does not support payload
decad.sweepPayload`), so it cannot be rendered either.

### decad refusals a `Builder` meets

decad refuses some parameter values a reshape ramp passes through. These results were measured at decad
`v0.0.0-20260930143515-7dde3ae229fd` and at decad commit `d0dc910` by building a
96×68×16 mm plate with three cut holes (a bore of radius 18 mm and two bolt holes of radius 7 mm) at chord 0.05 mm with `VerifyNone`:

| Chain | Result |
|---|---|
| Extrude → Cut ×3 → Fillet r | below 0.032 mm, 21 of the 31 radii in steps of 0.001 mm are refused (`an arc segment's pinned start and end radii differ`), in an irregular pattern (0.003, 0.015 and 0.03 mm build); every radius from 0.032 mm up builds, but about half of them read `Suspect` in `Verify`, with a centroid bound near 2514 mm |
| Extrude → Cut ×3 → Fillet 12 mm → cap-loop chamfer s | from 0.005 to 2 mm in steps of 0.005 mm, 93 of the 400 values build, every multiple of 0.125 mm among them; the rest are refused (`the offset changes the section's topology` or `the cap-loop offset drops a section feature`). Every chamfer that builds reads `Suspect` in `Verify`, because its volume bound is many times the volume it removes |
| Extrude → Fillet 12 mm on the vertical edges → cap-loop chamfer → Cut | refused: `a cap-loop chamfer's mesh carries no proof of the volume it and the body it stands for differ by, so no boolean may compose it` |
| Extrude → blind Cut (tool on a plane offset from XY) → through Cut ×2 (tools on XY) | the third Cut refused: `requested tolerance … is below the faceted body's minimum mesh bound …` |
| Extrude → through Cut ×2 (tools on XY) → blind Cut | builds |

A `Builder` that ramps such a parameter therefore treats a value below the feature's minimum as the feature being
absent, and rounds a value decad accepts only on a grid down to that grid. Its ramp holds 0, steps to the minimum
and grows from there. A refusal at a mid-ramp value is a `*render.FrameError` at that frame (§5.7). `render.New`
calls no `Builder`, and a render of the first frame alone does not reach a later frame's value, so neither finds it. Evaluating every
frame with `Scene.AtCached` through one `BuildCache`, and tessellating each distinct body at the style's chord,
builds every tuple the clip reaches and finds every refusal before a render.

## 12. Where each settled choice lives

Every design choice is stated once, in the section that owns it. This section only points.

| Choice | Owner |
|---|---|
| a program that renders clips is a nested module: `_clips/demo/` and `_gallery/` here | §4 D11 |
| decad's landing-page clip is the `clip` subcommand of decad's `_gallery/` module | §4 D11, §9 pass 4 |
| kinetograph has no shot, script or transition type | §9 pass 4 |
| the camera is a rig attachment and there is no orbit-camera type | §4 D5 |
| every quantity kinetograph interpolates is a scalar channel | §4 D1 |
| a driven node draws its caller's transform, checked and never interpolated or cached | §4 D12, §9 pass 5 |
| kinetograph never imports decad's dynamics code; decad's `_gallery` adapts a timeline to `TransformTrack` | §4 D12 |
| a linkage track calls `Linkage.PoseAt` for every time, at a fraction read from a `Channel` | §4 D13, §5.9 |
| `AddLinkage` puts one driven node per link under the root; collision marks stay with the caller | §5.9, §9 pass 6 |
| a looped linkage is filmed through one `decad.Schedule`, whose `PoseAt` a `ScheduleTrack` calls for every time | §4 D13, §5.11, §9 pass 7 |
| `sketch` is a test-and-example-only dependency | §4 D10 |
| lights are static in pass 1 and move in pass 3 | §4 D6, §9 |
| a light's color and intensity, and a part's fade, are `render.Style` channels bound by name | §4 D6, §9 pass 3 |
| a fade mixes layer renders near to far, not material alpha | §9 pass 3, §11 |
| time is `time.Duration`, a frame rate an `int` | §4 D3, §7 |
| reshape is pass 2 | §3, §9 |
| a reshape cache lives in one render call, never in the `Renderer` | §5.7 |
| the reshape cache key is each value's `MarshalText()` in sorted-name order | §5.7, §7 |
