package kinetograph

import (
	"context"
	"fmt"
	"slices"
	"time"

	"github.com/lestrrat-3d/decad"
	"github.com/lestrrat-3d/r3"
	"github.com/lestrrat-3d/units"
)

// LinkageTrack is the TransformTrack of one link of a decad.Linkage moving
// along a decad.Drive. At(t) reads the drive fraction s from a Dimensionless
// channel and returns the link's world pose from Linkage.PoseAt, the call
// Document.VerifyLinkage builds every pose it checks with, so a frame draws
// exactly the pose decad checked at that s.
//
// A LinkageTrack holds no mutable state, and PoseAt writes nothing, so At
// returns the same transform for the same t and is safe to call from several
// goroutines at once. It reads the linkage on every call: the caller MUST NOT
// add links to the linkage while a scene that reads it is evaluated.
type LinkageTrack struct {
	linkage  *decad.Linkage
	drive    decad.Drive // the track's own copy
	index    int         // the link's position in linkage.Links()
	fraction *Channel    // Dimensionless
}

// NewLinkageTrack returns the track of link under drive, reading the drive
// fraction s at t from fraction.At(t). A Linear fraction from
// units.Scalar(0) to units.Scalar(1) runs the drive once between its two
// keyframes and holds its ends outside them.
//
// NewLinkageTrack copies drive, each sweep's Via included, and calls
// linkage.PoseAt(drive, units.Scalar(0)) once to run decad's checks of the
// drive. It returns ErrNilLinkage for a nil linkage, ErrForeignLink unless
// link is one of linkage.Links() (nil and the ground link are not),
// ErrNilChannel for a nil fraction, ErrKind unless fraction.Kind() is
// units.Dimensionless, and PoseAt's error, wrapped, when decad refuses the
// drive.
func NewLinkageTrack(linkage *decad.Linkage, drive decad.Drive, link *decad.Link, fraction *Channel) (*LinkageTrack, error) {
	if linkage == nil {
		return nil, fmt.Errorf("%w: argument linkage", ErrNilLinkage)
	}
	index := -1
	if link != nil {
		index = slices.Index(linkage.Links(), link)
	}
	if index < 0 {
		return nil, fmt.Errorf("%w: argument link", ErrForeignLink)
	}
	drive, err := checkLinkageDrive(linkage, drive, fraction)
	if err != nil {
		return nil, err
	}
	return &LinkageTrack{linkage: linkage, drive: drive, index: index, fraction: fraction}, nil
}

// checkLinkageFraction returns ErrNilChannel for a nil fraction and ErrKind
// unless fraction is Dimensionless.
func checkLinkageFraction(fraction *Channel) error {
	if fraction == nil {
		return fmt.Errorf("%w: argument fraction", ErrNilChannel)
	}
	if fraction.Kind() != units.Dimensionless {
		return fmt.Errorf("%w: linkage fraction is %s, want %s", ErrKind, fraction.Kind(), units.Dimensionless)
	}
	return nil
}

// checkLinkageDrive checks fraction, and drive against linkage, and returns
// a copy of drive.
func checkLinkageDrive(linkage *decad.Linkage, drive decad.Drive, fraction *Channel) (decad.Drive, error) {
	if err := checkLinkageFraction(fraction); err != nil {
		return nil, err
	}
	owned := make(decad.Drive, len(drive))
	for i, sweep := range drive {
		sweep.Via = slices.Clone(sweep.Via)
		owned[i] = sweep
	}
	if _, err := linkage.PoseAt(owned, units.Scalar(0)); err != nil {
		return nil, fmt.Errorf("kinetograph: linkage drive: %w", err)
	}
	return owned, nil
}

// At returns the link's pose from linkage.PoseAt(drive, s) with
// s = fraction.At(t), passed to PoseAt unchanged: a value outside [0, 1]
// extends the drive's first or last segment, as PoseAt defines. It returns
// the channel's error wrapped, and PoseAt's error wrapped with s.
func (k *LinkageTrack) At(t time.Duration) (r3.Transform, error) {
	s, err := k.fraction.At(t)
	if err != nil {
		return r3.Transform{}, fmt.Errorf("kinetograph: linkage fraction: %w", err)
	}
	pose, err := k.linkage.PoseAt(k.drive, s)
	if err != nil {
		return r3.Transform{}, fmt.Errorf("kinetograph: linkage pose at s = %s: %w", s, err)
	}
	return pose.Poses[k.index], nil
}

// ScheduleTrack is the TransformTrack of one link of a decad.Linkage moving
// along a decad.Schedule, which Linkage.Schedule builds once from a drive for
// repeated PoseAt calls. At(t) reads the drive fraction s from a Dimensionless channel
// and returns the link's world pose from Schedule.PoseAt.
// Document.VerifyLinkage reads every pose of a drive that moves a closed loop
// through a schedule of the same inputs, so a frame draws exactly the pose
// decad checked at that s.
//
// For a drive that moves a loop, Linkage.PoseAt builds a new schedule on
// every call, while one Schedule keeps the loop's scene and caches the
// enclosures it has asked for. A LinkageTrack and a ScheduleTrack over the
// same drive return equal poses bit for bit; use a ScheduleTrack when the
// linkage has a loop. For a tree linkage, Schedule.PoseAt is Linkage.PoseAt.
//
// A ScheduleTrack holds no mutable state of its own. Schedule.PoseAt is safe
// for concurrent use and returns equal poses for equal fractions, so At
// returns the same transform for the same t and is safe to call from several
// goroutines at once. The caller MUST NOT add links to the schedule's
// linkage while a scene that reads it is evaluated.
type ScheduleTrack struct {
	schedule *decad.Schedule
	index    int      // the link's position in schedule.Linkage().Links()
	fraction *Channel // Dimensionless
}

// NewScheduleTrack returns the track of link under schedule, reading the
// drive fraction s at t from fraction.At(t). The schedule has already checked
// its drive, so NewScheduleTrack calls no PoseAt.
//
// It returns ErrNilSchedule for a nil schedule, ErrForeignLink unless link is
// one of schedule.Linkage().Links() (nil and the ground link are not),
// ErrNilChannel for a nil fraction, and ErrKind unless fraction.Kind() is
// units.Dimensionless.
func NewScheduleTrack(schedule *decad.Schedule, link *decad.Link, fraction *Channel) (*ScheduleTrack, error) {
	if schedule == nil {
		return nil, fmt.Errorf("%w: argument schedule", ErrNilSchedule)
	}
	index := -1
	if link != nil {
		index = slices.Index(schedule.Linkage().Links(), link)
	}
	if index < 0 {
		return nil, fmt.Errorf("%w: argument link", ErrForeignLink)
	}
	if err := checkLinkageFraction(fraction); err != nil {
		return nil, err
	}
	return &ScheduleTrack{schedule: schedule, index: index, fraction: fraction}, nil
}

// At returns the link's pose from schedule.PoseAt with s = fraction.At(t),
// passed to PoseAt unchanged under context.Background(). For a drive that
// moves a loop, PoseAt refuses an s outside [0, 1], and an s past the point
// where decad could certify the loop, with decad.ErrUnsupported; keep the
// channel inside the drive. It returns the channel's error wrapped, and
// PoseAt's error wrapped with s.
func (k *ScheduleTrack) At(t time.Duration) (r3.Transform, error) {
	s, err := k.fraction.At(t)
	if err != nil {
		return r3.Transform{}, fmt.Errorf("kinetograph: schedule fraction: %w", err)
	}
	pose, err := k.schedule.PoseAt(context.Background(), s)
	if err != nil {
		return r3.Transform{}, fmt.Errorf("kinetograph: schedule pose at s = %s: %w", s, err)
	}
	return pose.Poses[k.index], nil
}

// AddLinkage adds one driven node per link of linkage directly under the
// rig's root, each driven by a LinkageTrack over drive and fraction, and
// attaches every body of every link to its link's node as a part named
// names[body]. PoseAt returns world poses, so under the root a node's Local
// is its link's world pose. It returns the nodes in Linkage.Links() order; a
// caller attaches more parts, lights or the camera to them as to any node.
//
// Parts are added in Linkage.Links() order and, within a link, in
// Link.Bodies() order. names is only looked up: an entry for a body that no
// link holds is ignored, so one map may also name the bodies the caller
// attaches with AddPart.
//
// It returns ErrNilLinkage for a nil linkage, NewLinkageTrack's errors for
// fraction and drive, ErrUnnamedBody for a link body with no entry in names,
// and ErrDuplicateName for a name another part, or another body of the
// linkage, already uses. A failed AddLinkage adds no part. AddLinkage is not
// safe beside At or AtCached.
func (s *Scene) AddLinkage(linkage *decad.Linkage, drive decad.Drive, fraction *Channel,
	names map[*decad.Body]string) ([]*Node, error) {
	if linkage == nil {
		return nil, fmt.Errorf("%w: argument linkage", ErrNilLinkage)
	}
	drive, err := checkLinkageDrive(linkage, drive, fraction)
	if err != nil {
		return nil, err
	}
	return s.addLinks(linkage, names, func(k int) TransformTrack {
		return &LinkageTrack{linkage: linkage, drive: drive, index: k, fraction: fraction}
	})
}

// AddSchedule is AddLinkage for a prepared schedule: one driven node per
// link of schedule.Linkage() directly under the rig's root, each driven by a
// ScheduleTrack over schedule and fraction, with every link body attached as
// AddLinkage attaches it. It returns the nodes in Linkage.Links() order.
//
// It returns ErrNilSchedule for a nil schedule, NewScheduleTrack's errors
// for fraction, and AddLinkage's errors for names. A failed AddSchedule adds
// no part. AddSchedule is not safe beside At or AtCached.
func (s *Scene) AddSchedule(schedule *decad.Schedule, fraction *Channel,
	names map[*decad.Body]string) ([]*Node, error) {
	if schedule == nil {
		return nil, fmt.Errorf("%w: argument schedule", ErrNilSchedule)
	}
	if err := checkLinkageFraction(fraction); err != nil {
		return nil, err
	}
	return s.addLinks(schedule.Linkage(), names, func(k int) TransformTrack {
		return &ScheduleTrack{schedule: schedule, index: k, fraction: fraction}
	})
}

// addLinks adds one driven node per link of linkage under the root, driven
// by trackOf(k) for the link at index k, and the links' bodies as parts. It
// checks every name before it adds the first part.
func (s *Scene) addLinks(linkage *decad.Linkage, names map[*decad.Body]string,
	trackOf func(k int) TransformTrack) ([]*Node, error) {
	links := linkage.Links()
	nodes := make([]*Node, len(links))
	var added []part
	taken := make(map[string]struct{}) // the names this call adds; lookup only, never iterated
	for k, link := range links {
		node, err := s.rig.Root().Driven(trackOf(k))
		if err != nil {
			return nil, err
		}
		nodes[k] = node
		for j, body := range link.Bodies() {
			name, ok := names[body]
			if !ok {
				return nil, fmt.Errorf("%w: link %d body %d", ErrUnnamedBody, k, j)
			}
			_, used := s.names[name]
			_, again := taken[name]
			if used || again {
				return nil, fmt.Errorf("%w: part %q", ErrDuplicateName, name)
			}
			taken[name] = struct{}{}
			added = append(added, part{name: name, node: node, body: body})
		}
	}
	for _, p := range added {
		s.names[p.name] = struct{}{}
		s.parts = append(s.parts, p)
	}
	return nodes, nil
}
