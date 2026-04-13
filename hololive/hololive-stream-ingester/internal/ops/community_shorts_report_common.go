package ops

import (
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

func normalizeCommunityShortsSendCountTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.UTC()
}

func cloneCommunityShortsSendCountTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := normalizeCommunityShortsSendCountTime(*value)
	if normalized.IsZero() {
		return nil
	}
	return &normalized
}

func normalizeCommunityShortsSendCountTimePtrValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return normalizeCommunityShortsSendCountTime(*value)
}

func cloneCommunityShortsSendCountInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func formatCommunityShortsSendCountTime(value time.Time) string {
	if value.IsZero() {
		return "(none)"
	}
	return value.UTC().Format(time.RFC3339)
}

func formatCommunityShortsSendCountTimePtr(value *time.Time) string {
	if value == nil {
		return "(none)"
	}
	return formatCommunityShortsSendCountTime(*value)
}

func formatCommunityShortsSendCountInt64Ptr(value *int64) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func formatCommunityShortsSendCountFloat64Ptr(value *float64) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%.3f", *value)
}

func formatCommunityShortsSendCountBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func cloneCommunityShortsLatencyClassification(result outbox.PostLatencyClassificationResult) outbox.PostLatencyClassificationResult {
	cloned := outbox.PostLatencyClassificationResult{
		Status:             result.Status,
		ThresholdMillis:    result.ThresholdMillis,
		DelaySource:        result.DelaySource,
		InternalDelayCause: result.InternalDelayCause,
	}
	if len(result.Evidence) == 0 {
		return cloned
	}
	cloned.Evidence = make([]outbox.PostLatencyClassificationEvidence, 0, len(result.Evidence))
	for i := range result.Evidence {
		item := outbox.PostLatencyClassificationEvidence{
			Key:      result.Evidence[i].Key,
			Millis:   cloneCommunityShortsSendCountInt64(result.Evidence[i].Millis),
			Selected: result.Evidence[i].Selected,
		}
		if result.Evidence[i].Bool != nil {
			flag := *result.Evidence[i].Bool
			item.Bool = &flag
		}
		cloned.Evidence = append(cloned.Evidence, item)
	}
	return cloned
}

func renderCommunityShortsLatencyClassificationEvidence(result outbox.PostLatencyClassificationResult) string {
	if len(result.Evidence) == 0 {
		return "(none)"
	}

	parts := make([]string, 0, len(result.Evidence))
	for i := range result.Evidence {
		parts = append(parts, formatCommunityShortsLatencyClassificationEvidenceItem(result.Evidence[i]))
	}
	return strings.Join(parts, "; ")
}

func formatCommunityShortsLatencyClassificationEvidenceItem(item outbox.PostLatencyClassificationEvidence) string {
	value := "(none)"
	if item.Millis != nil {
		value = fmt.Sprintf("%d", *item.Millis)
	} else if item.Bool != nil {
		value = formatCommunityShortsSendCountBool(*item.Bool)
	}
	if item.Selected {
		value += "[selected]"
	}
	return fmt.Sprintf("%s=%s", item.Key, value)
}

func fallbackCommunityShortsSendCountValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return "(none)"
	}
	return trimmed
}
