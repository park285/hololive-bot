package live

import (
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type ConfirmedPremiereFact struct {
	VideoID     string
	ChannelID   string
	Title       string
	ScheduledAt *time.Time
	ReceivedAt  time.Time
}

type PremiereConflict struct {
	VideoID              string
	FieldName            string
	ExistingValueSHA256  string
	AttemptedValueSHA256 string
}

type PremiereDecision struct {
	Sessions     []SessionState
	Conflicts    []PremiereConflict
	Applications []Application
}

func MergeConfirmedPremieres(state State, facts []ConfirmedPremiereFact) PremiereDecision {
	working := state.clone()
	if working.Sessions == nil {
		working.Sessions = map[string]SessionState{}
	}

	decision := PremiereDecision{
		Sessions:     make([]SessionState, 0, len(facts)),
		Conflicts:    make([]PremiereConflict, 0),
		Applications: make([]Application, 0, len(facts)),
	}

	for i := range facts {
		mergeConfirmedPremiere(&working, &decision, &facts[i])
	}

	return decision
}

func mergeConfirmedPremiere(state *State, decision *PremiereDecision, fact *ConfirmedPremiereFact) {
	existing, ok := state.Sessions[fact.VideoID]
	if !ok {
		created := SessionState{
			VideoID:            fact.VideoID,
			ChannelID:          fact.ChannelID,
			Status:             StatusUpcoming,
			Title:              fact.Title,
			ScheduledStartTime: copyOptionalTime(fact.ScheduledAt),
			LastSeenAt:         fact.ReceivedAt.UTC(),
			IsPremiere:         new(true),
			Present:            true,
		}

		state.Sessions[fact.VideoID] = created
		decision.Sessions = append(decision.Sessions, created)
		decision.Applications = append(decision.Applications, premiereApplication(fact.VideoID, "APPLIED"))

		return
	}

	if existing.IsPremiere == nil {
		existing = existing.clone()
		existing.IsPremiere = new(true)

		state.Sessions[fact.VideoID] = existing
		decision.Sessions = append(decision.Sessions, existing)
		decision.Applications = append(decision.Applications, premiereApplication(fact.VideoID, "APPLIED"))

		return
	}

	if *existing.IsPremiere {
		decision.Applications = append(decision.Applications, premiereApplication(fact.VideoID, "REPLAY"))

		return
	}

	decision.Conflicts = append(decision.Conflicts, PremiereConflict{
		VideoID:              fact.VideoID,
		FieldName:            "is_premiere",
		ExistingValueSHA256:  contract.SHA256Hex([]byte("false")),
		AttemptedValueSHA256: contract.SHA256Hex([]byte("true")),
	})
	decision.Applications = append(decision.Applications, premiereApplication(fact.VideoID, "CONFLICT_KEEP"))
}

func premiereApplication(videoID, decision string) Application {
	return Application{
		EntityKind: "youtube_live_session",
		EntityKey:  videoID,
		Decision:   decision,
	}
}
