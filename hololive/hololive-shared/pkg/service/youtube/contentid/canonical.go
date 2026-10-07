// Package contentid는 모든 YouTube 발송 런타임이 공유하는 정본 논리 식별자를 소유한다.
package contentid

import (
	"crypto/sha256"
	"encoding/hex"
	jsonv2 "encoding/json/v2"
	"fmt"
	"regexp"
	"strings"
	"unicode/utf8"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	// MaxLogicalIDLength는 youtube_notification_delivery_ledger.logical_id의 길이 상한이다.
	MaxLogicalIDLength = 50
	// MaxRoomIDLength는 youtube_notification_delivery_ledger.room_id의 길이 상한이다.
	MaxRoomIDLength = 100

	shortPrefix     = "short:"
	communityPrefix = "community:"
)

// ErrorReason은 정본 식별자 검증 실패의 분류다.
type ErrorReason string

const (
	ErrorReasonEmpty           ErrorReason = "empty"
	ErrorReasonTooLong         ErrorReason = "too_long"
	ErrorReasonPrefixMismatch  ErrorReason = "prefix_mismatch"
	ErrorReasonUnsupportedKind ErrorReason = "unsupported_kind"
	ErrorReasonInvalidPayload  ErrorReason = "invalid_payload"
	ErrorReasonMismatch        ErrorReason = "mismatch"
)

// Error는 원본 값을 노출하지 않는 정본 식별자 오류다.
type Error struct {
	Kind   domain.OutboxKind
	Field  string
	Reason ErrorReason
	Cause  error
}

func (e *Error) Error() string {
	switch e.Reason {
	case ErrorReasonEmpty:
		return fmt.Sprintf("canonical youtube content id: %s is empty", e.Field)
	case ErrorReasonTooLong:
		return fmt.Sprintf("canonical youtube content id: %s is too long", e.Field)
	case ErrorReasonPrefixMismatch:
		return fmt.Sprintf("canonical youtube content id: %s prefix mismatch", e.Field)
	case ErrorReasonUnsupportedKind:
		return fmt.Sprintf("canonical youtube content id: unsupported outbox kind %s", e.Kind)
	case ErrorReasonInvalidPayload:
		return "canonical youtube content id: payload is invalid"
	case ErrorReasonMismatch:
		return fmt.Sprintf("canonical youtube content id: %s mismatch", e.Field)
	default:
		return "canonical youtube content id: invalid identity"
	}
}

func (e *Error) Unwrap() error {
	return e.Cause
}

// LogicalKey는 스키마 상한을 적용한 정본 전송 식별자다.
type LogicalKey struct {
	Kind      domain.OutboxKind
	LogicalID string
	RoomID    string
}

// Hash는 로그·메트릭에 사용할 고정 길이 단방향 키를 반환한다.
func (k LogicalKey) Hash() string {
	sum := sha256.Sum256([]byte(string(k.Kind) + "\x00" + k.LogicalID + "\x00" + k.RoomID))

	return hex.EncodeToString(sum[:16])
}

var communityPostURLPattern = regexp.MustCompile(`(?:^|/)post/([^"?#&/]+)`)

type notificationPayloadIdentity struct {
	CanonicalPostID string `json:"canonical_post_id"`
	PostID          string `json:"post_id"`
	VideoID         string `json:"video_id"`
}

// ResolveDeliveryKey는 outbox 행에서 논리 키를 구하고 제공자 호출 전에 Community·Shorts 식별자를 검증한다.
func ResolveDeliveryKey(kind domain.OutboxKind, contentID, payload, roomID string) (LogicalKey, error) {
	logicalID, err := ResolveDeliveryLogicalID(kind, contentID, payload)
	if err != nil {
		return LogicalKey{}, fmt.Errorf("resolve delivery key: %w", err)
	}

	key, err := ResolveLogicalKey(kind, logicalID, roomID)
	if err != nil {
		return LogicalKey{}, fmt.Errorf("resolve canonical delivery key: %w", err)
	}

	return key, nil
}

// ResolveDeliveryLogicalID는 방과 독립적인 outbox의 정본 논리 ID를 반환한다.
// Community·Shorts는 content_id와 일치하는 필수 canonical_post_id를, 나머지는 content_id를 사용한다.
// 리소스 ID로 대체하지 않으며 식별자가 없거나 잘못되면 실패한다.
func ResolveDeliveryLogicalID(kind domain.OutboxKind, contentID, payload string) (string, error) {
	if kind != domain.OutboxKindNewShort && kind != domain.OutboxKindCommunityPost {
		logicalID, err := ForOutboxKind(kind, contentID)
		if err != nil {
			return "", fmt.Errorf("resolve outbox content id: %w", err)
		}

		return logicalID, nil
	}

	payloadIdentity, err := parseNotificationPayloadIdentity(kind, payload)
	if err != nil {
		return "", fmt.Errorf("resolve delivery payload identity: %w", err)
	}

	contentLogicalID, err := ForOutboxKind(kind, contentID)
	if err != nil {
		return "", fmt.Errorf("resolve outbox content id: %w", err)
	}

	canonicalPostID, err := ForOutboxKind(kind, payloadIdentity.CanonicalPostID)
	if err != nil {
		return "", fmt.Errorf("resolve payload canonical post id: %w", err)
	}

	if contentLogicalID != canonicalPostID {
		return "", &Error{Kind: kind, Field: "payload identity", Reason: ErrorReasonMismatch}
	}

	return canonicalPostID, nil
}

func parseNotificationPayloadIdentity(kind domain.OutboxKind, payload string) (notificationPayloadIdentity, error) {
	if strings.TrimSpace(payload) == "" {
		return notificationPayloadIdentity{}, &Error{Kind: kind, Field: "payload", Reason: ErrorReasonEmpty}
	}

	var identity notificationPayloadIdentity

	if err := jsonv2.Unmarshal([]byte(payload), &identity); err != nil {
		return notificationPayloadIdentity{}, &Error{
			Kind: kind, Field: "payload", Reason: ErrorReasonInvalidPayload, Cause: err,
		}
	}

	return identity, nil
}

// ResolveLogicalKey는 ledger 기본 키를 정규화하고 검증한다.
func ResolveLogicalKey(kind domain.OutboxKind, resourceID, roomID string) (LogicalKey, error) {
	logicalID, err := ForOutboxKind(kind, resourceID)
	if err != nil {
		return LogicalKey{}, fmt.Errorf("resolve logical id: %w", err)
	}

	normalizedRoomID := strings.TrimSpace(roomID)
	if err := validateBounded(kind, "room id", normalizedRoomID, MaxRoomIDLength); err != nil {
		return LogicalKey{}, fmt.Errorf("resolve room id: %w", err)
	}

	return LogicalKey{Kind: kind, LogicalID: logicalID, RoomID: normalizedRoomID}, nil
}

func ForShort(videoID string) (string, error) {
	normalized, err := NormalizeShortVideoID(videoID)
	if err != nil {
		return "", fmt.Errorf("normalize short video ID: %w", err)
	}

	logicalID := shortPrefix + normalized
	if err := validateBounded(domain.OutboxKindNewShort, "logical id", logicalID, MaxLogicalIDLength); err != nil {
		return "", fmt.Errorf("validate short logical ID: %w", err)
	}

	return logicalID, nil
}

func ForCommunity(postID string) (string, error) {
	normalized, err := NormalizeCommunityPostID(postID)
	if err != nil {
		return "", fmt.Errorf("normalize community post ID: %w", err)
	}

	logicalID := communityPrefix + normalized
	if err := validateBounded(domain.OutboxKindCommunityPost, "logical id", logicalID, MaxLogicalIDLength); err != nil {
		return "", fmt.Errorf("validate community logical ID: %w", err)
	}

	return logicalID, nil
}

// ForOutboxKind는 지원하는 outbox 종류의 정본 논리 ID를 반환한다.
func ForOutboxKind(kind domain.OutboxKind, resourceID string) (string, error) {
	switch kind {
	case domain.OutboxKindNewShort:
		logicalID, err := ForShort(resourceID)
		if err != nil {
			return "", fmt.Errorf("canonicalize short outbox ID: %w", err)
		}

		return logicalID, nil
	case domain.OutboxKindCommunityPost:
		logicalID, err := ForCommunity(resourceID)
		if err != nil {
			return "", fmt.Errorf("canonicalize community outbox ID: %w", err)
		}

		return logicalID, nil
	case domain.OutboxKindNewVideo, domain.OutboxKindLiveStream:
		logicalID := strings.TrimSpace(resourceID)
		if err := validateBounded(kind, "logical id", logicalID, MaxLogicalIDLength); err != nil {
			return "", fmt.Errorf("validate outbox logical ID: %w", err)
		}

		return logicalID, nil
	default:
		return "", &Error{Kind: kind, Field: "kind", Reason: ErrorReasonUnsupportedKind}
	}
}

func NormalizeShortVideoID(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", &Error{Kind: domain.OutboxKindNewShort, Field: "short video id", Reason: ErrorReasonEmpty}
	}

	if rest, ok := strings.CutPrefix(value, shortPrefix); ok {
		value = strings.TrimSpace(rest)
	} else if strings.HasPrefix(value, communityPrefix) {
		return "", &Error{Kind: domain.OutboxKindNewShort, Field: "short video id", Reason: ErrorReasonPrefixMismatch}
	}

	if value == "" {
		return "", &Error{Kind: domain.OutboxKindNewShort, Field: "short video id", Reason: ErrorReasonEmpty}
	}

	if hasKnownPrefix(value) {
		return "", &Error{Kind: domain.OutboxKindNewShort, Field: "short video id", Reason: ErrorReasonPrefixMismatch}
	}

	return value, nil
}

func NormalizeCommunityPostID(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", &Error{Kind: domain.OutboxKindCommunityPost, Field: "community post id", Reason: ErrorReasonEmpty}
	}

	if rest, ok := strings.CutPrefix(value, communityPrefix); ok {
		value = strings.TrimSpace(rest)
	} else if strings.HasPrefix(value, shortPrefix) {
		return "", &Error{Kind: domain.OutboxKindCommunityPost, Field: "community post id", Reason: ErrorReasonPrefixMismatch}
	}

	value = normalizeCommunityCandidate(value)
	if value == "" {
		return "", &Error{Kind: domain.OutboxKindCommunityPost, Field: "community post id", Reason: ErrorReasonEmpty}
	}

	if hasKnownPrefix(value) {
		return "", &Error{Kind: domain.OutboxKindCommunityPost, Field: "community post id", Reason: ErrorReasonPrefixMismatch}
	}

	return value, nil
}

func normalizeCommunityCandidate(value string) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(value, `\/`, "/"))
	if normalized == "" {
		return ""
	}

	if matches := communityPostURLPattern.FindStringSubmatch(normalized); len(matches) == 2 {
		return strings.TrimSpace(matches[1])
	}

	return normalized
}

func validateBounded(kind domain.OutboxKind, field, value string, maxLength int) error {
	if value == "" {
		return &Error{Kind: kind, Field: field, Reason: ErrorReasonEmpty}
	}

	if utf8.RuneCountInString(value) > maxLength {
		return &Error{Kind: kind, Field: field, Reason: ErrorReasonTooLong}
	}

	return nil
}

func hasKnownPrefix(value string) bool {
	return strings.HasPrefix(value, shortPrefix) || strings.HasPrefix(value, communityPrefix)
}
