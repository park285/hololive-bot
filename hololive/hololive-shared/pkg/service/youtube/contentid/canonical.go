package contentid

import (
	"fmt"
	"regexp"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	shortPrefix     = "short:"
	communityPrefix = "community:"
)

var communityPostURLPattern = regexp.MustCompile(`(?:^|/)post/([^"?#&/]+)`)

func ForShort(videoID string) (string, error) {
	normalized, err := NormalizeShortVideoID(videoID)
	if err != nil {
		return "", err
	}

	return shortPrefix + normalized, nil
}

func ForCommunity(postID string) (string, error) {
	normalized, err := NormalizeCommunityPostID(postID)
	if err != nil {
		return "", err
	}

	return communityPrefix + normalized, nil
}

func ForOutboxKind(kind domain.OutboxKind, resourceID string) (string, error) {
	switch kind {
	case domain.OutboxKindNewShort:
		return ForShort(resourceID)
	case domain.OutboxKindCommunityPost:
		return ForCommunity(resourceID)
	default:
		return "", fmt.Errorf("canonical youtube content id: unsupported outbox kind %s", kind)
	}
}

func NormalizeShortVideoID(raw string) (string, error) {
	return normalizeForShort(raw)
}

func NormalizeCommunityPostID(raw string) (string, error) {
	return normalizeForCommunity(raw)
}

func normalizeForShort(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("canonical youtube content id: short video id is empty")
	}

	if rest, ok := strings.CutPrefix(value, shortPrefix); ok {
		value = strings.TrimSpace(rest)
	} else if strings.HasPrefix(value, communityPrefix) {
		return "", fmt.Errorf("canonical youtube content id: short video id prefix mismatch: %s", value)
	}

	if value == "" {
		return "", fmt.Errorf("canonical youtube content id: short video id is empty")
	}
	if hasKnownPrefix(value) {
		return "", fmt.Errorf("canonical youtube content id: short video id prefix mismatch: %s", value)
	}

	return value, nil
}

func normalizeForCommunity(raw string) (string, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return "", fmt.Errorf("canonical youtube content id: community post id is empty")
	}

	if rest, ok := strings.CutPrefix(value, communityPrefix); ok {
		value = strings.TrimSpace(rest)
	} else if strings.HasPrefix(value, shortPrefix) {
		return "", fmt.Errorf("canonical youtube content id: community post id prefix mismatch: %s", value)
	}

	value = normalizeCommunityCandidate(value)
	if value == "" {
		return "", fmt.Errorf("canonical youtube content id: community post id is empty")
	}
	if hasKnownPrefix(value) {
		return "", fmt.Errorf("canonical youtube content id: community post id prefix mismatch: %s", value)
	}

	return value, nil
}

func normalizeCommunityCandidate(value string) string {
	normalized := strings.TrimSpace(strings.ReplaceAll(value, `\/`, `/`))
	if normalized == "" {
		return ""
	}

	if matches := communityPostURLPattern.FindStringSubmatch(normalized); len(matches) == 2 {
		return strings.TrimSpace(matches[1])
	}

	return normalized
}

func hasKnownPrefix(value string) bool {
	return strings.HasPrefix(value, shortPrefix) || strings.HasPrefix(value, communityPrefix)
}
