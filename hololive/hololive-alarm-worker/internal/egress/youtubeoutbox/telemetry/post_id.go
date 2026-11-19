package telemetry

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

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
