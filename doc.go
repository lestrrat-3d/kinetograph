// Package kinetograph animates solids built with decad and prepares every
// frame of the animation for rendering. It writes no pixels: the render
// subpackage turns each Frame into a solidlens scene and a numbered PNG, and a
// video tool such as ffmpeg assembles the PNGs.
//
// # Layering
//
// The root package imports decad, r3 and units and names no color or pixel. It
// names a light only as a kind and a pose: where the light is and which way it
// shines. The render subpackage imports kinetograph and solidlens, and is the
// only package that imports solidlens. decad, solidlens, r3 and units never
// import kinetograph.
//
// # Model
//
// A Channel is a scalar function of time built from keyframes of one
// units.Kind with easing between them (D1, D3, D4). A Rig is a tree of Node
// joints: Fixed, Revolute (an axis through a center and an Angle channel),
// Prismatic (a direction and a Length channel) and Driven (a TransformTrack
// the caller supplies). A node's world transform is its own local transform
// composed with its parent's (D1). Parts (decad bodies) and one Camera attach
// to nodes of the same rig (D5), so a camera orbit is a revolute node and a
// dolly is a prismatic one. A Scene collects them; a Clip samples a Scene at a
// frame rate; Clip.Frame returns a Frame of poses as r3.Transforms.
//
// A driven node's local transform at t is the transform its track returns for
// t (D12). kinetograph asks the track at every time it evaluates the node,
// never interpolates between two results and keeps none. The track must return
// the same transform for the same t and be safe to call from several
// goroutines at once. A track error, or a result that is a reflection or not a
// rigid motion, fails the frame at t.
//
// # Linkages
//
// A LinkageTrack films one link of a decad.Linkage moving along a
// decad.Drive (D13). Its At reads the drive fraction s from a Dimensionless
// channel and returns the link's pose from Linkage.PoseAt, the call
// Document.VerifyLinkage builds every pose it checks with. Scene.AddLinkage
// adds one driven node per link under the rig's root and attaches each
// link's bodies to its node.
//
// A drive that moves a closed loop is filmed through a decad.Schedule, built
// once with Linkage.Schedule. A ScheduleTrack returns the link's pose from
// Schedule.PoseAt, and Scene.AddSchedule builds the nodes and parts as
// Scene.AddLinkage does.
//
// # Lights and fades
//
// Scene.AddLight attaches a PointLight or a DirectionalLight to a node, as
// the camera is attached (D5), and Frame.Lights holds each light's world
// position or unit direction at the frame time. The light's color and its
// intensity channel are render.Style's, bound by the light's name, and so is
// each part's fade: a Dimensionless channel of opacity in [0, 1]. render
// leaves a part at fade 0 out of the frame and mixes a part between 0 and 1
// over what is behind it by rendering the frame in layers.
//
// # Reshape
//
// Scene.AddParametric attaches a part whose body a Builder builds from
// Params: the values of named channels at the frame time. A BuildCache calls
// Build once per distinct part and parameter tuple, keyed by each value's
// units.Value.MarshalText in parameter-name order. Scene.At and Clip.Frame
// use a new cache on every call; Scene.AtCached and Clip.FrameCached take
// one, and render makes one per Frame or Sequence call.
//
// A rigid pose is applied to the tessellated vertices by the renderer, never
// to the decad body (D2). Time is time.Duration and a frame rate is an int
// number of frames per second (D3). Frame times are exact integer nanoseconds
// and the same inputs give the same frames (D9).
//
// The design is docs/design.md in the repository.
package kinetograph
