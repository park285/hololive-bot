package summarizer

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"sort"

	json "github.com/park285/llm-kakao-bots/shared-go/pkg/json"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func buildSummaryCacheKey(events []domain.MajorEvent, summaryType SummaryType, periodKey string) string {
	return fmt.Sprintf(
		"majorevent:summary:%s:%s:%s:%s",
		promptVersion,
		summaryType,
		periodKey,
		buildSummaryInputHash(events),
	)
}

func buildSummaryInputHash(events []domain.MajorEvent) string {
	if len(events) == 0 {
		return "empty"
	}

	projected := projectPromptEvents(events)
	sort.Slice(projected, func(i, j int) bool {
		if projected[i].DateStr != projected[j].DateStr {
			return projected[i].DateStr < projected[j].DateStr
		}
		if projected[i].Title != projected[j].Title {
			return projected[i].Title < projected[j].Title
		}
		if projected[i].Link != projected[j].Link {
			return projected[i].Link < projected[j].Link
		}
		if projected[i].EventType != projected[j].EventType {
			return projected[i].EventType < projected[j].EventType
		}
		return projected[i].Members < projected[j].Members
	})

	payload, err := json.Marshal(projected)
	if err != nil {
		return "marshal-error"
	}
	checksum := sha256.Sum256(payload)
	return hex.EncodeToString(checksum[:8])
}
