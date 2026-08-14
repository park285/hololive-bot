package profile

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type Policy struct {
	ClearMinObservations int
	ClearStability       time.Duration
}

func (p Policy) ClearEnabled() bool {
	return p.ClearMinObservations >= 2 && p.ClearStability > 0
}

type Field struct {
	Present bool
	Value   string
}

type Sample struct {
	ChannelID     string
	Provider      contract.Provider
	Handle        Field
	Description   Field
	Country       Field
	JoinedDate    Field
	Complete      bool
	ObservationID int64
	ScheduledFor  time.Time
	EffectiveAt   time.Time
	ReceivedAt    time.Time
}

type CanonicalField struct {
	Set          bool
	Value        string
	EffectiveAt  *time.Time
	EmptySlots   int
	EmptyFirstAt *time.Time
	EmptyLastAt  *time.Time
	EmptyFirstRx *time.Time
}

type Head struct {
	ChannelID   string
	Handle      CanonicalField
	Description CanonicalField
	Country     CanonicalField
	JoinedDate  CanonicalField
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
	WriteHead    bool
	Conflicts    []Conflict
	Applications []Application
}
