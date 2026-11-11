package domain

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
)

type AlarmDispatchSourceKind string

const (
	AlarmDispatchSourceKindCelebration    AlarmDispatchSourceKind = "celebration"
	AlarmDispatchSourceKindDeliveryDigest AlarmDispatchSourceKind = "delivery_digest"
	// AlarmDispatchSourceKindXSpace는 X 스페이스 시작 알림을 기존 LIVE 구독으로 전달한다.
	AlarmDispatchSourceKindXSpace AlarmDispatchSourceKind = "x_space"

	maxDeliveryDigestPeriodKeyBytes = 256
	maxDeliveryDigestMessageBytes   = 64 * 1024
)

type DeliveryDigestDispatchPayload struct {
	Kind               DeliveryOutboxKind `json:"kind"`
	PeriodKey          string             `json:"period_key"`
	PreRenderedMessage string             `json:"pre_rendered_message"`
}

func (p *DeliveryDigestDispatchPayload) Validate() error {
	if p == nil {
		return errors.New("delivery digest dispatch payload is nil")
	}

	if !p.Kind.IsValid() {
		return fmt.Errorf("delivery digest dispatch payload kind %q is invalid", p.Kind)
	}

	periodKey := strings.TrimSpace(p.PeriodKey)
	if periodKey == "" {
		return errors.New("delivery digest dispatch payload period key is empty")
	}

	if len(periodKey) > maxDeliveryDigestPeriodKeyBytes {
		return fmt.Errorf("delivery digest dispatch payload period key is too long: %d > %d bytes", len(periodKey), maxDeliveryDigestPeriodKeyBytes)
	}

	message := strings.TrimSpace(p.PreRenderedMessage)
	if message == "" {
		return errors.New("delivery digest dispatch payload message is empty")
	}

	if len(message) > maxDeliveryDigestMessageBytes {
		return fmt.Errorf("delivery digest dispatch payload message is too long: %d > %d bytes", len(message), maxDeliveryDigestMessageBytes)
	}

	return nil
}

func (p *DeliveryDigestDispatchPayload) Identity() string {
	if p == nil || !p.Kind.IsValid() || strings.TrimSpace(p.PeriodKey) == "" {
		return ""
	}

	sum := sha256.Sum256([]byte(string(p.Kind) + "\x00" + strings.TrimSpace(p.PeriodKey)))

	return "sha256:" + hex.EncodeToString(sum[:])
}

func (p *DeliveryDigestDispatchPayload) ContentIdentity() string {
	if p == nil {
		return ""
	}

	identity := p.Identity()
	if identity == "" {
		return ""
	}

	message := strings.TrimSpace(p.PreRenderedMessage)
	if message == "" {
		return ""
	}

	messageHash := sha256.Sum256([]byte(message))

	return identity + ":message_sha256:" + hex.EncodeToString(messageHash[:])
}
