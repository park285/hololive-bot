package telemetry

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

// PostIDLogValue returns the canonical logical ID (contentid.ResolveDeliveryLogicalID) for audit log post_id.
// An invalid identity is logged as an "invalid:<kind>:<reason>" label instead of a substitute ID, following
// DedupeKeyLogValue; the label carries only the value-redacted reason.
func PostIDLogValue(kind domain.OutboxKind, contentID, payload string) string {
	postID, err := contentid.ResolveDeliveryLogicalID(kind, contentID, payload)
	if err == nil {
		return postID
	}

	reason := "invalid_identity"

	if identityErr, ok := errors.AsType[*contentid.Error](err); ok {
		reason = string(identityErr.Reason)
	}

	return fmt.Sprintf("invalid:%s:%s", strings.TrimSpace(string(kind)), reason)
}
