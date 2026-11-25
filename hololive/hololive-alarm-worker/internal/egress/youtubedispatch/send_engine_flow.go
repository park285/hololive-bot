package youtubedispatch

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"strings"
	"time"

	"github.com/park285/iris-client-go/v6/iris"

	"github.com/kapu/hololive-alarm-worker/internal/egress/sendoutcome"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
	messagedelivery "github.com/kapu/hololive-shared/pkg/service/delivery"
)

type deliverySendRequest struct {
	clientRequestID string
	frozen          *store.FrozenRequest
	roomID          string
	variants        messagedelivery.MessageVariants
	message         string
	dedupeKeys      []string
}

var (
	ErrDeliveryDedupeKeyRequired  = errors.New("delivery dedupe key is required")
	errDeliverySendTimeout        = errors.New("delivery send timeout exceeded")
	errDeliverySendOutcomeUnknown = errors.New("delivery send outcome unknown")
)

const (
	deliveryReasonAuth        = "auth"
	deliveryReasonRateLimited = "rate-limited"
	deliveryReasonTransport   = "transport"
	deliveryReasonSendMessage = "send message"
	deliveryReasonPermanent   = "http-permanent"
)

var deliveryFailureReasonBySentinel = []struct {
	err       error
	reason    string
	permanent bool
}{
	{err: iris.ErrAuthFailed, reason: deliveryReasonAuth, permanent: true},
	{err: iris.ErrRateLimited, reason: deliveryReasonRateLimited},
	{err: iris.ErrTransport, reason: deliveryReasonTransport},
	{err: iris.ErrNotSent, reason: deliveryReasonTransport},
	{err: iris.ErrPermanent, reason: deliveryReasonPermanent, permanent: true},
}

func buildDeliverySendRequest(roomID string, message messagedelivery.MessageVariants, outboxes []domain.YouTubeNotificationOutbox) (deliverySendRequest, error) {
	if strings.TrimSpace(roomID) == "" {
		return deliverySendRequest{}, errors.New("build delivery send request: room id is empty")
	}

	if strings.TrimSpace(message.Plain) == "" || strings.TrimSpace(message.RichText) == "" {
		return deliverySendRequest{}, errors.New("build delivery send request: message is empty")
	}

	if len(outboxes) == 0 {
		return deliverySendRequest{}, errors.New("build delivery send request: outboxes are empty")
	}

	dedupeKeys := make([]string, 0, len(outboxes))
	for i := range outboxes {
		dedupeKey, err := outboxes[i].DedupeKey()
		if err != nil {
			return deliverySendRequest{}, fmt.Errorf("%w: build delivery send request: outbox[%d] dedupe key: %w", ErrDeliveryDedupeKeyRequired, i, err)
		}

		dedupeKeys = append(dedupeKeys, dedupeKey)
	}

	if err := validateDeliveryDedupeKeys(dedupeKeys); err != nil {
		return deliverySendRequest{}, fmt.Errorf("build delivery send request: %w", err)
	}

	return deliverySendRequest{roomID: roomID, variants: message, dedupeKeys: dedupeKeys}, nil
}

func dedupeKeyLogAttr(dedupeKeys []string) slog.Attr {
	return slog.Group(FieldDedupeKey, slog.Int("count", len(dedupeKeys)))
}

func validateDeliverySendRequest(req deliverySendRequest) error {
	if strings.TrimSpace(req.roomID) == "" {
		return errors.New("send delivery message: room id is empty")
	}

	if strings.TrimSpace(req.message) == "" {
		return errors.New("send delivery message: message is empty")
	}

	return validateDeliveryDedupeKeys(req.dedupeKeys)
}

func validateDeliveryDedupeKeys(dedupeKeys []string) error {
	if len(dedupeKeys) == 0 {
		return fmt.Errorf("%w: send delivery message: dedupe keys are empty", ErrDeliveryDedupeKeyRequired)
	}

	for i := range dedupeKeys {
		if strings.TrimSpace(dedupeKeys[i]) == "" {
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

	return deliveryReasonSendMessage
}

func shouldFallbackGroupedSend(err error) bool {
	if httpErr, ok := errors.AsType[*iris.HTTPError](err); ok && httpErr.StatusCode == http.StatusConflict {
		return false
	}

	return !errors.Is(err, errDeliverySendOutcomeUnknown) && deliveryFailureReason(err) == deliveryReasonPermanent
}

func deliverySendOutcomeUnknown(err error) bool {
	kind := sendoutcome.Classify(err)
	return kind == sendoutcome.OutcomeUnknown || kind == sendoutcome.TransportAmbiguous || errors.Is(err, errDeliverySendTimeout)
}

const maxDeliveryRetryAfter = 5 * time.Minute

func deliveryRetryAfter(err error) time.Duration {
	if httpErr, ok := errors.AsType[*iris.HTTPError](err); ok && httpErr != nil && httpErr.RetryAfter > 0 {
		if httpErr.RetryAfter > maxDeliveryRetryAfter {
			observeDeliveryRetryAfterClamped()

			return maxDeliveryRetryAfter
		}

		return httpErr.RetryAfter
	}

	return 0
}

func deliveryClientRequestID(roomID string, dedupeKeys []string) string {
	parts := make([]string, 0, len(dedupeKeys)+2)

	parts = append(parts, "youtube-outbox-delivery-v1", strings.TrimSpace(roomID))

	for i := range dedupeKeys {
		parts = append(parts, strings.TrimSpace(dedupeKeys[i]))
	}

	sum := sha256.Sum256([]byte(strings.Join(parts, "\x00")))

	return "hololive-outbox:" + hex.EncodeToString(sum[:16])
}
