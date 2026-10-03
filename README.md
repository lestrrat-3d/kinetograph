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
- **Clip and render.** A `Clip` samples a scene at a frame rate. `render.Renderer` tessellates each body once,
  moves the vertices by the node's world transform for each frame time, and writes `frame_000000.png`,
  `frame_000001.png`, and so on.

A failing frame stops the sequence with a `*render.FrameError` that names the frame index and time. The same
inputs give byte-identical PNGs on one Go toolchain and architecture; the rules are in
[docs/design.md](docs/design.md) section 7.

## Usage

[`examples/kinetograph_sequence_example_test.go`](examples/kinetograph_sequence_example_test.go) is the runnable
usage: a decad block turns on one revolute joint while the camera orbits on another, and the clip is rendered to
PNG files. `go test ./examples/` runs it and checks its output.

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
