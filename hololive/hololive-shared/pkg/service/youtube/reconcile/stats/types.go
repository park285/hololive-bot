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
