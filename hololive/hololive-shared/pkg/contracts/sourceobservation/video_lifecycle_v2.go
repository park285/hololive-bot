package sourceobservation

import (
	"errors"
	"time"
)

func validateVideoLifecycleV2(payload *VideoLiveCheckV1, observedAt time.Time) error {
	if payload.StartedAt != nil && payload.StartedAt.After(observedAt) {
		return errors.New("video lifecycle start is after observation")
	}

	if payload.HasLiveBroadcastDetails != nil && !*payload.HasLiveBroadcastDetails &&
		(payload.IsLiveNow != nil || payload.StartedAt != nil || payload.EndedAt != nil) {
		return errors.New("video lifecycle broadcast facts contradict missing details")
	}

	if payload.WaitingStateConfirmed == nil || !*payload.WaitingStateConfirmed {
		return nil
	}

	if payload.ScheduledAt == nil || !isTrue(payload.IsUpcoming) || payload.IsLiveNow == nil || *payload.IsLiveNow ||
		payload.CurrentlyLive() || payload.EndedAt != nil || payload.StartedAt != nil {
		return errors.New("video lifecycle waiting proof is contradictory")
	}

	return nil
}
