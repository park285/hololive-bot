package stats

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type Sample struct {
	ChannelID         string
	Provider          contract.Provider
	SubscriberCount   *int64
	ViewCount         *int64
	VideoCount        *int64
	SubscriberCovered bool
	ViewCovered       bool
	VideoCovered      bool
	ObservationID     int64
	ScheduledFor      time.Time
	EffectiveAt       time.Time
	ReceivedAt        time.Time
}

type Head struct {
	ChannelID                    string
	LastResolvedScheduledFor     *time.Time
	LastResolvedSubscriberCount  *int64
	LastResolvedViewCount        *int64
	LastResolvedVideoCount       *int64
	PriorResolvedScheduledFor    *time.Time
	PriorResolvedSubscriberCount *int64
	PriorResolvedViewCount       *int64
	PriorResolvedVideoCount      *int64
	UnresolvedScheduledFor       *time.Time
}

type SlotEvidence struct {
	Provider          contract.Provider
	SubscriberCount   *int64
	ViewCount         *int64
	VideoCount        *int64
	SubscriberCovered bool
	ViewCovered       bool
	VideoCovered      bool
	Digest            string
}

type State struct {
	ChannelID string
	Head      Head
	Slot      []SlotEvidence
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
	Sample        *Sample
	Head          Head
	WriteSnapshot bool
	ClearSnapshot bool
	Conflict      *Conflict
	Applications  []Application
}

func (s *State) clone() State {
	cloned := *s

	cloned.Head = s.Head.clone()
	cloned.Slot = make([]SlotEvidence, len(s.Slot))

	for i := range s.Slot {
		cloned.Slot[i] = s.Slot[i].clone()
	}

	return cloned
}

func (h *Head) clone() Head {
	cloned := *h

	cloned.LastResolvedScheduledFor = copyTimePointer(h.LastResolvedScheduledFor)
	cloned.LastResolvedSubscriberCount = copyCount(h.LastResolvedSubscriberCount)
	cloned.LastResolvedViewCount = copyCount(h.LastResolvedViewCount)
	cloned.LastResolvedVideoCount = copyCount(h.LastResolvedVideoCount)
	cloned.PriorResolvedScheduledFor = copyTimePointer(h.PriorResolvedScheduledFor)
	cloned.PriorResolvedSubscriberCount = copyCount(h.PriorResolvedSubscriberCount)
	cloned.PriorResolvedViewCount = copyCount(h.PriorResolvedViewCount)
	cloned.PriorResolvedVideoCount = copyCount(h.PriorResolvedVideoCount)
	cloned.UnresolvedScheduledFor = copyTimePointer(h.UnresolvedScheduledFor)

	return cloned
}

func (s *SlotEvidence) clone() SlotEvidence {
	cloned := *s

	cloned.SubscriberCount = copyCount(s.SubscriberCount)
	cloned.ViewCount = copyCount(s.ViewCount)
	cloned.VideoCount = copyCount(s.VideoCount)

	return cloned
}

func (e *Evidence) clone() Evidence {
	cloned := *e

	cloned.Sample.SubscriberCount = copyCount(e.Sample.SubscriberCount)
	cloned.Sample.ViewCount = copyCount(e.Sample.ViewCount)
	cloned.Sample.VideoCount = copyCount(e.Sample.VideoCount)

	return cloned
}

func copyTimePointer(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}

	return copyTime(*value)
}
