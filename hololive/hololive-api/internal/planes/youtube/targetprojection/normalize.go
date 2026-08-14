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
	if len(targets) > MaxTargetCount {
		return nil, nil, "", fmt.Errorf("%w: target count exceeds %d", ErrInvalidProjection, MaxTargetCount)
	}
	if len(reasons) > MaxReasonCount {
		return nil, nil, "", fmt.Errorf("%w: reason count exceeds %d", ErrInvalidProjection, MaxReasonCount)
	}
	normalizedTargets := make([]TargetSpec, 0, len(targets))
	seenTargets := make(map[targetIdentity]TargetSpec, len(targets))
	for i := range targets {
		target := targets[i]
		target.SubjectKey = strings.TrimSpace(target.SubjectKey)
		if target.SubjectKey == "" || len(target.SubjectKey) > 256 {
			return nil, nil, "", fmt.Errorf("%w: target %d subject is outside bounds", ErrInvalidProjection, i)
		}
		if !target.ObservationKind.Valid() {
			return nil, nil, "", fmt.Errorf("%w: target %d kind %q is invalid", ErrInvalidProjection, i, target.ObservationKind)
		}
		if target.Priority < 0 || target.Priority > 100 {
			return nil, nil, "", fmt.Errorf("%w: target %d priority is outside 0..100", ErrInvalidProjection, i)
		}
		if target.PollInterval < time.Second || target.PollInterval > 24*time.Hour || target.PollInterval%time.Millisecond != 0 {
			return nil, nil, "", fmt.Errorf("%w: target %d poll interval is outside schema bounds", ErrInvalidProjection, i)
		}
		identity := targetIdentity{subject: target.SubjectKey, kind: string(target.ObservationKind)}
		if previous, ok := seenTargets[identity]; ok {
			if previous != target {
				return nil, nil, "", fmt.Errorf("%w: target %s/%s has conflicting scheduling fields", ErrInvalidProjection, target.SubjectKey, target.ObservationKind)
			}
			continue
		}
		seenTargets[identity] = target
		normalizedTargets = append(normalizedTargets, target)
	}
	sort.Slice(normalizedTargets, func(i, j int) bool {
		if normalizedTargets[i].SubjectKey != normalizedTargets[j].SubjectKey {
			return normalizedTargets[i].SubjectKey < normalizedTargets[j].SubjectKey
		}
		return normalizedTargets[i].ObservationKind < normalizedTargets[j].ObservationKind
	})

	type reasonIdentity struct {
		targetIdentity
		kind string
		key  string
	}
	normalizedReasons := make([]TargetReason, 0, len(reasons))
	seenReasons := make(map[reasonIdentity]struct{}, len(reasons))
	for i := range reasons {
		reason := reasons[i]
		reason.SubjectKey = strings.TrimSpace(reason.SubjectKey)
		reason.ReasonKind = strings.TrimSpace(reason.ReasonKind)
		reason.ReasonKey = strings.TrimSpace(reason.ReasonKey)
		identity := targetIdentity{subject: reason.SubjectKey, kind: string(reason.ObservationKind)}
		if _, ok := seenTargets[identity]; !ok {
			return nil, nil, "", fmt.Errorf("%w: reason %d does not reference a target", ErrInvalidProjection, i)
		}
		if reason.ReasonKind == "" || len(reason.ReasonKind) > 128 || reason.ReasonKey == "" || len(reason.ReasonKey) > 512 {
			return nil, nil, "", fmt.Errorf("%w: reason %d is outside bounds", ErrInvalidProjection, i)
		}
		reasonID := reasonIdentity{targetIdentity: identity, kind: reason.ReasonKind, key: reason.ReasonKey}
		if _, ok := seenReasons[reasonID]; ok {
			continue
		}
		seenReasons[reasonID] = struct{}{}
		normalizedReasons = append(normalizedReasons, reason)
	}
	sort.Slice(normalizedReasons, func(i, j int) bool {
		left, right := normalizedReasons[i], normalizedReasons[j]
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
	})

	hashInput := make([]projectionHashTarget, len(normalizedTargets))
	for i := range normalizedTargets {
		target := normalizedTargets[i]
		hashInput[i] = projectionHashTarget{
			SubjectKey: target.SubjectKey, ObservationKind: string(target.ObservationKind),
			Priority: target.Priority, PollIntervalMS: target.PollInterval.Milliseconds(), Enabled: target.Enabled,
		}
	}
	encoded, err := json.Marshal(hashInput)
	if err != nil {
		return nil, nil, "", fmt.Errorf("%w: encode projection hash input: %w", ErrInvalidProjection, err)
	}
	digest := sha256.Sum256(encoded)
	return normalizedTargets, normalizedReasons, hex.EncodeToString(digest[:]), nil
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
