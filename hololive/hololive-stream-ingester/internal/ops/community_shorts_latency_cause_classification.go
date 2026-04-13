package ops

import (
	"strings"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

func classifyCommunityShortsLatencyCauseInternalJudgment(
	row CommunityShortsLatencyCauseRow,
) (CommunityShortsInternalCauseJudgment, string) {
	if row.DelaySource == outbox.PostDelaySourceExternalCollection {
		return CommunityShortsInternalCauseJudgmentNonInternal, "delay_source=external_collection"
	}

	if row.InternalDelayCause != "" && row.InternalDelayCause != outbox.PostInternalDelayCauseNone {
		return CommunityShortsInternalCauseJudgmentInternalSystem, buildCommunityShortsInternalCauseBasis(row.DelaySource, row.InternalDelayCause)
	}

	switch row.DelaySource {
	case outbox.PostDelaySourceInternalDelivery:
		return CommunityShortsInternalCauseJudgmentInternalSystem, "delay_source=internal_delivery"
	case outbox.PostDelaySourceMixed:
		return CommunityShortsInternalCauseJudgmentInternalSystem, "delay_source=mixed"
	}

	if row.LatencyClassification.Status == outbox.PostLatencyClassificationStatusInsufficientEvidence {
		return CommunityShortsInternalCauseJudgmentNonInternal, "latency_classification=insufficient_evidence"
	}

	return CommunityShortsInternalCauseJudgmentNonInternal, "delay_source=none"
}

func buildCommunityShortsInternalCauseBasis(
	delaySource outbox.PostDelaySource,
	cause outbox.PostInternalDelayCause,
) string {
	parts := make([]string, 0, 2)
	if delaySource != "" && delaySource != outbox.PostDelaySourceNone {
		parts = append(parts, "delay_source="+string(delaySource))
	}
	if cause != "" && cause != outbox.PostInternalDelayCauseNone {
		parts = append(parts, "internal_delay_cause="+string(cause))
	}
	if len(parts) == 0 {
		return "internal_delay_cause=none"
	}
	return strings.Join(parts, ",")
}

func buildCommunityShortsLatencyCauseEvidence(row CommunityShortsLatencyCauseRow) CommunityShortsLatencyCauseEvidence {
	evidence := CommunityShortsLatencyCauseEvidence{}
	seenFields := make(map[string]struct{}, 6)
	addField := func(field string) {
		if field == "" {
			return
		}
		if _, exists := seenFields[field]; exists {
			return
		}
		seenFields[field] = struct{}{}
		evidence.Fields = append(evidence.Fields, field)
	}

	addField("delay_source")
	addField("internal_delay_cause")

	switch row.DelaySource {
	case outbox.PostDelaySourceExternalCollection:
		addField("publish_to_detect_millis")
	case outbox.PostDelaySourceInternalDelivery:
		addField("internal_latency_millis")
	case outbox.PostDelaySourceMixed:
		addField("publish_to_detect_millis")
		addField("internal_latency_millis")
	}

	switch row.InternalDelayCause {
	case outbox.PostInternalDelayCauseQueueWait:
		addField("queue_wait_millis")
	case outbox.PostInternalDelayCauseRetryAccumulation:
		addField("retry_accumulation_millis")
	case outbox.PostInternalDelayCauseJobFailure:
		addField("job_failure_detected")
	}

	if row.LatencyClassification.Status == outbox.PostLatencyClassificationStatusInsufficientEvidence {
		addField("latency_classification.status")
	}

	for i := range row.LatencyClassification.Evidence {
		item := row.LatencyClassification.Evidence[i]
		if !item.Selected {
			continue
		}
		evidence.SelectedClassificationKeys = append(evidence.SelectedClassificationKeys, item.Key)
		addField(communityShortsLatencyCauseEvidenceFieldForKey(item.Key))
	}
	if len(evidence.SelectedClassificationKeys) > 0 {
		addField("latency_classification.evidence")
	}

	return evidence
}

func communityShortsLatencyCauseEvidenceFieldForKey(key outbox.PostLatencyClassificationEvidenceKey) string {
	switch key {
	case outbox.PostLatencyClassificationEvidenceKeyAlarmLatency:
		return "alarm_latency_millis"
	case outbox.PostLatencyClassificationEvidenceKeyPublishToDetect:
		return "publish_to_detect_millis"
	case outbox.PostLatencyClassificationEvidenceKeyInternalLatency:
		return "internal_latency_millis"
	case outbox.PostLatencyClassificationEvidenceKeyQueueWait:
		return "queue_wait_millis"
	case outbox.PostLatencyClassificationEvidenceKeyRetryAccumulation:
		return "retry_accumulation_millis"
	case outbox.PostLatencyClassificationEvidenceKeyJobFailure:
		return "job_failure_detected"
	default:
		return ""
	}
}
