package render

import (
	"context"
	"fmt"
	"image"
	"maps"
	"slices"
	"time"

	"github.com/lestrrat-3d/decad"
	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/solidlens"
	"github.com/lestrrat-3d/units"

	"github.com/lestrrat-3d/kinetograph"
	"github.com/lestrrat-3d/kinetograph/internal/memo"
)

// tessellation is one body's mesh, held as plain slices so a frame applies a
// pose without touching decad again.
type tessellation struct {
	vertices  []r3.Vec
	triangles [][3]int
}

// renderPart is one part's mesh and resolved appearance. mesh is nil for a
// parametric part, whose mesh comes from the call's run.
type renderPart struct {
	name       string
	mesh       *tessellation
	appearance Appearance
}

// renderLight is one node light's appearance, in Scene.Lights order.
type renderLight struct {
	name       string
	appearance LightAppearance
}

// posedMesh is a solidlens.TriangleSource: the part's vertices under one
// frame's transform and the part's unchanged triangle indices.
type posedMesh struct {
	vertices  []r3.Vec
	triangles [][3]int
}

func (m *posedMesh) Vertices() []r3.Vec  { return m.vertices }
func (m *posedMesh) Triangles() [][3]int { return m.triangles }

// Renderer holds a clip, its style and the tessellation of every part AddPart
// attached. It is immutable after New and safe to use from several goroutines.
// It holds no cache of rebuilt bodies: each Frame or Sequence call makes its
// own run.
type Renderer struct {
	clip   *kinetograph.Clip
	style  Style
	parts  []renderPart  // in Scene.Parts order
	lights []renderLight // in Scene.Lights order
}

// run is the per-call state of one Frame or Sequence call: the bodies
// parametric parts were built into and their meshes. Its workers share it.
type run struct {
	builds *kinetograph.BuildCache
	meshes memo.Map[*decad.Body, *tessellation]
}

func newRun() *run {
	return &run{builds: kinetograph.NewBuildCache()}
}

// tessellate returns body's mesh at chord, tessellating it once per run.
func (rn *run) tessellate(ctx context.Context, body *decad.Body, chord units.Value) (*tessellation, error) {
	return rn.meshes.Get(ctx, body, func(ctx context.Context) (*tessellation, error) {
		return tessellate(ctx, body, chord)
	})
}

// tessellate draws body at chord with decad.VerifyNone: the mesh is drawn,
// never proven.
func tessellate(ctx context.Context, body *decad.Body, chord units.Value) (*tessellation, error) {
	m, err := body.Tessellate(ctx, chord, decad.WithVerification(decad.VerifyNone))
	if err != nil {
		return nil, err
	}
	return &tessellation{vertices: m.Vertices(), triangles: m.Triangles()}, nil
}

// New tessellates every part AddPart attached to clip's scene at style.Chord,
// once per distinct body. It reads the parts with Scene.Parts and the lights
// with Scene.Lights, so it evaluates no frame and calls no Builder; a
// parametric part is built and tessellated by the Frame or Sequence call that
// renders it.
//
// It returns kinetograph.ErrNilContext for a nil ctx, ctx.Err() when ctx is
// done, ErrStyle for a non-positive Width or Height or a Parts name that no
// part carries, and kinetograph.ErrKind for a Chord that is not a Length. It
// then checks the lights and fades, in this order, and reports the first
// failure:
//
//  1. Every scene light needs a Style.Lights entry and every Style.Lights name
//     a light, or ErrStyle naming the light. Names are checked in sorted
//     order.
//  2. Each LightAppearance, in sorted light-name order: the zero Color is
//     ErrStyle; a nil Intensity is kinetograph.ErrNilChannel, a kind other
//     than units.Dimensionless is kinetograph.ErrKind, and a keyframe below 0
//     is ErrStyle.
//  3. Default.Fade, then each Parts fade in sorted name order: a kind other
//     than units.Dimensionless is kinetograph.ErrKind, and a keyframe outside
//     [0, 1] is ErrStyle naming the part, or "Default".
//
// decad's own tolerance errors pass through wrapped, with the part's name.
// clip MUST NOT be nil.
func New(ctx context.Context, clip *kinetograph.Clip, style Style) (*Renderer, error) {
	if ctx == nil {
		return nil, kinetograph.ErrNilContext
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	if err := style.validate(); err != nil {
		return nil, err
	}
	infos := clip.Scene().Parts()
	known := make(map[string]struct{}, len(infos))
	for _, p := range infos {
		known[p.Name] = struct{}{}
	}
	for _, name := range slices.Sorted(maps.Keys(style.Parts)) {
		if _, ok := known[name]; !ok {
			return nil, fmt.Errorf("%w: Parts names %q, which no part carries", ErrStyle, name)
		}
	}
	sceneLights := clip.Scene().Lights()
	if err := style.validateLights(sceneLights); err != nil {
		return nil, err
	}
	if err := style.validateFades(); err != nil {
		return nil, err
	}

	cache := map[*decad.Body]*tessellation{}
	parts := make([]renderPart, len(infos))
	for i, p := range infos {
		appearance, ok := style.Parts[p.Name]
		if !ok {
			appearance = style.Default
		}
		parts[i] = renderPart{name: p.Name, appearance: appearance}
		if p.Parametric {
			continue
		}
		mesh, ok := cache[p.Body]
		if !ok {
			m, err := tessellate(ctx, p.Body, style.Chord)
			if err != nil {
				return nil, fmt.Errorf("render: tessellating part %q: %w", p.Name, err)
			}
			mesh = m
			cache[p.Body] = mesh
		}
		parts[i].mesh = mesh
	}
	lights := make([]renderLight, len(sceneLights))
	for i, l := range sceneLights {
		lights[i] = renderLight{name: l.Name, appearance: style.Lights[l.Name]}
	}
	return &Renderer{clip: clip, style: style, parts: parts, lights: lights}, nil
}

// frameModel is one part that a frame draws: its posed model and its fade,
// in (0, 1].
type frameModel struct {
	model solidlens.Model
	fade  float64
}

// framePlan is one evaluated frame, ready to draw: the solidlens scene
// without its models, and the parts that are not hidden, in Scene.Parts
// order.
type framePlan struct {
	scene  solidlens.Scene
	models []frameModel
}

// plan evaluates frame i through rn: it poses the parts, evaluates every
// fade and light intensity at the frame's time, and leaves out the parts at
// fade 0. Every part is still built and tessellated, whatever its fade.
// Errors are raw: the callers decide how to wrap them.
func (r *Renderer) plan(ctx context.Context, rn *run, i int) (*framePlan, error) {
	f, err := r.clip.FrameCached(ctx, i, rn.builds)
	if err != nil {
		return nil, err
	}
	fov, err := f.Camera.FOV.In(units.Degree)
	if err != nil {
		return nil, fmt.Errorf("camera field of view: %w", err)
	}
	models := make([]frameModel, 0, len(r.parts))
	for k, part := range r.parts {
		pose := f.Poses[k]
		mesh := part.mesh
		if mesh == nil {
			mesh, err = rn.tessellate(ctx, pose.Body, r.style.Chord)
			if err != nil {
				return nil, fmt.Errorf("tessellating part %q: %w", part.name, err)
			}
		}
		fade := 1.0
		if part.appearance.Fade != nil {
			fade, err = evaluate(part.appearance.Fade, f.Time)
			if err != nil {
				return nil, fmt.Errorf("part %q fade: %w", part.name, err)
			}
			fade = min(max(fade, 0), 1)
		}
		if fade == 0 {
			continue
		}
		tf := pose.Transform
		posed := &posedMesh{
			vertices:  make([]r3.Vec, len(mesh.vertices)),
			triangles: mesh.triangles,
		}
		for v, p := range mesh.vertices {
			posed.vertices[v] = tf.Apply(p)
		}
		models = append(models, frameModel{
			model: solidlens.Model{
				Mesh:         posed,
				Material:     part.appearance.Material,
				BackMaterial: part.appearance.Back,
				Edges:        part.appearance.Edges,
			},
			fade: fade,
		})
	}

	// New slices on every frame: workers share the Style's.
	directional := slices.Clone(r.style.DirectionalLights)
	points := slices.Clone(r.style.PointLights)
	for k, light := range r.lights {
		pose := f.Lights[k]
		intensity, err := evaluate(light.appearance.Intensity, f.Time)
		if err != nil {
			return nil, fmt.Errorf("light %q intensity: %w", light.name, err)
		}
		intensity = max(intensity, 0)
		color := light.appearance.Color
		if pose.Kind == kinetograph.PointLight {
			points = append(points, solidlens.PointLight{Position: pose.Position, Color: color, Intensity: intensity})
			continue
		}
		directional = append(directional, solidlens.DirectionalLight{Direction: pose.Direction, Color: color, Intensity: intensity})
	}

	return &framePlan{
		scene: solidlens.Scene{
			Camera: solidlens.Camera{
				Position: f.Camera.Position,
				Target:   f.Camera.Target,
				Up:       f.Camera.Up,
				FOV:      fov,
			},
			DirectionalLights: directional,
			PointLights:       points,
			Background:        r.style.Background,
		},
		models: models,
	}, nil
}

// evaluate returns c at t as a bare number, read with Value.In(units.One).
// New checked that c is Dimensionless.
func evaluate(c *kinetograph.Channel, t time.Duration) (float64, error) {
	v, err := c.At(t)
	if err != nil {
		return 0, err
	}
	return v.In(units.One)
}

func (r *Renderer) settings() solidlens.Settings {
	return solidlens.Settings{Width: r.style.Width, Height: r.style.Height}
}

// frameImage evaluates and draws frame i through rn. Errors are raw.
func (r *Renderer) frameImage(ctx context.Context, rn *run, i int) (*image.RGBA, error) {
	p, err := r.plan(ctx, rn, i)
	if err != nil {
		return nil, err
	}
	return r.draw(ctx, p)
}

// Frame renders frame i into a new image. It builds and tessellates each
// parametric part once for this call.
//
// A part whose fade is 0 at the frame is left out. With no part strictly
// between 0 and 1, the image is one solidlens.Render of the remaining parts.
// With n fading parts, Frame renders n + 1 layers, one at a time, and mixes
// them from the farthest fading part to the nearest, as docs/design.md §9
// (pass 3) states: a fading part j at fade f_j turns each pixel it changes
// into f_j · (the layer with it) + (1 − f_j) · (the mix behind it).
//
// It returns kinetograph.ErrNilContext for a nil ctx, kinetograph.ErrFrameRange
// for an i outside the clip, ctx.Err() unchanged when ctx is done, and
// otherwise a *FrameError wrapping the cause.
func (r *Renderer) Frame(ctx context.Context, i int) (*image.RGBA, error) {
	if ctx == nil {
		return nil, kinetograph.ErrNilContext
	}
	if i < 0 || i >= r.clip.FrameCount() {
		return nil, fmt.Errorf("%w: frame %d of %d", kinetograph.ErrFrameRange, i, r.clip.FrameCount())
	}
	img, err := r.frameImage(ctx, newRun(), i)
	if err != nil {
		return nil, r.frameError(ctx, i, err)
	}
	return img, nil
}

// frameError wraps err as the failure of frame i, except that a cancelled ctx
// is returned as ctx.Err() unwrapped.
func (r *Renderer) frameError(ctx context.Context, i int, err error) error {
	if cerr := ctx.Err(); cerr != nil {
		return cerr
	}
	return &FrameError{Index: i, Time: r.clip.FrameTime(i), Err: err}
}
