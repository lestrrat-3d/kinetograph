# CLAUDE.md

Guidance for working in this repository. Read before making structural changes.
Update when a design decision gets resolved.

## What this is

**kinetograph** animates solids built with `decad` (a headless CAD engine) and renders
every frame to a numbered PNG with `solidlens`. A video tool (ffmpeg) assembles the PNGs;
kinetograph writes no video.

- Timeline: every animated quantity is a scalar `Channel` of `units.Value` keyframes with
  easing.
- Rig: joints form a tree. A joint value becomes an `r3.Transform`; a child's world
  transform is its own composed with its parent's. Bodies AND the camera attach to nodes.
- Reshape: a `Builder` makes a part's body from channel values (`Params`); a `BuildCache`
  calls it once per distinct tuple.
- Appearance: lights attach to nodes (`Scene.AddLight`); their color and intensity channel,
  and each part's fade channel, are `render.Style`'s. A fading part is drawn by rendering
  the frame in layers and mixing them far to near (`render/fade.go`).
- Output: `render` builds one solidlens scene per frame and writes one PNG per frame.

**Current state: pass 1 (the initial pass), pass 2 (reshape) and pass 3 (animated
appearance) are implemented.** `docs/design.md` is the contract. §5.1–§5.6 there are the
initial pass's public API, §5.7–§5.8 reshape's, §9 "Pass 3" animated appearance's. §9 also
names pass 4, the landing-page clip in `_clips/decad-landing/`, built with passes 1, 2 and 3
(act B uses pass 1 only), and §12 points at where each settled choice lives.

## Read before you write

| Before writing | Read |
|---|---|
| Any file | `docs/design.md` §8 (package layout: one row per file) |
| Any public type or function | `docs/design.md` §4 (decisions D1–D11), §5 (API), §6 (errors) |
| Channel, easing or interpolation code | `docs/design.md` §5.1, §7 |
| Rig, joint or camera code | `docs/design.md` §4 D1, D2, D5, §5.2, §5.3 |
| Anything under `render/` | `docs/design.md` §5.5, §6, §7; decad's `_gallery/` (the reference use of solidlens) |
| Reshape code (`reshape.go`, `internal/memo/`, render's per-call caches) | `docs/design.md` §5.7, §7 |
| Light or fade code (`light.go`, `render/fade.go`, `render/style.go`) | `docs/design.md` §4 D6, §9 pass 3, §11 |
| Tests | `docs/design.md` §10 |
| Anything the surrounding `.go` file already documents | that file's own doc comments |

## Hard rules

- **Layering is `render -> kinetograph -> decad -> r3 -> units`, with `render -> solidlens`.**
  `render` is the ONLY package that imports solidlens. The root package never names a color
  or a pixel, and names a light only as a kind plus a node-local position or direction
  (pass 3, `docs/design.md` §4 D6). Light color, light intensity and part fade are
  `render.Style`'s. NEVER import kinetograph from decad, solidlens, r3 or units; they do
  not know it exists.
- **Every animated quantity is a scalar channel.** A rotation is an axis plus an `Angle`
  channel through `r3.RotationAround`; a slide is a direction plus a `Length` channel through
  `r3.Translation`. NEVER interpolate an orientation: no quaternion, no slerp, no rotation
  matrix blend. A tumbling body is a chain of revolute joints.
- **A rigid pose moves the tessellated vertices, never the decad body.** Tessellate each part
  once per `(body, chord)` with `decad.WithVerification(decad.VerifyNone)`; per frame apply
  the node's world transform with `r3.Transform.Apply`. NEVER call `Body.Placed` or
  `Body.PlacedCopy` per frame. A reflection (`Transform.IsReflection()`) is `ErrReflection`.
- **A reshape cache lives in one call, never on a receiver.** `Renderer` and `Scene` stay
  immutable; `Renderer.Frame`/`Sequence` make a `BuildCache` and a mesh cache per call. The
  key is each `Value.MarshalText()` in sorted parameter-name order (names sorted once in
  `AddParametric`). Build and tessellation go through `internal/memo`, so several workers
  never build or tessellate one key twice.
- **NEVER hand-roll coordinate math.** Vectors, transforms, composition, normalization →
  `r3`. A unit direction is `Vec.Normalize`; composition is `Then`; `Then` order is
  `child.Local(t).Then(parent.World(t))`.
- **No bare `float64` quantity in the public API.** Angles, distances, tolerances, fields of
  view → `units.Value`; wrong `Kind` → `ErrKind`, never a coercion. Time → `time.Duration`;
  frame rate → `int` frames per second. The ONLY bare values: an `r3.Vec` (millimetres by
  convention, or a direction), `Easing.Ease`'s `u` in `[0, 1]`, `int` counts (frames, pixels),
  and solidlens's own appearance types inside `render.Style`. See `docs/design.md` §4 D4.
- **NEVER skip a frame.** A frame that fails stops the sequence with
  `*render.FrameError{Index, Time, Err}`. Several workers → report the LOWEST failing index.
  Cancellation returns `ctx.Err()` unwrapped. A failed frame leaves no file (write to a temp
  name, rename on success).
- **Determinism is a contract** (`docs/design.md` §7). Frame times are integer nanoseconds.
  NEVER iterate a map into output order, read `time.Now`, or use randomness. Models are
  emitted in `AddPart` order. NEVER commit a PNG golden: float rounding differs between
  amd64 and arm64 → compare in-process re-renders and computed pixel centroids instead.
- **NEVER add a video encoder.** Output is a PNG sequence; the README states the ffmpeg
  command.
- **NEVER add a public API that contradicts `docs/design.md`.** Extending it is fine; changing
  a decision means changing the doc first.
- **NEVER add a `go.mod` module without recording the decision here.** Approved:
  - `github.com/lestrrat-3d/decad` — bodies and `Body.Tessellate`.
  - `github.com/lestrrat-3d/solidlens` — raster renderer; `render` package ONLY.
  - `github.com/lestrrat-3d/r3` — `Vec`, `Transform`.
  - `github.com/lestrrat-3d/units` — typed quantities.
  - `github.com/lestrrat-3d/sketch` — needed to BUILD decad bodies; **tests and examples
    only**, never imported by production code (`docs/design.md` §4 D10).
  - `github.com/lestrrat-go/option/v3` — functional options (house library).
  - `github.com/stretchr/testify/require` — assertions, **test code only**. NEVER import from
    production code.
- **Tooling lives in its own nested module.** The landing-page clip program (pass 4) is
  `_clips/decad-landing/` with its own `go.mod` and an `_` prefix, modeled on decad's
  `_gallery/` (`docs/design.md` §4 D11). `_clips/demo/` is the demo clip, same shape.
  `_gallery/` is the README's GIF program, same shape: it renders the frames and prints the
  ffmpeg commands that write `docs/images/*.gif`. A clip or gallery program NEVER joins the
  library's module. A new nested module needs a CI `clips` matrix entry and a dependabot
  `gomod` entry.
- **The camera is a rig attachment** (`docs/design.md` §4 D5). An orbit is a revolute node
  about an axis through the target. NEVER add an orbit-camera type beside it.
- **Correctness must be observable.** Every capability ships with a test asserting on a
  computed result (a transform component, a `units.Value`, a pixel centroid, a frame count),
  NEVER merely "it ran".

## Conventions

- Go style, testing and file-layout rules: `~/.claude/docs/go.md`. Tests use
  `testify/require` (never `assert`), external `_test` package, `t.Context()`.
- Test bodies come from `decad/decadtest` (`NewBlock`, `NewPrism`), never from a hand-written
  mesh: the real producer feeds the real consumer.
- User-facing usage → executable Go examples in `examples/` with verified `// Output:`
  blocks. NEVER README-only snippets. NEVER `package main`.
- Docs state **current state only** — no changelogs, no "was X, now Y".
- Every non-test `.go` file in the root and in `render/` carries a row in
  `docs/design.md` §8.
- Prose a human reads (README, `docs/`, doc comments) follows `~/.claude/docs/prose.md`.

## Verification

```
go test ./...      # must pass
go vet ./...       # must pass
golangci-lint run  # v2.12.2, config in .golangci.yml (copied from decad)
```

Local `golangci-lint` version MUST match CI's before a clean local run is trusted.
