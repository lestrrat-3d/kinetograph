package render

import (
	"context"
	"fmt"
	"image"

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
	clip  *kinetograph.Clip
	style Style
	parts []renderPart // in AddPart order
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
// once per distinct body. It reads the parts with Scene.Parts, so it evaluates
// no frame and calls no Builder; a parametric part is built and tessellated by
// the Frame or Sequence call that renders it. It returns ErrStyle for a
// non-positive Width or Height or a Parts name that no part carries,
// kinetograph.ErrKind for a Chord that is not a Length, kinetograph.ErrNilContext
// for a nil ctx, and ctx.Err() when ctx is done. decad's own tolerance errors
// pass through wrapped, with the part's name. clip MUST NOT be nil.
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
	for name := range style.Parts {
		if _, ok := known[name]; !ok {
			return nil, fmt.Errorf("%w: Parts names %q, which no part carries", ErrStyle, name)
		}
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
	return &Renderer{clip: clip, style: style, parts: parts}, nil
}

// scene evaluates frame i through rn and builds its solidlens scene. Errors
// are raw: the callers decide how to wrap them.
func (r *Renderer) scene(ctx context.Context, rn *run, i int) (solidlens.Scene, error) {
	f, err := r.clip.FrameCached(ctx, i, rn.builds)
	if err != nil {
		return solidlens.Scene{}, err
	}
	fov, err := f.Camera.FOV.In(units.Degree)
	if err != nil {
		return solidlens.Scene{}, fmt.Errorf("camera field of view: %w", err)
	}
	models := make([]solidlens.Model, len(r.parts))
	for k, part := range r.parts {
		pose := f.Poses[k]
		mesh := part.mesh
		if mesh == nil {
			mesh, err = rn.tessellate(ctx, pose.Body, r.style.Chord)
			if err != nil {
				return solidlens.Scene{}, fmt.Errorf("tessellating part %q: %w", part.name, err)
			}
		}
		tf := pose.Transform
		posed := &posedMesh{
			vertices:  make([]r3.Vec, len(mesh.vertices)),
			triangles: mesh.triangles,
		}
		for v, p := range mesh.vertices {
			posed.vertices[v] = tf.Apply(p)
		}
		models[k] = solidlens.Model{
			Mesh:         posed,
			Material:     part.appearance.Material,
			BackMaterial: part.appearance.Back,
			Edges:        part.appearance.Edges,
		}
	}
	return solidlens.Scene{
		Camera: solidlens.Camera{
			Position: f.Camera.Position,
			Target:   f.Camera.Target,
			Up:       f.Camera.Up,
			FOV:      fov,
		},
		Models:            models,
		DirectionalLights: r.style.DirectionalLights,
		PointLights:       r.style.PointLights,
		Background:        r.style.Background,
	}, nil
}

func (r *Renderer) settings() solidlens.Settings {
	return solidlens.Settings{Width: r.style.Width, Height: r.style.Height}
}

// Frame renders frame i into a new image. It builds and tessellates each
// parametric part once for this call. It returns kinetograph.ErrNilContext
// for a nil ctx, kinetograph.ErrFrameRange for an i outside the clip, ctx.Err()
// unchanged when ctx is done, and otherwise a *FrameError wrapping the cause.
func (r *Renderer) Frame(ctx context.Context, i int) (*image.RGBA, error) {
	if ctx == nil {
		return nil, kinetograph.ErrNilContext
	}
	if i < 0 || i >= r.clip.FrameCount() {
		return nil, fmt.Errorf("%w: frame %d of %d", kinetograph.ErrFrameRange, i, r.clip.FrameCount())
	}
	scene, err := r.scene(ctx, newRun(), i)
	if err != nil {
		return nil, r.frameError(ctx, i, err)
	}
	img, err := solidlens.Render(ctx, scene, r.settings())
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
