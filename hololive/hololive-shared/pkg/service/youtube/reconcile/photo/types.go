package photo

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type Policy struct {
	ChangeMinObservations int
	ChangeStability       time.Duration
}

func (p Policy) ChangeEnabled() bool {
	return p.ChangeMinObservations >= 2 && p.ChangeStability > 0
}

type Variant struct {
	Kind               string
	URL                string
	Width              int
	Height             int
	StableMediaID      string
	ContentFingerprint string
}

type Sample struct {
	ChannelID     string
	Provider      contract.Provider
	Variants      []Variant
	Complete      bool
	ObservationID int64
	ScheduledFor  time.Time
	EffectiveAt   time.Time
	ReceivedAt    time.Time
}

type Canonical struct {
	Identity     string
	URL          string
	Width        int
	Height       int
	EffectiveAt  *time.Time
	Candidate    string
	CandidateURL string
	CandidateW   int
	CandidateH   int
	Slots        int
	FirstAt      *time.Time
	LastAt       *time.Time
	FirstRx      *time.Time
}

type Head struct {
	ChannelID string
	Kinds     map[string]Canonical
}

type State struct {
	ChannelID string
	Head      Head
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
	WriteProduct map[string]Canonical
	Conflicts    []Conflict
	Applications []Application
}

func Identity(variant Variant) string {
	if variant.StableMediaID != "" {
		return "id:" + variant.StableMediaID
	}
	if variant.ContentFingerprint != "" {
		return "fp:" + variant.ContentFingerprint
	}
	return ""
}
