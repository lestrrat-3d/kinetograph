package kinetograph_test

import (
	"math"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/lestrrat-3d/decad"
	"github.com/lestrrat-3d/decad/decadtest"
	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/units"
	"github.com/stretchr/testify/require"

	"github.com/lestrrat-3d/kinetograph"
)

// linkageFrame is one frame at 64 fps, exactly 15625000 ns.
const linkageFrame = time.Second / 64

// arm is a two-link arm. The upper arm, x ∈ [0, 40], turns about Z through
// the origin; the forearm, x ∈ [40, 80], with a pin beside it, turns about Z
// through the elbow (40, 0, 0). Its drive turns the shoulder from 0° to 90°
// and the elbow from 0° to −90°, so the forearm keeps its orientation.
type arm struct {
	linkage  *decad.Linkage
	shoulder *decad.Link
	elbow    *decad.Link
	upper    *decad.Body
	forearm  *decad.Body
	pin      *decad.Body
}

func newArm(t *testing.T) arm {
	t.Helper()
	doc := decad.New()
	a := arm{
		linkage: decad.NewLinkage(),
		upper:   decadtest.NewBlock(t, doc, 0, -7, 40, 7, units.Millimeters(10)),
		forearm: decadtest.NewBlock(t, doc, 40, -7, 80, 7, units.Millimeters(10)),
		pin:     decadtest.NewBlock(t, doc, 76, 8, 80, 12, units.Millimeters(10)),
	}
	var err error
	a.shoulder, err = a.linkage.Ground().Revolute(r3.Vec{}, r3.Vec{Z: 1}, []*decad.Body{a.upper})
	require.NoError(t, err)
	a.elbow, err = a.shoulder.Revolute(r3.Vec{X: 40}, r3.Vec{Z: 1}, []*decad.Body{a.forearm, a.pin})
	require.NoError(t, err)
	return a
}

// drive returns a new copy of the arm's drive.
func (a arm) drive() decad.Drive {
	return decad.Drive{
		{Link: a.shoulder, From: units.Degrees(0), To: units.Degrees(90)},
		{Link: a.elbow, From: units.Degrees(0), To: units.Degrees(-90)},
	}
}

// viaDrive returns a new copy of a drive that passes the shoulder through 60°
// and the elbow through −30° at s = 1/2.
func (a arm) viaDrive() decad.Drive {
	return decad.Drive{
		{Link: a.shoulder, From: units.Degrees(0), To: units.Degrees(90), Via: []units.Value{units.Degrees(60)}},
		{Link: a.elbow, From: units.Degrees(0), To: units.Degrees(-90), Via: []units.Value{units.Degrees(-30)}},
	}
}

// fourSecondFraction runs a drive once from 0 at time 0 to 1 at 4 s: 256
// frames at 64 fps.
func fourSecondFraction(t *testing.T) *kinetograph.Channel {
	t.Helper()
	return mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Scalar(0)},
		kinetograph.Keyframe{At: 256 * linkageFrame, Value: units.Scalar(1)},
	)
}

// transformBits are a transform's twelve components as raw float64 bits, so
// two transforms compare equal only when they are bit-identical.
func transformBits(tr r3.Transform) [12]uint64 {
	b := tr.Basis()
	tv := tr.Translation()
	var out [12]uint64
	for i, v := range []r3.Vec{b.EX, b.EY, b.EZ, tv} {
		out[3*i] = math.Float64bits(v.X)
		out[3*i+1] = math.Float64bits(v.Y)
		out[3*i+2] = math.Float64bits(v.Z)
	}
	return out
}

// poseAt is PoseAt's pose of the link at index at s.
func poseAt(t *testing.T, l *decad.Linkage, d decad.Drive, s units.Value, index int) r3.Transform {
	t.Helper()
	pose, err := l.PoseAt(d, s)
	require.NoError(t, err)
	return pose.Poses[index]
}

func TestLinkageTrackMatchesPoseAt(t *testing.T) {
	a := newArm(t)
	fraction := fourSecondFraction(t)
	for index, link := range a.linkage.Links() {
		track, err := kinetograph.NewLinkageTrack(a.linkage, a.drive(), link, fraction)
		require.NoError(t, err)
		for _, c := range []struct {
			frame int
			s     float64
		}{
			{0, 0},
			{1, 1.0 / 256},
			{85, 85.0 / 256},
			{86, 86.0 / 256},
			{171, 171.0 / 256},
			{256, 1},
			{300, 1}, // held after the last keyframe
			{-1, 0},  // held before the first
		} {
			at := time.Duration(c.frame) * linkageFrame
			s, err := fraction.At(at)
			require.NoError(t, err)
			require.Equal(t, c.s, s.Mag(), "frame %d", c.frame)
			require.Equal(t, units.Scalar(c.s).Unit(), s.Unit())

			got, err := track.At(at)
			require.NoError(t, err)
			want := poseAt(t, a.linkage, a.drive(), units.Scalar(c.s), index)
			require.Equal(t, transformBits(want), transformBits(got), "link %d frame %d", index, c.frame)
		}
	}

	// At s = 1/4 the shoulder has turned θ = 22.5° and the forearm keeps its
	// orientation, so the tip (80, 0, 0) sits at (40·cos θ + 40, 40·sin θ, 0).
	elbow, err := kinetograph.NewLinkageTrack(a.linkage, a.drive(), a.elbow, fraction)
	require.NoError(t, err)
	pose, err := elbow.At(64 * linkageFrame)
	require.NoError(t, err)
	theta := math.Pi / 8
	tip := pose.Apply(r3.Vec{X: 80})
	require.True(t, tip.Equal(r3.Vec{X: 40*math.Cos(theta) + 40, Y: 40 * math.Sin(theta)}, 1e-9), "tip %v", tip)
}

func TestLinkageTrackPassesFractionAndWaypointsThrough(t *testing.T) {
	a := newArm(t)
	eased := mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Scalar(0)},
		kinetograph.Keyframe{At: 4 * time.Second, Value: units.Scalar(1), Ease: kinetograph.SmoothStep},
	)
	for name, drive := range map[string]func() decad.Drive{"plain": a.drive, "via": a.viaDrive} {
		t.Run(name, func(t *testing.T) {
			track, err := kinetograph.NewLinkageTrack(a.linkage, drive(), a.elbow, eased)
			require.NoError(t, err)
			for i := range 9 {
				at := time.Duration(i) * 500 * time.Millisecond
				s, err := eased.At(at)
				require.NoError(t, err)
				got, err := track.At(at)
				require.NoError(t, err)
				require.Equal(t, transformBits(poseAt(t, a.linkage, drive(), s, 1)), transformBits(got), "at %s", at)
			}
		})
	}

	// Halfway through a linear fraction the via drive is at its waypoint: the
	// shoulder at 60° and the forearm turned 60° − 30° = 30° in the world, so
	// the tip is the elbow (40·cos 60°, 40·sin 60°) plus 40 mm along 30°.
	track, err := kinetograph.NewLinkageTrack(a.linkage, a.viaDrive(), a.elbow, fourSecondFraction(t))
	require.NoError(t, err)
	pose, err := track.At(2 * time.Second)
	require.NoError(t, err)
	tip := pose.Apply(r3.Vec{X: 80})
	want := r3.Vec{
		X: 40*math.Cos(math.Pi/3) + 40*math.Cos(math.Pi/6),
		Y: 40*math.Sin(math.Pi/3) + 40*math.Sin(math.Pi/6),
	}
	require.True(t, tip.Equal(want, 1e-9), "tip %v, want %v", tip, want)
}

func TestNewLinkageTrackRefusals(t *testing.T) {
	a := newArm(t)
	other := decad.NewLinkage()
	stranger, err := other.Ground().Revolute(r3.Vec{}, r3.Vec{Z: 1},
		[]*decad.Body{decadtest.NewBlock(t, decad.New(), 0, 0, 1, 1, units.Millimeters(1))})
	require.NoError(t, err)
	lengthDrive := decad.Drive{{Link: a.shoulder, From: units.Millimeters(0), To: units.Millimeters(1)}}
	fraction := fourSecondFraction(t)

	for _, c := range []struct {
		name     string
		linkage  *decad.Linkage
		drive    decad.Drive
		link     *decad.Link
		fraction *kinetograph.Channel
		want     error
	}{
		{"nil linkage", nil, a.drive(), a.shoulder, fraction, kinetograph.ErrNilLinkage},
		{"nil link", a.linkage, a.drive(), nil, fraction, kinetograph.ErrForeignLink},
		{"ground", a.linkage, a.drive(), a.linkage.Ground(), fraction, kinetograph.ErrForeignLink},
		{"other linkage", a.linkage, a.drive(), stranger, fraction, kinetograph.ErrForeignLink},
		{"nil fraction", a.linkage, a.drive(), a.shoulder, nil, kinetograph.ErrNilChannel},
		{"angle fraction", a.linkage, a.drive(), a.shoulder, kinetograph.Constant(units.Degrees(0)), kinetograph.ErrKind},
		{"length sweep on a revolute", a.linkage, lengthDrive, a.shoulder, fraction, decad.ErrUnitKind},
	} {
		t.Run(c.name, func(t *testing.T) {
			track, err := kinetograph.NewLinkageTrack(c.linkage, c.drive, c.link, c.fraction)
			require.ErrorIs(t, err, c.want)
			require.Nil(t, track)
		})
	}
}

func TestLinkageTrackOwnsItsDrive(t *testing.T) {
	a := newArm(t)
	drive := a.viaDrive()
	fraction := fourSecondFraction(t)
	track, err := kinetograph.NewLinkageTrack(a.linkage, drive, a.elbow, fraction)
	require.NoError(t, err)
	drive[0].To = units.Degrees(10)
	drive[1].Via[0] = units.Degrees(-80)

	for _, frame := range []int{0, 64, 128, 200, 256} {
		at := time.Duration(frame) * linkageFrame
		s, err := fraction.At(at)
		require.NoError(t, err)
		got, err := track.At(at)
		require.NoError(t, err)
		require.Equal(t, transformBits(poseAt(t, a.linkage, a.viaDrive(), s, 1)), transformBits(got), "frame %d", frame)
	}
}

func TestLinkageTrackReportsFractionFailure(t *testing.T) {
	a := newArm(t)
	// At 500 ms the fraction is 0 + MaxFloat64 · 2, which overflows.
	fraction := mustChannel(t,
		kinetograph.Keyframe{At: 0, Value: units.Scalar(0)},
		kinetograph.Keyframe{At: time.Second, Value: units.Scalar(math.MaxFloat64), Ease: doubling{}},
	)
	track, err := kinetograph.NewLinkageTrack(a.linkage, a.drive(), a.elbow, fraction)
	require.NoError(t, err)
	_, err = track.At(500 * time.Millisecond)
	require.ErrorIs(t, err, units.ErrNotFinite)
	require.ErrorContains(t, err, "kinetograph: linkage fraction")
}

func TestLinkageTrackIsSafeConcurrently(t *testing.T) {
	a := newArm(t)
	fraction := fourSecondFraction(t)
	track, err := kinetograph.NewLinkageTrack(a.linkage, a.drive(), a.elbow, fraction)
	require.NoError(t, err)

	const workers, times = 8, 16
	results := make([][times][12]uint64, workers)
	failures := make([]error, workers)
	var wg sync.WaitGroup
	for w := range workers {
		wg.Go(func() {
			for i := range times {
				pose, err := track.At(time.Duration(16*i) * linkageFrame)
				if err != nil {
					failures[w] = err
					return
				}
				results[w][i] = transformBits(pose)
			}
		})
	}
	wg.Wait()

	for w := range workers {
		require.NoError(t, failures[w])
		for i := range times {
			s, err := fraction.At(time.Duration(16*i) * linkageFrame)
			require.NoError(t, err)
			require.Equal(t, transformBits(poseAt(t, a.linkage, a.drive(), s, 1)), results[w][i], "worker %d time %d", w, i)
		}
	}
}

// armNames names every body of the arm and the wall.
func armNames(a arm, wall *decad.Body) map[*decad.Body]string {
	return map[*decad.Body]string{a.upper: "upper", a.forearm: "forearm", a.pin: "pin", wall: "wall"}
}

func TestAddLinkagePosesEveryLinkBody(t *testing.T) {
	a := newArm(t)
	wall := decadtest.NewBlock(t, decad.New(), -50, 60, 100, 70, units.Millimeters(20))
	rig := kinetograph.NewRig()
	scene := kinetograph.NewScene(rig)
	fraction := fourSecondFraction(t)
	nodes, err := scene.AddLinkage(a.linkage, a.drive(), fraction, armNames(a, wall))
	require.NoError(t, err)
	require.Len(t, nodes, 2)
	require.NoError(t, scene.AddPart("wall", rig.Root(), wall))
	require.NoError(t, scene.SetCamera(rig.Root(), defaultCamera()))

	parts := scene.Parts()
	names := make([]string, len(parts))
	for i, p := range parts {
		names[i] = p.Name
	}
	require.Equal(t, []string{"upper", "forearm", "pin", "wall"}, names)
	require.Same(t, a.upper, parts[0].Body)
	require.Same(t, a.forearm, parts[1].Body)
	require.Same(t, a.pin, parts[2].Body)

	links := []int{0, 1, 1} // the link index of each linkage part, in Parts order
	for _, frame := range []int{0, 86, 171, 256, 300} {
		at := time.Duration(frame) * linkageFrame
		s, err := fraction.At(at)
		require.NoError(t, err)
		f, err := scene.At(t.Context(), at)
		require.NoError(t, err)
		require.Len(t, f.Poses, 4)
		for i, k := range links {
			want := transformBits(poseAt(t, a.linkage, a.drive(), s, k))
			require.Equal(t, want, transformBits(f.Poses[i].Transform), "part %s frame %d", f.Poses[i].Name, frame)
			local, err := nodes[k].Local(at)
			require.NoError(t, err)
			require.Equal(t, want, transformBits(local), "node %d frame %d", k, frame)
		}
		require.True(t, f.Poses[3].Transform.Equal(r3.Identity(), 0))
	}
}

func TestAddLinkageRefusalsAddNoPart(t *testing.T) {
	a := newArm(t)
	wall := decadtest.NewBlock(t, decad.New(), -50, 60, 100, 70, units.Millimeters(20))
	fraction := fourSecondFraction(t)
	lengthDrive := decad.Drive{{Link: a.shoulder, From: units.Millimeters(0), To: units.Millimeters(1)}}

	noPin := armNames(a, wall)
	delete(noPin, a.pin)
	shared := armNames(a, wall)
	shared[a.pin] = "forearm"
	clash := armNames(a, wall)
	clash[a.upper] = "wall"

	for _, c := range []struct {
		name     string
		linkage  *decad.Linkage
		drive    decad.Drive
		fraction *kinetograph.Channel
		names    map[*decad.Body]string
		want     error
	}{
		{"nil linkage", nil, a.drive(), fraction, armNames(a, wall), kinetograph.ErrNilLinkage},
		{"missing name", a.linkage, a.drive(), fraction, noPin, kinetograph.ErrUnnamedBody},
		{"two bodies one name", a.linkage, a.drive(), fraction, shared, kinetograph.ErrDuplicateName},
		{"name of an existing part", a.linkage, a.drive(), fraction, clash, kinetograph.ErrDuplicateName},
		{"nil fraction", a.linkage, a.drive(), nil, armNames(a, wall), kinetograph.ErrNilChannel},
		{"refused drive", a.linkage, lengthDrive, fraction, armNames(a, wall), decad.ErrUnitKind},
	} {
		t.Run(c.name, func(t *testing.T) {
			rig := kinetograph.NewRig()
			scene := kinetograph.NewScene(rig)
			require.NoError(t, scene.AddPart("wall", rig.Root(), wall))
			nodes, err := scene.AddLinkage(c.linkage, c.drive, c.fraction, c.names)
			require.ErrorIs(t, err, c.want)
			require.Nil(t, nodes)
			require.True(t, slices.Equal([]kinetograph.PartInfo{{Name: "wall", Body: wall}}, scene.Parts()),
				"parts %v", scene.Parts())
		})
	}
}
