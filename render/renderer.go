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
)

// tessellation is one body's mesh, held as plain slices so a frame applies a
// pose without touching decad again.
type tessellation struct {
	vertices  []r3.Vec
	triangles [][3]int
}

// renderPart is one part's mesh and resolved appearance.
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

// Renderer holds a clip, its style and every part's tessellation. It is
// immutable after New and safe to use from several goroutines.
type Renderer struct {
	clip  *kinetograph.Clip
	style Style
	parts []renderPart // in AddPart order
}

// New tessellates every part of clip's scene at style.Chord, once per
// distinct body, with decad.VerifyNone: the mesh is drawn, never proven. It
// returns ErrStyle for a non-positive Width or Height or a Parts name that no
// part carries, kinetograph.ErrKind for a Chord that is not a Length, and
// kinetograph.ErrNilContext for a nil ctx. decad's own tolerance errors pass
// through wrapped, with the part's name. clip MUST NOT be nil.
func New(ctx context.Context, clip *kinetograph.Clip, style Style) (*Renderer, error) {
	if ctx == nil {
		return nil, kinetograph.ErrNilContext
	}
	if err := style.validate(); err != nil {
		return nil, err
	}
	// Frame 0 names every part and carries its body; the parts do not change
	// between frames.
	first, err := clip.Frame(ctx, 0)
	if err != nil {
		return nil, err
	}
	known := make(map[string]struct{}, len(first.Poses))
	for _, p := range first.Poses {
		known[p.Name] = struct{}{}
	}
	for name := range style.Parts {
		if _, ok := known[name]; !ok {
			return nil, fmt.Errorf("%w: Parts names %q, which no part carries", ErrStyle, name)
		}
	}

	cache := map[*decad.Body]*tessellation{}
	parts := make([]renderPart, len(first.Poses))
	for i, p := range first.Poses {
		mesh, ok := cache[p.Body]
		if !ok {
			m, err := p.Body.Tessellate(ctx, style.Chord, decad.WithVerification(decad.VerifyNone))
			if err != nil {
				return nil, fmt.Errorf("render: tessellating part %q: %w", p.Name, err)
			}
			mesh = &tessellation{vertices: m.Vertices(), triangles: m.Triangles()}
			cache[p.Body] = mesh
		}
		appearance, ok := style.Parts[p.Name]
		if !ok {
			appearance = style.Default
		}
		parts[i] = renderPart{name: p.Name, mesh: mesh, appearance: appearance}
	}
	return &Renderer{clip: clip, style: style, parts: parts}, nil
}

// scene evaluates frame i and builds its solidlens scene. Errors are raw: the
// callers decide how to wrap them.
func (r *Renderer) scene(ctx context.Context, i int) (solidlens.Scene, error) {
	f, err := r.clip.Frame(ctx, i)
	if err != nil {
		return solidlens.Scene{}, err
	}
	fov, err := f.Camera.FOV.In(units.Degree)
	if err != nil {
		return solidlens.Scene{}, fmt.Errorf("camera field of view: %w", err)
	}
	models := make([]solidlens.Model, len(r.parts))
	for k, part := range r.parts {
		tf := f.Poses[k].Transform
		posed := &posedMesh{
			vertices:  make([]r3.Vec, len(part.mesh.vertices)),
			triangles: part.mesh.triangles,
		}
		for v, p := range part.mesh.vertices {
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

// Frame renders frame i into a new image. It returns kinetograph.ErrNilContext
// for a nil ctx, kinetograph.ErrFrameRange for an i outside the clip, ctx.Err()
// unchanged when ctx is done, and otherwise a *FrameError wrapping the cause.
func (r *Renderer) Frame(ctx context.Context, i int) (*image.RGBA, error) {
	if ctx == nil {
		return nil, kinetograph.ErrNilContext
	}
	if i < 0 || i >= r.clip.FrameCount() {
		return nil, fmt.Errorf("%w: frame %d of %d", kinetograph.ErrFrameRange, i, r.clip.FrameCount())
	}
	scene, err := r.scene(ctx, i)
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
