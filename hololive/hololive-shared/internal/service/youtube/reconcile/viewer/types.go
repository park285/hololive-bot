package viewer

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

const (
	AvailabilityAvailable   = "AVAILABLE"
	AvailabilityHidden      = "HIDDEN"
	AvailabilityUnavailable = "UNAVAILABLE"
	ResolutionResolved      = "RESOLVED"
	ResolutionUnresolved    = "UNRESOLVED"
)

type Sample struct {
	VideoID       string
	Provider      contract.Provider
	ViewerCount   *int64
	Availability  string
	WindowStart   time.Time
	WindowSeconds int
	ObservationID int64
	ScheduledFor  time.Time
	EffectiveAt   time.Time
	ReceivedAt    time.Time
}

type Head struct {
	VideoID                   string
	LastResolvedWindowStart   *time.Time
	LastResolvedCount         *int64
	LastResolvedAvailability  string
	PriorResolvedWindowStart  *time.Time
	PriorResolvedCount        *int64
	PriorResolvedAvailability string
	UnresolvedWindowStart     *time.Time
}

type WindowEvidence struct {
	Provider     contract.Provider
	ViewerCount  *int64
	Availability string
	Digest       string
}

type State struct {
	VideoID string
	Head    Head
	Window  []WindowEvidence
}

type Evidence struct {
	ObservationID int64
	Provider      contract.Provider
	Sample        Sample
}

type Conflict struct {
	FieldName            string
	ExistingValueSHA256  string
	AttemptedValueSHA256 string
}

type Application struct {
	EntityKind string
	EntityKey  string
	Decision   string
}

type Decision struct {
	Sample       *Sample
	Head         Head
	ClearProduct bool
	Conflict     *Conflict
	Applications []Application
}

func (s *State) clone() State {
	cloned := *s

	cloned.Head = s.Head.clone()
	cloned.Window = make([]WindowEvidence, len(s.Window))

	for i := range s.Window {
		cloned.Window[i] = s.Window[i].clone()
	}

	return cloned
}

func (h *Head) clone() Head {
	cloned := *h

	cloned.LastResolvedWindowStart = copyTimePointer(h.LastResolvedWindowStart)
	cloned.LastResolvedCount = copyCount(h.LastResolvedCount)
	cloned.PriorResolvedWindowStart = copyTimePointer(h.PriorResolvedWindowStart)
	cloned.PriorResolvedCount = copyCount(h.PriorResolvedCount)
	cloned.UnresolvedWindowStart = copyTimePointer(h.UnresolvedWindowStart)

	return cloned
}

func (w *WindowEvidence) clone() WindowEvidence {
	cloned := *w

	cloned.ViewerCount = copyCount(w.ViewerCount)

	return cloned
}

func (e *Evidence) clone() Evidence {
	cloned := *e

	cloned.Sample.ViewerCount = copyCount(e.Sample.ViewerCount)

	return cloned
}

func copyTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	return copyTime(*value)
}
