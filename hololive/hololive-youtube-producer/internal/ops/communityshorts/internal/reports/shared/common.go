package shared

import (
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

const NoneValue = "(none)"

func NormalizeSendCountTime(value time.Time) time.Time {
	if value.IsZero() {
		return time.Time{}
	}
	return value.UTC()
}

func CloneSendCountTime(value *time.Time) *time.Time {
	if value == nil {
		return nil
	}
	normalized := NormalizeSendCountTime(*value)
	if normalized.IsZero() {
		return nil
	}
	return &normalized
}

func NormalizeSendCountTimePtrValue(value *time.Time) time.Time {
	if value == nil {
		return time.Time{}
	}
	return NormalizeSendCountTime(*value)
}

func CloneSendCountInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}

func FormatSendCountTime(value time.Time) string {
	if value.IsZero() {
		return NoneValue
	}
	return value.UTC().Format(time.RFC3339)
}

func FormatSendCountTimePtr(value *time.Time) string {
	if value == nil {
		return NoneValue
	}
	return FormatSendCountTime(*value)
}

func FormatSendCountInt64Ptr(value *int64) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%d", *value)
}

func FormatSendCountFloat64Ptr(value *float64) string {
	if value == nil {
		return ""
	}
	return fmt.Sprintf("%.3f", *value)
}

func FormatSendCountBool(value bool) string {
	if value {
		return "true"
	}
	return "false"
}

func CloneLatencyClassification(result *outbox.PostLatencyClassificationResult) outbox.PostLatencyClassificationResult {
	if result == nil {
		return outbox.PostLatencyClassificationResult{}
	}
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
			Millis:   CloneSendCountInt64(result.Evidence[i].Millis),
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

func RenderLatencyClassificationEvidence(result *outbox.PostLatencyClassificationResult) string {
	if result == nil {
		return NoneValue
	}
	if len(result.Evidence) == 0 {
		return NoneValue
	}

	parts := make([]string, 0, len(result.Evidence))
	for i := range result.Evidence {
		parts = append(parts, FormatLatencyClassificationEvidenceItem(result.Evidence[i]))
	}
	return strings.Join(parts, "; ")
}

func FormatLatencyClassificationEvidenceItem(item outbox.PostLatencyClassificationEvidence) string {
	value := NoneValue
	if item.Millis != nil {
		value = fmt.Sprintf("%d", *item.Millis)
	} else if item.Bool != nil {
		value = FormatSendCountBool(*item.Bool)
	}
	if item.Selected {
		value += "[selected]"
	}
	return fmt.Sprintf("%s=%s", item.Key, value)
}

func FallbackSendCountValue(value string) string {
	trimmed := strings.TrimSpace(value)
	if trimmed == "" {
		return NoneValue
	}
	return trimmed
}
