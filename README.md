# kinetograph

kinetograph animates solids built with [decad](https://github.com/lestrrat-3d/decad), a headless CAD engine in Go,
and renders every frame with [solidlens](https://github.com/lestrrat-3d/solidlens) to a numbered PNG file. It
writes no video; a tool such as ffmpeg assembles the PNGs.

## What it does

- **Channels.** An animated value is a `Channel` of `units.Value` keyframes of one kind (an angle, a length), with
  easing between them. Time is `time.Duration`; a frame rate is an `int` count of frames per second.
- **Rig.** Joints form a tree. A `Revolute` joint turns about an axis by an angle channel, a `Prismatic` joint
  slides along a direction by a length channel, and a `Fixed` joint holds a constant transform. A node's world
  transform is its own composed with its parent's.
- **Parts and camera.** decad bodies and one perspective camera attach to nodes. A camera orbit is a revolute
  node whose axis passes through the target; a dolly is a prismatic node.
- **Lights.** A point or directional light attaches to a node like the camera, so a light that circles a part is
  a light on a revolute node. `render.Style.Lights` gives each light, by name, a color and an intensity channel.
- **Fades.** `render.Appearance.Fade` is a part's opacity channel, from 0 to 1. A part at 0 is left out of the
  frame. A part between 0 and 1 is mixed over what is behind it: render draws the frame in layers, with and
  without each fading part, and mixes them from the farthest fading part to the nearest.
- **Reshape.** `Scene.AddParametric` attaches a part whose body a `Builder` builds from the values of named
  channels at each frame time, such as a block whose width is a length channel. The builder is called once per
  distinct set of values, so a held value costs no rebuild, and several render workers never build one set twice.
- **Clip and render.** A `Clip` samples a scene at a frame rate. `render.Renderer` tessellates each body once,
  moves the vertices by the node's world transform for each frame time, and writes `frame_000000.png`,
  `frame_000001.png`, and so on. A rebuilt body is tessellated once in the render call that needs it.

A failing frame stops the sequence with a `*render.FrameError` that names the frame index and time. The same
inputs give byte-identical PNGs on one Go toolchain and architecture; the rules are in
[docs/design.md](docs/design.md) section 7.

## Usage

[`examples/kinetograph_sequence_example_test.go`](examples/kinetograph_sequence_example_test.go) is the runnable
usage: a decad block turns on one revolute joint while the camera orbits on another, and the clip is rendered to
PNG files. [`examples/kinetograph_reshape_example_test.go`](examples/kinetograph_reshape_example_test.go) renders a
slab whose width follows a channel and prints each width the builder is asked for.
[`examples/kinetograph_appearance_example_test.go`](examples/kinetograph_appearance_example_test.go) fades a block
in while a point light circles it, and prints the light's position at each frame. `go test ./examples/` runs all
three and checks their output.

[`_clips/demo/`](_clips/demo/main.go) renders a 4.5 s demo clip: two pins drop into a drilled plate, and a ring
rises off it and turns half a turn while the camera orbits. `cd _clips/demo && go run . -out out` writes 108 frames
at 960x720 into `out/`.

[`_clips/decad-landing/`](_clips/decad-landing/main.go) renders decad's 24 s landing-page clip in three shots: a
flange plate built feature by feature, a shelf of six decad parts, and the DECAD wordmark.
`cd _clips/decad-landing && go run . > assemble.sh && sh assemble.sh` writes 750 frames at 1280x720 into `out/`,
then `out/decad-landing.mp4` and `out/decad-landing.gif`.

Assemble the frames into a video with ffmpeg:

```
ffmpeg -framerate 24 -i frame_%06d.png -c:v libx264 -pix_fmt yuv420p clip.mp4
```

The `yuv420p` pixel format needs an even image width and height. kinetograph does not check this, because it is a
property of that codec.

## Design

[docs/design.md](docs/design.md) is the contract: the decisions, the public API, the error behaviour, the
determinism rules and the plan for later passes.

## License

See [LICENSE](LICENSE).
