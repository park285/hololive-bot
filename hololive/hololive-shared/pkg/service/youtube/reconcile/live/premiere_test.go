package live

import (
	"reflect"
	"testing"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

const premiereTestVideoID = "premiere"

func TestMergeConfirmedPremieresCreatesUpcomingSession(t *testing.T) {
	scheduled := time.Date(2026, time.August, 30, 3, 0, 0, 0, time.UTC)
	received := scheduled.Add(-time.Hour)

	decision := MergeConfirmedPremieres(State{}, []ConfirmedPremiereFact{{
		VideoID:     premiereTestVideoID,
		ChannelID:   "UC_PREMIERE",
		Title:       "Premiere title",
		ScheduledAt: &scheduled,
		ReceivedAt:  received,
	}})

	if len(decision.Sessions) != 1 {
		t.Fatalf("sessions = %d, want 1", len(decision.Sessions))
	}

	got := decision.Sessions[0]
	if got.VideoID != premiereTestVideoID || got.ChannelID != "UC_PREMIERE" || got.Status != StatusUpcoming {
		t.Fatalf("created session identity = %#v", got)
	}

	if got.Title != "Premiere title" || got.ScheduledStartTime == nil || !got.ScheduledStartTime.Equal(scheduled) {
		t.Fatalf("created session metadata = %#v", got)
	}

	if !got.LastSeenAt.Equal(received) || got.IsPremiere == nil || !*got.IsPremiere {
		t.Fatalf("created session classification = %#v", got)
	}

	if len(decision.Conflicts) != 0 || len(decision.Applications) != 1 || decision.Applications[0].Decision != "APPLIED" {
		t.Fatalf("created decision = %#v", decision)
	}
}

func TestMergeConfirmedPremieresOnlyFillsUnknownClassification(t *testing.T) {
	scheduled := time.Date(2026, time.August, 30, 3, 0, 0, 0, time.UTC)
	started := scheduled.Add(time.Minute)
	ended := started.Add(time.Hour)
	seen := ended.Add(time.Minute)
	existing := SessionState{
		VideoID:            premiereTestVideoID,
		ChannelID:          "UC_PREMIERE",
		Status:             StatusEnded,
		Title:              "Live-owned title",
		TopicID:            "gaming",
		ThumbnailURL:       "https://example.test/premiere.jpg",
		ScheduledStartTime: &scheduled,
		StartedAt:          &started,
		EndedAt:            &ended,
		LiveFirstSeenAt:    &started,
		LastSeenAt:         seen,
		Clock: LiveEvidenceClock{
			LastLivePositiveAt:     &started,
			LastLivePositiveSeenAt: &started,
			EndedAt:                &ended,
		},
		Present: true,
	}
	want := existing.clone()

	want.IsPremiere = new(true)

	decision := MergeConfirmedPremieres(State{Sessions: map[string]SessionState{premiereTestVideoID: existing}}, []ConfirmedPremiereFact{{
		VideoID:     premiereTestVideoID,
		ChannelID:   "UC_OTHER",
		Title:       "Content title",
		ScheduledAt: new(scheduled.Add(24 * time.Hour)),
		ReceivedAt:  seen.Add(24 * time.Hour),
	}})

	if len(decision.Sessions) != 1 || !reflect.DeepEqual(decision.Sessions[0], want) {
		t.Fatalf("merged session = %#v, want %#v", decision.Sessions, want)
	}
}

func TestMergeConfirmedPremieresReplayAndConflict(t *testing.T) {
	tests := []struct {
		name            string
		existing        bool
		wantApplication string
		wantConflict    bool
	}{
		{name: "replay", existing: true, wantApplication: "REPLAY"},
		{name: "conflict", existing: false, wantApplication: "CONFLICT_KEEP", wantConflict: true},
	}

	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			decision := MergeConfirmedPremieres(State{Sessions: map[string]SessionState{
				premiereTestVideoID: {
					VideoID:    premiereTestVideoID,
					ChannelID:  "UC_PREMIERE",
					Status:     StatusLive,
					IsPremiere: new(test.existing),
				},
			}}, []ConfirmedPremiereFact{{VideoID: premiereTestVideoID}})

			if len(decision.Sessions) != 0 {
				t.Fatalf("sessions = %#v, want no write", decision.Sessions)
			}

			if len(decision.Applications) != 1 || decision.Applications[0].Decision != test.wantApplication {
				t.Fatalf("applications = %#v", decision.Applications)
			}

			if (len(decision.Conflicts) == 1) != test.wantConflict {
				t.Fatalf("conflicts = %#v", decision.Conflicts)
			}

			if test.wantConflict {
				conflict := decision.Conflicts[0]
				if conflict.FieldName != "is_premiere" ||
					conflict.ExistingValueSHA256 != contract.SHA256Hex([]byte("false")) ||
					conflict.AttemptedValueSHA256 != contract.SHA256Hex([]byte("true")) {
					t.Fatalf("conflict = %#v", conflict)
				}
			}
		})
	}
}

func TestSessionStateCloneSeparatesPremierePointer(t *testing.T) {
	state := SessionState{IsPremiere: new(true)}
	cloned := state.clone()

	*cloned.IsPremiere = false

	if state.IsPremiere == nil || !*state.IsPremiere {
		t.Fatalf("source is_premiere changed through clone: %v", state.IsPremiere)
	}
}
