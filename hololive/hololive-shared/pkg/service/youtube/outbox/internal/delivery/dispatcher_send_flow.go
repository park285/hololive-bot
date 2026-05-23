package delivery

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/logschema"
	"github.com/park285/iris-client-go/iris"
)

type deliverySendRequest struct {
	roomID     string
	message    string
	dedupeKeys []string
}

const (
	communityShortsDeliveryPath        = "youtube_outbox_dispatcher"
	deliveryDedupeKeyLogField          = logschema.FieldDedupeKey
	deliveryAttemptStartedLogMessage   = logschema.CommunityShortsDeliveryAttemptMessage
	deliveryAttemptStartedAtLogField   = logschema.FieldAttemptStartedAt
	deliveryResultLogMessage           = logschema.CommunityShortsDeliveryResultMessage
	deliveryAuditLogMessage            = logschema.CommunityShortsDeliveryAuditMessage
	deliveryAuditPostIDLogField        = logschema.FieldPostID
	deliveryAuditContentIDLogField     = logschema.FieldContentID
	deliveryAuditAlarmTypeLogField     = logschema.FieldAlarmType
	deliveryAuditSentAtLogField        = logschema.FieldSentAt
	deliveryAuditSendResultLogField    = logschema.FieldSendResult
	deliveryAuditFailureReasonLogField = logschema.FieldFailureReason
	deliveryAuditModeLogField          = logschema.FieldDeliveryMode
	deliveryAuditPathLogField          = logschema.FieldDeliveryPath
)

var ErrDeliveryDedupeKeyRequired = errors.New("delivery dedupe key is required")
var errDeliverySendTimeout = errors.New("delivery send timeout exceeded")

var deliveryFailureReasonBySentinel = []struct {
	err       error
	reason    string
	permanent bool
}{
	{err: iris.ErrAuthFailed, reason: "auth", permanent: true},
	{err: iris.ErrRateLimited, reason: "rate-limited"},
	{err: iris.ErrTransport, reason: "transport"},
	{err: iris.ErrPermanent, reason: "http-permanent", permanent: true},
}

func buildDeliverySendRequest(roomID, message string, outboxes []domain.YouTubeNotificationOutbox) (deliverySendRequest, error) {
	if strings.TrimSpace(roomID) == "" {
		return deliverySendRequest{}, fmt.Errorf("build delivery send request: room id is empty")
	}
	if strings.TrimSpace(message) == "" {
		return deliverySendRequest{}, fmt.Errorf("build delivery send request: message is empty")
	}
	if len(outboxes) == 0 {
		return deliverySendRequest{}, fmt.Errorf("build delivery send request: outboxes are empty")
	}

	dedupeKeys := make([]string, 0, len(outboxes))
	for i := range outboxes {
		dedupeKey, err := outboxes[i].DedupeKey()
		if err != nil {
			return deliverySendRequest{}, fmt.Errorf("%w: build delivery send request: outbox[%d] dedupe key: %w", ErrDeliveryDedupeKeyRequired, i, err)
		}
		dedupeKeys = append(dedupeKeys, dedupeKey)
	}

	req := deliverySendRequest{
		roomID:     roomID,
		message:    message,
		dedupeKeys: dedupeKeys,
	}
	if err := validateDeliverySendRequest(req); err != nil {
		return deliverySendRequest{}, fmt.Errorf("build delivery send request: %w", err)
	}

	return req, nil
}

func dedupeKeyLogAttr(dedupeKeys []string) slog.Attr {
	cloned := make([]string, 0, len(dedupeKeys))
	for i := range dedupeKeys {
		cloned = append(cloned, strings.TrimSpace(dedupeKeys[i]))
	}
	return slog.Any(deliveryDedupeKeyLogField, cloned)
}

func validateDeliverySendRequest(req deliverySendRequest) error {
	if strings.TrimSpace(req.roomID) == "" {
		return fmt.Errorf("send delivery message: room id is empty")
	}
	if strings.TrimSpace(req.message) == "" {
		return fmt.Errorf("send delivery message: message is empty")
	}
	if len(req.dedupeKeys) == 0 {
		return fmt.Errorf("%w: send delivery message: dedupe keys are empty", ErrDeliveryDedupeKeyRequired)
	}
	for i := range req.dedupeKeys {
		if strings.TrimSpace(req.dedupeKeys[i]) == "" {
			return fmt.Errorf("%w: send delivery message: dedupe key at index %d is empty", ErrDeliveryDedupeKeyRequired, i)
		}
	}
	return nil
}

func deliveryFailureReason(err error) string {
	if errors.Is(err, ErrDeliveryDedupeKeyRequired) {
		return "dedupe key"
	}
	for _, item := range deliveryFailureReasonBySentinel {
		if errors.Is(err, item.err) {
			return item.reason
		}
	}
	return "send message"
}

func deliveryFailureReasonIsPermanent(reason string) bool {
	for _, item := range deliveryFailureReasonBySentinel {
		if item.reason == reason {
			return item.permanent
		}
	}
	return false
}

func deliveryClientRequestID(roomID string, dedupeKeys []string) string {
	parts := make([]string, 0, len(dedupeKeys)+2)
	parts = append(parts, "youtube-outbox-delivery-v1")
	parts = append(parts, strings.TrimSpace(roomID))
	for i := range dedupeKeys {
		parts = append(parts, strings.TrimSpace(dedupeKeys[i]))
	}
	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))
	return "hololive-outbox:" + hex.EncodeToString(sum[:16])
}
