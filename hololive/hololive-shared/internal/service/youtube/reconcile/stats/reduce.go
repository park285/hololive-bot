package stats

import (
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func Reduce(state State, evidence Evidence) (Decision, error) {
	if evidence.Sample.ChannelID == "" {
		return Decision{}, errors.New("channel stats reducer received empty channel id")
	}

	digest, err := sampleDigest(&evidence.Sample)
	if err != nil {
		return Decision{}, fmt.Errorf("sample digest: %w", err)
	}

	workingState := state.clone()
	workingEvidence := evidence.clone()
	sample := &workingEvidence.Sample

	sample.Provider = workingEvidence.Provider
	sample.ObservationID = workingEvidence.ObservationID

	head := workingState.Head

	head.ChannelID = evidence.Sample.ChannelID

	if sameSlot(head.UnresolvedScheduledFor, sample.ScheduledFor) {
		return replayOrConflict(&workingState, sample, digest, &head), nil
	}

	if head.LastResolvedScheduledFor != nil && sample.ScheduledFor.Before(*head.LastResolvedScheduledFor) {
		return olderSlot(&workingState, sample, digest, &head), nil
	}

	if sameSlot(head.LastResolvedScheduledFor, sample.ScheduledFor) || slotHasConflict(workingState.Slot, sample) {
		return replayOrConflict(&workingState, sample, digest, &head), nil
	}

	return advanceResolved(&head, sample), nil
}

func olderSlot(state *State, sample *Sample, digest string, head *Head) Decision {
	if slotHasConflict(state.Slot, sample) {
		return unresolvedDecision(head, sample, firstConflictingDigest(state.Slot, sample), digest, false)
	}

	for i := range state.Slot {
		if state.Slot[i].Digest == digest {
			return replayDecision(head, sample)
		}
	}

	return Decision{
		Sample:        sample,
		Head:          *head,
		WriteSnapshot: true,
		Applications: []Application{{
			EntityKind: "youtube_channel_stats", EntityKey: sample.ChannelID, Decision: "OLDER_RETAINED",
		}},
	}
}

func slotHasConflict(existing []SlotEvidence, sample *Sample) bool {
	for i := range existing {
		if samplesConflict(slotSample(&existing[i]), sample) {
			return true
		}
	}

	return false
}

func firstConflictingDigest(existing []SlotEvidence, sample *Sample) string {
	for i := range existing {
		if samplesConflict(slotSample(&existing[i]), sample) {
			return existing[i].Digest
		}
	}

	return ""
}

func replayOrConflict(state *State, sample *Sample, digest string, head *Head) Decision {
	if decision, ok := matchSlotEvidence(state.Slot, sample, digest, head); ok {
		return decision
	}

	if sameSlot(head.LastResolvedScheduledFor, sample.ScheduledFor) && lastResolvedConflicts(head, sample) {
		return unresolvedDecision(head, sample, lastResolvedDigest(head), digest, true)
	}

	if sameSlot(head.LastResolvedScheduledFor, sample.ScheduledFor) {
		return replayDecision(head, sample)
	}

	return advanceResolved(head, sample)
}

func matchSlotEvidence(existing []SlotEvidence, sample *Sample, digest string, head *Head) (Decision, bool) {
	for i := range existing {
		item := &existing[i]
		if item.Provider == sample.Provider {
			if item.Digest == digest {
				return replayDecision(head, sample), true
			}

			return unresolvedDecision(head, sample, item.Digest, digest, true), true
		}

		if samplesConflict(slotSample(item), sample) {
			return unresolvedDecision(head, sample, item.Digest, digest, true), true
		}
	}

	return Decision{}, false
}

func advanceResolved(head *Head, sample *Sample) Decision {
	head.PriorResolvedScheduledFor = head.LastResolvedScheduledFor
	head.PriorResolvedSubscriberCount = head.LastResolvedSubscriberCount
	head.PriorResolvedViewCount = head.LastResolvedViewCount
	head.PriorResolvedVideoCount = head.LastResolvedVideoCount
	head.LastResolvedScheduledFor = copyTime(sample.ScheduledFor)
	head.LastResolvedSubscriberCount = mergeCount(sample.SubscriberCovered, sample.SubscriberCount, head.LastResolvedSubscriberCount)
	head.LastResolvedViewCount = mergeCount(sample.ViewCovered, sample.ViewCount, head.LastResolvedViewCount)
	head.LastResolvedVideoCount = mergeCount(sample.VideoCovered, sample.VideoCount, head.LastResolvedVideoCount)
	head.UnresolvedScheduledFor = nil

	return Decision{
		Sample:        sample,
		Head:          *head,
		WriteSnapshot: true,
		Applications: []Application{{
			EntityKind: "youtube_channel_stats", EntityKey: sample.ChannelID, Decision: "APPLIED",
		}},
	}
}

func unresolvedDecision(head *Head, sample *Sample, existingDigest, attemptedDigest string, revertCurrent bool) Decision {
	if revertCurrent && sameSlot(head.LastResolvedScheduledFor, sample.ScheduledFor) {
		head.LastResolvedScheduledFor = head.PriorResolvedScheduledFor
		head.LastResolvedSubscriberCount = head.PriorResolvedSubscriberCount
		head.LastResolvedViewCount = head.PriorResolvedViewCount
		head.LastResolvedVideoCount = head.PriorResolvedVideoCount
		head.PriorResolvedScheduledFor = nil
		head.PriorResolvedSubscriberCount = nil
		head.PriorResolvedViewCount = nil
		head.PriorResolvedVideoCount = nil
	}

	head.UnresolvedScheduledFor = copyTime(sample.ScheduledFor)

	return Decision{
		Sample:        sample,
		Head:          *head,
		ClearSnapshot: true,
		Conflict: &Conflict{
			FieldName:            "channel_stats",
			ExistingValueSHA256:  existingDigest,
			AttemptedValueSHA256: attemptedDigest,
		},
		Applications: []Application{{
			EntityKind: "youtube_channel_stats", EntityKey: sample.ChannelID, Decision: "UNRESOLVED",
		}},
	}
}

func replayDecision(head *Head, sample *Sample) Decision {
	return Decision{
		Sample: sample,
		Head:   *head,
		Applications: []Application{{
			EntityKind: "youtube_channel_stats", EntityKey: sample.ChannelID, Decision: "REPLAY",
		}},
	}
}

func SampleDigest(sample *Sample) (string, error) {
	out, err := sampleDigest(sample)
	if err != nil {
		return out, fmt.Errorf("sample digest: %w", err)
	}

	return out, nil
}

func sampleDigest(sample *Sample) (string, error) {
	payload, err := jsonv2.Marshal(struct {
		SubscriberCount   *int64 `json:"subscriber_count"`
		ViewCount         *int64 `json:"view_count"`
		VideoCount        *int64 `json:"video_count"`
		SubscriberCovered bool   `json:"subscriber_covered"`
		ViewCovered       bool   `json:"view_covered"`
		VideoCovered      bool   `json:"video_covered"`
	}{
		SubscriberCount: sample.SubscriberCount, ViewCount: sample.ViewCount, VideoCount: sample.VideoCount,
		SubscriberCovered: sample.SubscriberCovered, ViewCovered: sample.ViewCovered, VideoCovered: sample.VideoCovered,
	}, jsonv2.Deterministic(true))
	if err != nil {
		return "", fmt.Errorf("marshal channel stats digest: %w", err)
	}

	return contract.SHA256Hex(payload), nil
}

func lastResolvedDigest(head *Head) string {
	digestSample := Sample{
		SubscriberCount:   head.LastResolvedSubscriberCount,
		ViewCount:         head.LastResolvedViewCount,
		VideoCount:        head.LastResolvedVideoCount,
		SubscriberCovered: head.LastResolvedSubscriberCount != nil,
		ViewCovered:       head.LastResolvedViewCount != nil,
		VideoCovered:      head.LastResolvedVideoCount != nil,
	}

	digest, err := sampleDigest(&digestSample)
	if err != nil {
		return ""
	}

	return digest
}

func lastResolvedConflicts(head *Head, sample *Sample) bool {
	resolved := Sample{
		SubscriberCount:   head.LastResolvedSubscriberCount,
		ViewCount:         head.LastResolvedViewCount,
		VideoCount:        head.LastResolvedVideoCount,
		SubscriberCovered: sample.SubscriberCovered && head.LastResolvedSubscriberCount != nil,
		ViewCovered:       sample.ViewCovered && head.LastResolvedViewCount != nil,
		VideoCovered:      sample.VideoCovered && head.LastResolvedVideoCount != nil,
	}

	return samplesConflict(&resolved, sample)
}

func samplesConflict(left, right *Sample) bool {
	return overlappingCountConflicts(left.SubscriberCovered, left.SubscriberCount, right.SubscriberCovered, right.SubscriberCount) ||
		overlappingCountConflicts(left.ViewCovered, left.ViewCount, right.ViewCovered, right.ViewCount) ||
		overlappingCountConflicts(left.VideoCovered, left.VideoCount, right.VideoCovered, right.VideoCount)
}

func overlappingCountConflicts(leftCovered bool, left *int64, rightCovered bool, right *int64) bool {
	if !leftCovered || !rightCovered {
		return false
	}

	if left == nil && right == nil {
		return false
	}

	if left == nil || right == nil {
		return true
	}

	return *left != *right
}

func slotSample(item *SlotEvidence) *Sample {
	return &Sample{
		Provider:          item.Provider,
		SubscriberCount:   item.SubscriberCount,
		ViewCount:         item.ViewCount,
		VideoCount:        item.VideoCount,
		SubscriberCovered: item.SubscriberCovered,
		ViewCovered:       item.ViewCovered,
		VideoCovered:      item.VideoCovered,
	}
}

func mergeCount(covered bool, incoming, previous *int64) *int64 {
	if covered {
		return copyCount(incoming)
	}

	return copyCount(previous)
}

func sameSlot(existing *time.Time, scheduled time.Time) bool {
	return existing != nil && existing.Equal(scheduled)
}

func copyTime(value time.Time) *time.Time {
	copied := value.UTC()
	return &copied
}

func copyCount(value *int64) *int64 {
	if value == nil {
		return nil
	}

	copied := *value

	return &copied
}
