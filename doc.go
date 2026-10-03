// Package kinetograph animates solids built with decad and prepares every
// frame of the animation for rendering. It writes no pixels: the render
// subpackage turns each Frame into a solidlens scene and a numbered PNG, and a
// video tool such as ffmpeg assembles the PNGs.
//
// # Layering
//
// The root package imports decad, r3 and units and names no color, light or
// pixel. The render subpackage imports kinetograph and solidlens, and is the
// only package that imports solidlens. decad, solidlens, r3 and units never
// import kinetograph.
//
// # Model
//
// A Channel is a scalar function of time built from keyframes of one
// units.Kind with easing between them (D1, D3, D4). A Rig is a tree of Node
// joints: Fixed, Revolute (an axis through a center and an Angle channel) and
// Prismatic (a direction and a Length channel). A node's world transform is
// its own local transform composed with its parent's (D1). Parts (decad
// bodies) and one Camera attach to nodes of the same rig (D5), so a camera
// orbit is a revolute node and a dolly is a prismatic one. A Scene collects
// them; a Clip samples a Scene at a frame rate; Clip.Frame returns a Frame of
// poses as r3.Transforms.
//
// A rigid pose is applied to the tessellated vertices by the renderer, never
// to the decad body (D2). Time is time.Duration and a frame rate is an int
// number of frames per second (D3). Frame times are exact integer nanoseconds
// and the same inputs give the same frames (D9).
//
// The design is docs/design.md in the repository.
package kinetograph
