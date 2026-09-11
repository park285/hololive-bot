package youtubejscollector

import (
	jsonv2 "encoding/json/v2"
	"fmt"
	"testing"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejs"
)

const restrictedTestChannelID = "UC_TEST"

func TestChannelLiveRunnerPublishesRestrictedSchedulesAsPartial(t *testing.T) {
	t.Parallel()

	for _, restrictedOnly := range []bool{false, true} {
		t.Run(fmt.Sprintf("restricted_only=%t", restrictedOnly), func(t *testing.T) {
			t.Parallel()

			var response youtubejs.ChannelResult

			loadJSON(t, "channel.json", &response)

			if restrictedOnly {
				response.LiveSessions = nil
			}

			response.UnavailableLiveSessions = []youtubejs.UnavailableLiveSession{{
				VideoID: "restricted-video", ChannelID: restrictedTestChannelID, Reason: "access_restricted",
			}}

			input := youtubeInput(t, restrictedTestChannelID, "youtubejs_channel_live", contract.KindLiveSnapshot)

			result, err := NewChannelLiveRunner(&channelFake{result: response}).Collect(t.Context(), input)
			if err != nil {
				t.Fatal(err)
			}

			observations := result.Output().Observations()
			checkpoints := result.Output().Checkpoints()

			if result.Kind() != collectutil.CollectComplete || len(observations) != 1 || len(checkpoints) != 1 {
				t.Fatalf("poll must complete with one partial observation: %#v", result)
			}

			observation := observations[0]
			if observation.Completeness != contract.CompletenessPartial ||
				contract.NegativeEligible(observation.Completeness, observation.Continuity) {
				t.Fatalf("restricted schedule became absence evidence: %#v", observation)
			}

			if !checkpoints[0].LastScheduledFor.Equal(observation.ScheduledFor) {
				t.Fatal("checkpoint did not preserve the observed poll slot")
			}

			var payload contract.LiveSnapshotV1

			if err := jsonv2.Unmarshal(observation.Payload, &payload); err != nil {
				t.Fatal(err)
			}

			if len(payload.Sessions) != len(response.LiveSessions) {
				t.Fatalf("valid sessions changed: %#v", payload.Sessions)
			}

			for _, session := range payload.Sessions {
				if session.VideoID == "restricted-video" {
					t.Fatal("unknown schedule was promoted to a canonical session")
				}
			}
		})
	}
}

func TestChannelRunnerRejectsInvalidUnavailableSessions(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name   string
		change func(*youtubejs.ChannelResult)
	}{
		{"other_channel", func(r *youtubejs.ChannelResult) { r.UnavailableLiveSessions[0].ChannelID = "UC_OTHER" }},
		{"empty_identity", func(r *youtubejs.ChannelResult) { r.UnavailableLiveSessions[0].VideoID = " " }},
		{"unknown_reason", func(r *youtubejs.ChannelResult) { r.UnavailableLiveSessions[0].Reason = "parser_drift" }},
		{"duplicate", func(r *youtubejs.ChannelResult) {
			r.UnavailableLiveSessions = append(r.UnavailableLiveSessions, r.UnavailableLiveSessions[0])
		}},
		{"overlap", func(r *youtubejs.ChannelResult) { r.UnavailableLiveSessions[0].VideoID = r.LiveSessions[0].VideoID }},
		{"missing_tab", func(r *youtubejs.ChannelResult) { r.MissingTab = true }},
		{"over_budget", func(r *youtubejs.ChannelResult) {
			for i := range 32 {
				r.UnavailableLiveSessions = append(r.UnavailableLiveSessions, youtubejs.UnavailableLiveSession{
					VideoID: fmt.Sprintf("restricted-%d", i), ChannelID: restrictedTestChannelID, Reason: "access_restricted",
				})
			}
		}},
	}
	for _, testCase := range cases {
		t.Run(testCase.name, func(t *testing.T) {
			t.Parallel()

			var response youtubejs.ChannelResult

			loadJSON(t, "channel.json", &response)

			response.UnavailableLiveSessions = []youtubejs.UnavailableLiveSession{{
				VideoID: "restricted-video", ChannelID: restrictedTestChannelID, Reason: "access_restricted",
			}}
			testCase.change(&response)

			result, err := NewChannelLiveRunner(&channelFake{result: response}).Collect(t.Context(),
				youtubeInput(t, restrictedTestChannelID, "youtubejs_channel_live", contract.KindLiveSnapshot))
			if err == nil || collecterr.CodeOf(err) != collecterr.ParserDrift || !result.IsZero() {
				t.Fatalf("invalid unresolved identity was published: result=%#v err=%v", result, err)
			}
		})
	}
}
