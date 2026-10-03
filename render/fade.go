package render

import (
	"cmp"
	"context"
	"image"
	"math"
	"slices"

	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/solidlens"
)

// draw renders p. With no fading part it is one solidlens.Render over every
// model p holds. With n fading parts it renders the layer scenes far to near
// and mixes them, as docs/design.md §9 (pass 3) states:
//
//	R_{n+1} = S_{n+1}
//	R_j     = f_j · S_j + (1 − f_j) · R_{j+1}   where pixel S_j differs from S_{j+1} in any byte
//	R_j     = R_{j+1}                           everywhere else
//	out     = clamp(round(R_1), 0, 255)
//
// S_j holds the opaque parts and the fading parts j … n, with fading part 1
// the nearest to the camera, and S_{n+1} the opaque parts alone. The layers
// are rendered one at a time on the calling goroutine, so a frame holds one
// float64 accumulator and two images whatever n is. ctx is checked before
// each layer, and a done ctx returns ctx.Err() unwrapped.
func (r *Renderer) draw(ctx context.Context, p *framePlan) (*image.RGBA, error) {
	fading := nearToFar(p)
	if len(fading) == 0 {
		return solidlens.Render(ctx, p.layer(func(int) bool { return true }), r.settings())
	}

	// include[k] says whether model k is in the layer being rendered. The
	// first layer, S_{n+1}, holds the opaque models only.
	include := make([]bool, len(p.models))
	for k, m := range p.models {
		include[k] = m.fade == 1
	}
	in := func(k int) bool { return include[k] }

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	prev, err := solidlens.Render(ctx, p.layer(in), r.settings())
	if err != nil {
		return nil, err
	}
	acc := make([]float64, len(prev.Pix))
	for b, v := range prev.Pix {
		acc[b] = float64(v)
	}
	for _, k := range slices.Backward(fading) {
		include[k] = true
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		cur, err := solidlens.Render(ctx, p.layer(in), r.settings())
		if err != nil {
			return nil, err
		}
		mix(acc, cur.Pix, prev.Pix, p.models[k].fade)
		prev = cur
	}

	// prev is S_1 and no longer needed: it takes the result.
	for b, v := range acc {
		prev.Pix[b] = uint8(min(max(math.Floor(v+0.5), 0), 255))
	}
	return prev, nil
}

// mix folds layer cur, at fade f, into acc wherever cur differs from the
// layer behind it in any of a pixel's four bytes. Each product is wrapped in
// an explicit float64 conversion, so the compiler cannot fuse it into an FMA
// and the mix is the same on every architecture for the same bytes.
func mix(acc []float64, cur, behind []uint8, f float64) {
	g := 1 - f
	for p := 0; p < len(cur); p += 4 {
		if cur[p] == behind[p] && cur[p+1] == behind[p+1] && cur[p+2] == behind[p+2] && cur[p+3] == behind[p+3] {
			continue
		}
		for b := p; b < p+4; b++ {
			acc[b] = float64(f*float64(cur[b])) + float64(g*acc[b])
		}
	}
}

// nearToFar returns the indices into p.models of the fading models, nearest
// first. A model's sort key is the view depth of the centre of its posed
// vertices' axis-aligned bounds; equal keys keep Scene.Parts order, which is
// the order p.models is in.
func nearToFar(p *framePlan) []int {
	var fading []int
	for k, m := range p.models {
		if m.fade < 1 {
			fading = append(fading, k)
		}
	}
	if len(fading) == 0 {
		return nil
	}
	cam := p.scene.Camera
	// A zero view direction gives every key 0, so the order is Scene.Parts
	// order; solidlens then refuses the camera when the first layer renders.
	forward, _ := cam.Target.Sub(cam.Position).Normalize()
	depth := make([]float64, len(p.models))
	for _, k := range fading {
		depth[k] = boundsCentre(p.models[k].model.Mesh.Vertices()).Sub(cam.Position).Dot(forward)
	}
	slices.SortStableFunc(fading, func(a, b int) int { return cmp.Compare(depth[a], depth[b]) })
	return fading
}

// boundsCentre returns the centre of the axis-aligned bounds of vertices, and
// the origin for no vertices.
func boundsCentre(vertices []r3.Vec) r3.Vec {
	if len(vertices) == 0 {
		return r3.Vec{}
	}
	lo, hi := vertices[0], vertices[0]
	for _, v := range vertices[1:] {
		lo = r3.Vec{X: min(lo.X, v.X), Y: min(lo.Y, v.Y), Z: min(lo.Z, v.Z)}
		hi = r3.Vec{X: max(hi.X, v.X), Y: max(hi.Y, v.Y), Z: max(hi.Z, v.Z)}
	}
	return lo.Add(hi).Scale(0.5)
}

// layer returns p's scene holding the models for which in returns true, in
// Scene.Parts order. The posed meshes are shared, never copied.
func (p *framePlan) layer(in func(k int) bool) solidlens.Scene {
	scene := p.scene
	scene.Models = make([]solidlens.Model, 0, len(p.models))
	for k, m := range p.models {
		if in(k) {
			scene.Models = append(scene.Models, m.model)
		}
	}
	return scene
}
