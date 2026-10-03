package kinetograph

import "errors"

// The sentinel vocabulary of docs/design.md §6. Every error a constructor
// returns wraps one of these (or one of units' or r3's sentinels, where the
// design says so), so a caller branches with errors.Is.
var (
	// ErrNoKeyframes is returned by NewChannel when it is given no keyframes.
	ErrNoKeyframes = errors.New("kinetograph: channel has no keyframes")

	// ErrKeyframeOrder is returned by NewChannel when keyframe times are not
	// strictly increasing.
	ErrKeyframeOrder = errors.New("kinetograph: keyframe times are not strictly increasing")

	// ErrKind is returned when a channel or value has the wrong units.Kind for
	// where it is used: two keyframes of different kinds, an Angle channel
	// expected and a Length given, or the reverse.
	ErrKind = errors.New("kinetograph: wrong quantity kind")

	// ErrReflection is returned by Node.Fixed for a transform that mirrors.
	ErrReflection = errors.New("kinetograph: transform is a reflection")

	// ErrInvalidTransform is returned by Node.Fixed for a transform that is
	// not a rigid motion, the zero r3.Transform among them.
	ErrInvalidTransform = errors.New("kinetograph: transform is not a rigid motion")

	// ErrDegenerateDirection is returned by Node.Prismatic for a zero or
	// non-finite direction.
	ErrDegenerateDirection = errors.New("kinetograph: direction has no length")

	// ErrDuplicateName is returned by Scene.AddPart for a part name already
	// used in the scene.
	ErrDuplicateName = errors.New("kinetograph: part name already used")

	// ErrForeignNode is returned when a node belongs to a different rig than
	// the scene's, or is nil.
	ErrForeignNode = errors.New("kinetograph: node belongs to another rig")

	// ErrNilBody is returned by Scene.AddPart for a nil body, and wrapped by
	// Scene.At when a Builder returns a nil body and a nil error.
	ErrNilBody = errors.New("kinetograph: nil body")

	// ErrNilBuilder is returned by Scene.AddParametric for a nil Builder.
	ErrNilBuilder = errors.New("kinetograph: nil builder")

	// ErrNilChannel is returned for a nil channel where one is required.
	ErrNilChannel = errors.New("kinetograph: nil channel")

	// ErrNoCamera is returned when a scene without a camera is evaluated.
	ErrNoCamera = errors.New("kinetograph: scene has no camera")

	// ErrEmptyScene is returned when a scene without parts is evaluated.
	ErrEmptyScene = errors.New("kinetograph: scene has no parts")

	// ErrInvalidClip is returned by NewClip for fps < 1, duration <= 0, or a
	// frame count that does not fit an int.
	ErrInvalidClip = errors.New("kinetograph: invalid clip")

	// ErrFrameRange is returned for a frame index outside the clip.
	ErrFrameRange = errors.New("kinetograph: frame index out of range")

	// ErrNilContext is returned by every function taking a context.Context
	// when it is given nil, before any work.
	ErrNilContext = errors.New("kinetograph: nil context")
)
