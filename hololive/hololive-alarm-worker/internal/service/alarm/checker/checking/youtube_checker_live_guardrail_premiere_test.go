package checking

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestObservePersistedLiveGuardrailsExcludesConfirmedPremiere(t *testing.T) {
	t.Parallel()

	for name, persistedPremiere := range map[string]bool{
		"persisted premiere": true,
		"merged premiere":    false,
	} {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			now := time.Date(2026, time.September, 10, 10, 0, 0, 0, time.UTC)
			ordinary := &domain.Stream{ID: "ordinary-live", ChannelID: testChannelID1, Status: domain.StreamStatusLive}
			premiere := &domain.Stream{ID: "premiere-live", ChannelID: testChannelID1, Status: domain.StreamStatusLive, IsPremiere: persistedPremiere}
			classifiedPremiere := *premiere

			classifiedPremiere.IsPremiere = true

			var logs bytes.Buffer

			checker := &YouTubeChecker{
				persistedLiveSource: &guardrailEvidenceSource{},
				logger:              slog.New(slog.NewTextHandler(&logs, nil)),
			}

			checker.observePersistedLiveGuardrails(t.Context(), []PersistedYouTubeLiveSession{
				{Stream: ordinary, LastSeenAt: now, LiveFirstSeenAt: now.Add(-3 * time.Minute)},
				{Stream: premiere, LastSeenAt: now, LiveFirstSeenAt: now.Add(-3 * time.Minute)},
			}, map[string][]*domain.Stream{
				testChannelID1: {ordinary, &classifiedPremiere},
			}, map[string][]string{testChannelID1: {testRoomID1}}, now)

			assert.Contains(t, logs.String(), "alarm.youtube.live_guardrail.missing_dispatch")
			assert.Contains(t, logs.String(), "stream_id=ordinary-live")
			assert.NotContains(t, logs.String(), "stream_id=premiere-live")
		})
	}
}
