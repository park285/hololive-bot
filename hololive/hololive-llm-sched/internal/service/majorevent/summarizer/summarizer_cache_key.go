package summarizer

import (
	"cmp"
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"slices"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func buildSummaryCacheKey(events []domain.MajorEvent, summaryType SummaryType, periodKey string) (string, error) {
	inputHash, err := buildSummaryInputHash(events)
	if err != nil {
		return "", err
	}

	return fmt.Sprintf(
		"majorevent:summary:%s:%s:%s:%s",
		promptVersion,
		summaryType,
		periodKey,
		inputHash,
	), nil
}

func buildSummaryInputHash(events []domain.MajorEvent) (string, error) {
	if len(events) == 0 {
		return "empty", nil
	}

	projected := projectPromptEvents(events)
	slices.SortFunc(projected, func(a, b eventForPrompt) int {
		if byDate := cmp.Compare(a.DateStr, b.DateStr); byDate != 0 {
			return byDate
		}
		if byTitle := cmp.Compare(a.Title, b.Title); byTitle != 0 {
			return byTitle
		}
		if byLink := cmp.Compare(a.Link, b.Link); byLink != 0 {
			return byLink
		}
		if byEventType := cmp.Compare(a.EventType, b.EventType); byEventType != 0 {
			return byEventType
		}
		return cmp.Compare(a.Members, b.Members)
	})

	payload, err := json.Marshal(projected)
	if err != nil {
		return "", fmt.Errorf("marshal summary cache input: %w", err)
	}
	checksum := sha256.Sum256(payload)
	return hex.EncodeToString(checksum[:8]), nil
}
