package timeline

import (
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func NormalizePostTrackingIdentities(identities []PostTrackingIdentity) ([]PostTrackingIdentity, error) {
	if len(identities) == 0 {
		return nil, nil
	}

	normalized := make([]PostTrackingIdentity, 0, len(identities))
	seen := make(map[string]struct{}, len(identities))
	for i := range identities {
		identity, ok, err := normalizePostTrackingIdentity(identities[i])
		if err != nil {
			return nil, err
		}
		if !ok {
			continue
		}
		key := PostTrackingIdentityKey(identity.Kind, identity.ContentID)
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		normalized = append(normalized, identity)
	}
	return normalized, nil
}

func normalizePostTrackingIdentity(identity PostTrackingIdentity) (PostTrackingIdentity, bool, error) {
	contentID := strings.TrimSpace(identity.ContentID)
	if contentID == "" {
		return PostTrackingIdentity{}, false, nil
	}
	switch identity.Kind {
	case domain.OutboxKindCommunityPost, domain.OutboxKindNewShort:
		return PostTrackingIdentity{Kind: identity.Kind, ContentID: contentID}, true, nil
	case domain.OutboxKindNewVideo, domain.OutboxKindLiveStream, domain.OutboxKindMilestone:
		return PostTrackingIdentity{}, false, fmt.Errorf("unsupported tracking identity kind: %s", identity.Kind)
	default:
		return PostTrackingIdentity{}, false, fmt.Errorf("unsupported tracking identity kind: %s", identity.Kind)
	}
}

func PostTrackingIdentityKey(kind domain.OutboxKind, contentID string) string {
	trimmed := strings.TrimSpace(contentID)
	if trimmed == "" {
		return ""
	}
	return string(kind) + ":" + trimmed
}
