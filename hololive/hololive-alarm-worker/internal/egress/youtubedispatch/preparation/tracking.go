package preparation

import (
	"errors"
	"fmt"
	"slices"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type TrackingEvidence struct {
	Kind          domain.OutboxKind
	PostID        string
	ActiveClaim   *lifecycle.AlarmClaimToken
	AlreadySent   bool
	ClaimDeferred bool
}

var ErrClaimDeferred = errors.New("post tracking claim is held by another execution")

func ResolveTrackingRequirement(evidence TrackingEvidence) (lifecycle.TrackingRequirement, error) {
	if evidence.Kind != domain.OutboxKindNewShort && evidence.Kind != domain.OutboxKindCommunityPost {
		return lifecycle.NoTracking{}, nil
	}

	if evidence.ActiveClaim != nil {
		requirement, err := lifecycle.NewRequireClaimOrAlreadySent(*evidence.ActiveClaim)
		if err != nil {
			return nil, fmt.Errorf("resolve tracking requirement: %w", err)
		}

		return requirement, nil
	}

	if evidence.AlreadySent {
		requirement, err := lifecycle.NewRequireAlreadySent(evidence.Kind, evidence.PostID)
		if err != nil {
			return nil, fmt.Errorf("resolve tracking requirement: %w", err)
		}

		return requirement, nil
	}

	if evidence.ClaimDeferred {
		return nil, ErrClaimDeferred
	}

	return nil, errors.New("resolve tracking requirement: neither exact claim nor durable sent evidence exists")
}

type TrackingAction uint8

const (
	TrackingNoMutation TrackingAction = iota + 1
	TrackingConsumeClaim
)

type TrackingFinalState struct {
	AlreadySent bool
	ActiveClaim *lifecycle.AlarmClaimToken
}

func EvaluateTrackingFinalization(requirement lifecycle.TrackingRequirement, state TrackingFinalState) (TrackingAction, error) {
	switch requirement := requirement.(type) {
	case lifecycle.NoTracking:
		return TrackingNoMutation, nil
	case lifecycle.RequireAlreadySent:
		if !state.AlreadySent {
			return 0, errors.New("finalize tracking: required already-sent state is absent")
		}

		return TrackingNoMutation, nil
	case lifecycle.RequireClaimOrAlreadySent:
		if state.AlreadySent {
			return TrackingNoMutation, nil
		}

		if state.ActiveClaim == nil || !sameClaimToken(requirement.Token(), *state.ActiveClaim) {
			return 0, errors.New("finalize tracking: exact claim token is absent")
		}

		return TrackingConsumeClaim, nil
	default:
		return 0, errors.New("finalize tracking: unsupported requirement")
	}
}

func DeduplicateTrackingRequirements(requirements []lifecycle.TrackingRequirement) ([]lifecycle.TrackingRequirement, error) {
	result := make([]lifecycle.TrackingRequirement, 0, len(requirements))
	indexByPost := make(map[string]int, len(requirements))

	for i := range requirements {
		requirement := requirements[i]
		if requirement == nil {
			return nil, fmt.Errorf("deduplicate tracking requirements: requirement[%d] is nil", i)
		}

		key, tracked := trackingKey(requirement)
		if !tracked {
			result = append(result, requirement)
			continue
		}

		if index, ok := indexByPost[key]; ok {
			merged, err := mergeTrackingRequirements(result[index], requirement)
			if err != nil {
				return nil, fmt.Errorf("deduplicate tracking requirements: %w", err)
			}

			result[index] = merged

			continue
		}

		indexByPost[key] = len(result)
		result = append(result, requirement)
	}

	return slices.Clone(result), nil
}

func trackingKey(requirement lifecycle.TrackingRequirement) (string, bool) {
	switch requirement := requirement.(type) {
	case lifecycle.RequireClaimOrAlreadySent:
		token := requirement.Token()
		return string(token.Kind()) + "\x00" + token.PostID(), true
	case lifecycle.RequireAlreadySent:
		return string(requirement.OutboxKind()) + "\x00" + requirement.PostID(), true
	default:
		return "", false
	}
}

func mergeTrackingRequirements(a, b lifecycle.TrackingRequirement) (lifecycle.TrackingRequirement, error) {
	claimA, aHasClaim := a.(lifecycle.RequireClaimOrAlreadySent)
	claimB, bHasClaim := b.(lifecycle.RequireClaimOrAlreadySent)

	if aHasClaim && bHasClaim && !sameClaimToken(claimA.Token(), claimB.Token()) {
		return nil, errors.New("same post has conflicting claim tokens")
	}

	if aHasClaim {
		return a, nil
	}

	if bHasClaim {
		return b, nil
	}

	return a, nil
}

func sameClaimToken(a, b lifecycle.AlarmClaimToken) bool {
	return a.Kind() == b.Kind() && a.PostID() == b.PostID() && a.AuthorizedAt().Equal(b.AuthorizedAt())
}
