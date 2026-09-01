// Package contentid owns the canonical logical identity used by every YouTube
// egress runtime.
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
	// MaxLogicalIDLength matches youtube_notification_delivery_ledger.logical_id.
	MaxLogicalIDLength = 50
	// MaxRoomIDLength matches youtube_notification_delivery_ledger.room_id.
	MaxRoomIDLength = 100

	shortPrefix     = "short:"
	communityPrefix = "community:"
)

// ErrorReason classifies canonical identity validation failures.
type ErrorReason string

const (
	ErrorReasonEmpty           ErrorReason = "empty"
	ErrorReasonTooLong         ErrorReason = "too_long"
	ErrorReasonPrefixMismatch  ErrorReason = "prefix_mismatch"
	ErrorReasonUnsupportedKind ErrorReason = "unsupported_kind"
	ErrorReasonInvalidPayload  ErrorReason = "invalid_payload"
	ErrorReasonMismatch        ErrorReason = "mismatch"
)

// Error is a typed, value-redacted canonical identity failure.
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

// LogicalKey is the schema-bounded, canonical delivery identity.
type LogicalKey struct {
	Kind      domain.OutboxKind
	LogicalID string
	RoomID    string
}

// Hash returns a bounded, non-reversible key suitable for logs and metrics.
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

// ResolveDeliveryKey derives a logical key from an outbox row and validates
// Community/Shorts payload identity before any provider call.
func ResolveDeliveryKey(kind domain.OutboxKind, contentID, payload, roomID string) (LogicalKey, error) {
	if kind != domain.OutboxKindNewShort && kind != domain.OutboxKindCommunityPost {
		key, err := ResolveLogicalKey(kind, contentID, roomID)
		if err != nil {
			return LogicalKey{}, fmt.Errorf("resolve delivery key: %w", err)
		}

		return key, nil
	}

	payloadIdentity, err := parseNotificationPayloadIdentity(kind, payload)
	if err != nil {
		return LogicalKey{}, fmt.Errorf("resolve delivery payload identity: %w", err)
	}

	contentLogicalID, err := ForOutboxKind(kind, contentID)
	if err != nil {
		return LogicalKey{}, fmt.Errorf("resolve outbox content id: %w", err)
	}

	canonicalPostID, err := ForOutboxKind(kind, payloadIdentity.CanonicalPostID)
	if err != nil {
		return LogicalKey{}, fmt.Errorf("resolve payload canonical post id: %w", err)
	}

	if contentLogicalID != canonicalPostID {
		return LogicalKey{}, &Error{Kind: kind, Field: "payload identity", Reason: ErrorReasonMismatch}
	}

	key, err := ResolveLogicalKey(kind, canonicalPostID, roomID)
	if err != nil {
		return LogicalKey{}, fmt.Errorf("resolve canonical delivery key: %w", err)
	}

	return key, nil
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

// ResolveLogicalKey canonicalizes and validates a ledger primary key.
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

// ForOutboxKind returns the canonical logical ID for every supported outbox kind.
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
	case domain.OutboxKindNewVideo, domain.OutboxKindLiveStream, domain.OutboxKindMilestone:
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
