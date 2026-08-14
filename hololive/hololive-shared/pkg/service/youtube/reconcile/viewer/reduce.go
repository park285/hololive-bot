package viewer

import (
	"encoding/json"
	"fmt"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

func Reduce(state State, evidence Evidence) (Decision, error) {
	if evidence.Sample.VideoID == "" {
		return Decision{}, fmt.Errorf("viewer reducer received empty video id")
	}
	digest, err := sampleDigest(evidence.Sample.Availability, evidence.Sample.ViewerCount)
	if err != nil {
		return Decision{}, err
	}
	sample := evidence.Sample
	sample.Provider = evidence.Provider
	sample.ObservationID = evidence.ObservationID
	head := state.Head
	head.VideoID = evidence.Sample.VideoID
	if sameWindow(head.UnresolvedWindowStart, sample.WindowStart) {
		return replayOrConflict(state, sample, digest, head, "WINDOW_UNRESOLVED")
	}
	if head.LastResolvedWindowStart != nil && sample.WindowStart.Before(*head.LastResolvedWindowStart) {
		return Decision{
			Sample: &sample,
			Head:   head,
			Applications: []Application{{
				EntityKind: "youtube_live_viewer_sample", EntityKey: windowKey(sample), Decision: "OLDER_WINDOW_RETAINED",
			}},
		}, nil
	}
	if sameWindow(head.LastResolvedWindowStart, sample.WindowStart) || windowHasConflict(state.Window, digest) {
		return replayOrConflict(state, sample, digest, head, "EQUAL_WINDOW")
	}
	return advanceResolved(head, sample), nil
}

func windowHasConflict(existing []WindowEvidence, digest string) bool {
	for _, item := range existing {
		if item.Digest != "" && item.Digest != digest {
			return true
		}
	}
	return false
}

func replayOrConflict(state State, sample Sample, digest string, head Head, decision string) (Decision, error) {
	for _, existing := range state.Window {
		if existing.Provider == sample.Provider {
			if existing.Digest == digest {
				return Decision{
					Sample: &sample,
					Head:   head,
					Applications: []Application{{
						EntityKind: "youtube_live_viewer_sample", EntityKey: windowKey(sample), Decision: "REPLAY",
					}},
				}, nil
			}
			return unresolvedDecision(head, sample, existing.Digest, digest), nil
		}
		if existing.Digest != digest {
			return unresolvedDecision(head, sample, existing.Digest, digest), nil
		}
	}
	if decision == "EQUAL_WINDOW" && lastResolvedDigest(head) != digest {
		return unresolvedDecision(head, sample, lastResolvedDigest(head), digest), nil
	}
	return Decision{
		Sample: &sample,
		Head:   head,
		Applications: []Application{{
			EntityKind: "youtube_live_viewer_sample", EntityKey: windowKey(sample), Decision: "REPLAY",
		}},
	}, nil
}

func advanceResolved(head Head, sample Sample) Decision {
	head.PriorResolvedWindowStart = head.LastResolvedWindowStart
	head.PriorResolvedCount = head.LastResolvedCount
	head.PriorResolvedAvailability = head.LastResolvedAvailability
	head.LastResolvedWindowStart = copyTime(sample.WindowStart)
	head.LastResolvedCount = copyCount(sample.ViewerCount)
	head.LastResolvedAvailability = sample.Availability
	head.UnresolvedWindowStart = nil
	return Decision{
		Sample: &sample,
		Head:   head,
		Applications: []Application{{
			EntityKind: "youtube_live_viewer_sample", EntityKey: windowKey(sample), Decision: "APPLIED",
		}},
	}
}

func unresolvedDecision(head Head, sample Sample, existingDigest, attemptedDigest string) Decision {
	if sameWindow(head.LastResolvedWindowStart, sample.WindowStart) {
		head.LastResolvedWindowStart = head.PriorResolvedWindowStart
		head.LastResolvedCount = head.PriorResolvedCount
		head.LastResolvedAvailability = head.PriorResolvedAvailability
		head.PriorResolvedWindowStart = nil
		head.PriorResolvedCount = nil
		head.PriorResolvedAvailability = ""
	}
	head.UnresolvedWindowStart = copyTime(sample.WindowStart)
	conflict := Conflict{
		FieldName:            "viewer_count",
		ExistingValueSHA256:  existingDigest,
		AttemptedValueSHA256: attemptedDigest,
	}
	return Decision{
		Sample:       &sample,
		Head:         head,
		ClearProduct: true,
		Conflict:     &conflict,
		Applications: []Application{{
			EntityKind: "youtube_live_viewer_sample", EntityKey: windowKey(sample), Decision: "UNRESOLVED",
		}},
	}
}

func SampleDigest(availability string, count *int64) (string, error) {
	return sampleDigest(availability, count)
}

func sampleDigest(availability string, count *int64) (string, error) {
	payload, err := json.Marshal(struct {
		Availability string `json:"availability"`
		ViewerCount  *int64 `json:"viewer_count"`
	}{Availability: availability, ViewerCount: count})
	if err != nil {
		return "", fmt.Errorf("marshal viewer sample digest: %w", err)
	}
	return contract.SHA256Hex(payload), nil
}

func lastResolvedDigest(head Head) string {
	digest, err := sampleDigest(head.LastResolvedAvailability, head.LastResolvedCount)
	if err != nil {
		return ""
	}
	return digest
}

func windowKey(sample Sample) string {
	return sample.VideoID + "/" + sample.WindowStart.UTC().Format(time.RFC3339Nano)
}

func sameWindow(existing *time.Time, window time.Time) bool {
	return existing != nil && existing.Equal(window)
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
