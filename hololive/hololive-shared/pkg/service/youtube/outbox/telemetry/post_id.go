package telemetry

import (
	"errors"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

// PostIDLogValue는 감사 로그의 post_id에 정본 논리 ID를 사용한다.
// 잘못된 식별자는 DedupeKeyLogValue와 같은 값 비노출 형식인 invalid:<kind>:<reason>으로 기록한다.
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
