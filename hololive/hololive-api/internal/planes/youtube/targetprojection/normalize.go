package targetprojection

import (
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
	"strings"
	"time"
)

type targetIdentity struct {
	subject string
	kind    string
}

type projectionHashTarget struct {
	SubjectKey      string `json:"subject_key"`
	ObservationKind string `json:"observation_kind"`
	Priority        int16  `json:"priority"`
	PollIntervalMS  int64  `json:"poll_interval_ms"`
	Enabled         bool   `json:"enabled"`
}

func normalize(targets []TargetSpec, reasons []TargetReason) ([]TargetSpec, []TargetReason, string, error) {
	if err := validateProjectionCounts(targets, reasons); err != nil {
		return nil, nil, "", err
	}
	normalizedTargets, seenTargets, err := normalizeTargets(targets)
	if err != nil {
		return nil, nil, "", err
	}
	normalizedReasons, err := normalizeReasons(reasons, seenTargets)
	if err != nil {
		return nil, nil, "", err
	}
	digest, err := hashNormalizedTargets(normalizedTargets)
	if err != nil {
		return nil, nil, "", err
	}
	return normalizedTargets, normalizedReasons, digest, nil
}

func validateProjectionCounts(targets []TargetSpec, reasons []TargetReason) error {
	if len(targets) > MaxTargetCount {
		return fmt.Errorf("%w: target count exceeds %d", ErrInvalidProjection, MaxTargetCount)
	}
	if len(reasons) > MaxReasonCount {
		return fmt.Errorf("%w: reason count exceeds %d", ErrInvalidProjection, MaxReasonCount)
	}
	return nil
}

func normalizeTargets(targets []TargetSpec) ([]TargetSpec, map[targetIdentity]TargetSpec, error) {
	normalized := make([]TargetSpec, 0, len(targets))
	seen := make(map[targetIdentity]TargetSpec, len(targets))
	for i := range targets {
		accepted, keep, err := acceptTarget(targets[i], i, seen)
		if err != nil {
			return nil, nil, err
		}
		if keep {
			normalized = append(normalized, accepted)
		}
	}
	sort.Slice(normalized, lessTargetSpec(normalized))
	return normalized, seen, nil
}

func acceptTarget(target TargetSpec, index int, seen map[targetIdentity]TargetSpec) (TargetSpec, bool, error) {
	target.SubjectKey = strings.TrimSpace(target.SubjectKey)
	if err := validateTargetFields(target, index); err != nil {
		return TargetSpec{}, false, err
	}
	identity := targetIdentity{subject: target.SubjectKey, kind: string(target.ObservationKind)}
	if previous, ok := seen[identity]; ok {
		if previous != target {
			return TargetSpec{}, false, fmt.Errorf("%w: target %s/%s has conflicting scheduling fields", ErrInvalidProjection, target.SubjectKey, target.ObservationKind)
		}
		return TargetSpec{}, false, nil
	}
	seen[identity] = target
	return target, true, nil
}

func validateTargetFields(target TargetSpec, index int) error {
	if target.SubjectKey == "" || len(target.SubjectKey) > 256 {
		return fmt.Errorf("%w: target %d subject is outside bounds", ErrInvalidProjection, index)
	}
	if !target.ObservationKind.Valid() {
		return fmt.Errorf("%w: target %d kind %q is invalid", ErrInvalidProjection, index, target.ObservationKind)
	}
	if target.Priority < 0 || target.Priority > 100 {
		return fmt.Errorf("%w: target %d priority is outside 0..100", ErrInvalidProjection, index)
	}
	if invalidPollInterval(target.PollInterval) {
		return fmt.Errorf("%w: target %d poll interval is outside schema bounds", ErrInvalidProjection, index)
	}
	return nil
}

func invalidPollInterval(interval time.Duration) bool {
	return interval < time.Second || interval > 24*time.Hour || interval%time.Millisecond != 0
}

func lessTargetSpec(targets []TargetSpec) func(int, int) bool {
	return func(i, j int) bool {
		if targets[i].SubjectKey != targets[j].SubjectKey {
			return targets[i].SubjectKey < targets[j].SubjectKey
		}
		return targets[i].ObservationKind < targets[j].ObservationKind
	}
}

type reasonIdentity struct {
	targetIdentity
	kind string
	key  string
}

func normalizeReasons(reasons []TargetReason, seenTargets map[targetIdentity]TargetSpec) ([]TargetReason, error) {
	normalized := make([]TargetReason, 0, len(reasons))
	seen := make(map[reasonIdentity]struct{}, len(reasons))
	for i := range reasons {
		reason, keep, err := acceptReason(reasons[i], i, seenTargets, seen)
		if err != nil {
			return nil, err
		}
		if keep {
			normalized = append(normalized, reason)
		}
	}
	sort.Slice(normalized, lessTargetReason(normalized))
	return normalized, nil
}

func acceptReason(
	reason TargetReason,
	index int,
	seenTargets map[targetIdentity]TargetSpec,
	seen map[reasonIdentity]struct{},
) (TargetReason, bool, error) {
	reason.SubjectKey = strings.TrimSpace(reason.SubjectKey)
	reason.ReasonKind = strings.TrimSpace(reason.ReasonKind)
	reason.ReasonKey = strings.TrimSpace(reason.ReasonKey)
	identity := targetIdentity{subject: reason.SubjectKey, kind: string(reason.ObservationKind)}
	if _, ok := seenTargets[identity]; !ok {
		return TargetReason{}, false, fmt.Errorf("%w: reason %d does not reference a target", ErrInvalidProjection, index)
	}
	if invalidReasonBounds(reason) {
		return TargetReason{}, false, fmt.Errorf("%w: reason %d is outside bounds", ErrInvalidProjection, index)
	}
	reasonID := reasonIdentity{targetIdentity: identity, kind: reason.ReasonKind, key: reason.ReasonKey}
	if _, ok := seen[reasonID]; ok {
		return TargetReason{}, false, nil
	}
	seen[reasonID] = struct{}{}
	return reason, true, nil
}

func invalidReasonBounds(reason TargetReason) bool {
	return reason.ReasonKind == "" || len(reason.ReasonKind) > 128 || reason.ReasonKey == "" || len(reason.ReasonKey) > 512
}

func lessTargetReason(reasons []TargetReason) func(int, int) bool {
	return func(i, j int) bool {
		return targetReasonLess(reasons[i], reasons[j])
	}
}

func targetReasonLess(left, right TargetReason) bool {
	if left.SubjectKey != right.SubjectKey {
		return left.SubjectKey < right.SubjectKey
	}
	if left.ObservationKind != right.ObservationKind {
		return left.ObservationKind < right.ObservationKind
	}
	if left.ReasonKind != right.ReasonKind {
		return left.ReasonKind < right.ReasonKind
	}
	return left.ReasonKey < right.ReasonKey
}

func hashNormalizedTargets(targets []TargetSpec) (string, error) {
	hashInput := make([]projectionHashTarget, len(targets))
	for i := range targets {
		target := targets[i]
		hashInput[i] = projectionHashTarget{
			SubjectKey: target.SubjectKey, ObservationKind: string(target.ObservationKind),
			Priority: target.Priority, PollIntervalMS: target.PollInterval.Milliseconds(), Enabled: target.Enabled,
		}
	}
	encoded, err := json.Marshal(hashInput)
	if err != nil {
		return "", fmt.Errorf("%w: encode projection hash input: %w", ErrInvalidProjection, err)
	}
	digest := sha256.Sum256(encoded)
	return hex.EncodeToString(digest[:]), nil
}

func sameReasons(left, right []TargetReason) bool {
	if len(left) != len(right) {
		return false
	}
	for i := range left {
		if left[i] != right[i] {
			return false
		}
	}
	return true
}
