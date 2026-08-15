package sourceobservation

import (
	"fmt"
	"strings"
	"time"
)

func (proof *LeaseProof) validate(scheduledFor time.Time) error {
	if err := validateBoundedText("lease job key", proof.JobKey, 512); err != nil {
		return err
	}
	if err := validateBoundedText("collection job kind", proof.CollectionJobKind, 128); err != nil {
		return err
	}
	if err := validateBoundedText("lease owner instance", proof.OwnerInstance, 128); err != nil {
		return err
	}
	if proof.FenceEpoch <= 0 || proof.ProjectionGeneration <= 0 {
		return fmt.Errorf("validate source observation envelope: lease fence and projection generation must be positive")
	}
	if proof.ScheduledFor.IsZero() || !proof.ScheduledFor.Equal(scheduledFor) {
		return fmt.Errorf("validate source observation envelope: lease scheduled slot mismatch")
	}
	return nil
}

func (p Provider) Valid() bool {
	return p == ProviderHolodex || p == ProviderYouTubeJS || p == ProviderHololiveOfficial
}

func (k ObservationKind) Valid() bool {
	switch k {
	case KindCommunityPage, KindVideoList, KindShortsList, KindLiveSnapshot,
		KindViewerSample, KindChannelStats, KindChannelProfile, KindChannelPhoto, KindSchedule:
		return true
	default:
		return false
	}
}

func (c Completeness) Valid() bool {
	return c == CompletenessComplete || c == CompletenessPartial || c == CompletenessUnknown
}

func (c Continuity) Valid() bool {
	return c == ContinuityContiguous || c == ContinuityGapUnresolved || c == ContinuityNotApplicable
}

func (s Status) Valid() bool {
	return s == StatusPending || s == StatusProcessing || s == StatusProcessed || s == StatusDeadLetter
}

func NegativeEligible(completeness Completeness, continuity Continuity) bool {
	return completeness == CompletenessComplete &&
		(continuity == ContinuityContiguous || continuity == ContinuityNotApplicable)
}

func KindAllowsSourceEventTime(kind ObservationKind) bool {
	switch kind {
	case KindCommunityPage, KindLiveSnapshot, KindViewerSample, KindChannelProfile, KindChannelPhoto, KindSchedule:
		return true
	case KindVideoList, KindShortsList, KindChannelStats:
		return false
	default:
		return false
	}
}

func ValidateMaxSourceEventFutureSkew(value time.Duration) error {
	if value < 0 || value > MaxSourceEventFutureSkew {
		return fmt.Errorf("source event future skew must be between zero and %s", MaxSourceEventFutureSkew)
	}
	return nil
}

func SourceEventAtAllowed(observation ObservationClock, maxFutureSkew time.Duration) bool {
	if observation.SourceEventAt == nil || observation.ReceivedAt.IsZero() ||
		!KindAllowsSourceEventTime(observation.ObservationKind) || ValidateMaxSourceEventFutureSkew(maxFutureSkew) != nil {
		return false
	}
	return !observation.SourceEventAt.After(observation.ReceivedAt.Add(maxFutureSkew))
}

func EffectiveAt(observation ObservationClock, maxFutureSkew time.Duration) (time.Time, bool) {
	if SourceEventAtAllowed(observation, maxFutureSkew) {
		return observation.SourceEventAt.UTC(), false
	}
	return observation.ScheduledFor.UTC(), observation.SourceEventAt != nil
}

func validateBoundedText(name, value string, maxLength int) error {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return fmt.Errorf("validate source observation envelope: %s is empty", name)
	}
	if len(value) > maxLength {
		return fmt.Errorf("validate source observation envelope: %s exceeds %d bytes", name, maxLength)
	}
	if trimmed != value {
		return fmt.Errorf("validate source observation envelope: %s must not contain surrounding whitespace", name)
	}
	return nil
}
