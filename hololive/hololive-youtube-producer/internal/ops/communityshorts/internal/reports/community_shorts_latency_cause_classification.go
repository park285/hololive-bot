package reports

import (
	"strings"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

var (
	communityShortsInternalDelaySourceBasis = map[outbox.PostDelaySource]string{
		outbox.PostDelaySourceInternalDelivery: "delay_source=internal_delivery",
		outbox.PostDelaySourceMixed:            "delay_source=mixed",
	}

	communityShortsLatencyCauseDelaySourceFields = map[outbox.PostDelaySource][]string{
		outbox.PostDelaySourceExternalCollection: {"publish_to_detect_millis"},
		outbox.PostDelaySourceInternalDelivery:   {"internal_latency_millis"},
		outbox.PostDelaySourceMixed:              {"publish_to_detect_millis", "internal_latency_millis"},
	}

	communityShortsLatencyCauseInternalCauseFields = map[outbox.PostInternalDelayCause]string{
		outbox.PostInternalDelayCauseQueueWait:         "queue_wait_millis",
		outbox.PostInternalDelayCauseRetryAccumulation: "retry_accumulation_millis",
		outbox.PostInternalDelayCauseJobFailure:        "job_failure_detected",
	}

	communityShortsLatencyCauseEvidenceKeyFields = map[outbox.PostLatencyClassificationEvidenceKey]string{
		outbox.PostLatencyClassificationEvidenceKeyAlarmLatency:      "alarm_latency_millis",
		outbox.PostLatencyClassificationEvidenceKeyPublishToDetect:   "publish_to_detect_millis",
		outbox.PostLatencyClassificationEvidenceKeyInternalLatency:   "internal_latency_millis",
		outbox.PostLatencyClassificationEvidenceKeyQueueWait:         "queue_wait_millis",
		outbox.PostLatencyClassificationEvidenceKeyRetryAccumulation: "retry_accumulation_millis",
		outbox.PostLatencyClassificationEvidenceKeyJobFailure:        "job_failure_detected",
	}
)

func classifyCommunityShortsLatencyCauseInternalJudgment(
	row CommunityShortsLatencyCauseRow,
) (CommunityShortsInternalCauseJudgment, string) {
	if row.DelaySource == outbox.PostDelaySourceExternalCollection {
		return CommunityShortsInternalCauseJudgmentNonInternal, "delay_source=external_collection"
	}

	if hasCommunityShortsInternalDelayCause(row.InternalDelayCause) {
		return CommunityShortsInternalCauseJudgmentInternalSystem, buildCommunityShortsInternalCauseBasis(row.DelaySource, row.InternalDelayCause)
	}

	if basis, ok := communityShortsInternalDelaySourceBasis[row.DelaySource]; ok {
		return CommunityShortsInternalCauseJudgmentInternalSystem, basis
	}

	if row.LatencyClassification.Status == outbox.PostLatencyClassificationStatusInsufficientEvidence {
		return CommunityShortsInternalCauseJudgmentNonInternal, "latency_classification=insufficient_evidence"
	}

	return CommunityShortsInternalCauseJudgmentNonInternal, "delay_source=none"
}

func hasCommunityShortsInternalDelayCause(cause outbox.PostInternalDelayCause) bool {
	return cause != "" && cause != outbox.PostInternalDelayCauseNone
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

	addCommunityShortsLatencyCauseEvidenceField(&evidence, seenFields, "delay_source")
	addCommunityShortsLatencyCauseEvidenceField(&evidence, seenFields, "internal_delay_cause")
	addCommunityShortsLatencyCauseEvidenceFields(&evidence, seenFields, communityShortsLatencyCauseDelaySourceFields[row.DelaySource])
	addCommunityShortsLatencyCauseEvidenceField(&evidence, seenFields, communityShortsLatencyCauseInternalCauseFields[row.InternalDelayCause])

	if row.LatencyClassification.Status == outbox.PostLatencyClassificationStatusInsufficientEvidence {
		addCommunityShortsLatencyCauseEvidenceField(&evidence, seenFields, "latency_classification.status")
	}

	if addCommunityShortsLatencyCauseSelectedEvidenceFields(&evidence, seenFields, row.LatencyClassification.Evidence) {
		addCommunityShortsLatencyCauseEvidenceField(&evidence, seenFields, "latency_classification.evidence")
	}

	return evidence
}

func addCommunityShortsLatencyCauseEvidenceField(
	evidence *CommunityShortsLatencyCauseEvidence,
	seenFields map[string]struct{},
	field string,
) {
	if field == "" {
		return
	}
	if _, exists := seenFields[field]; exists {
		return
	}
	seenFields[field] = struct{}{}
	evidence.Fields = append(evidence.Fields, field)
}

func addCommunityShortsLatencyCauseEvidenceFields(
	evidence *CommunityShortsLatencyCauseEvidence,
	seenFields map[string]struct{},
	fields []string,
) {
	for _, field := range fields {
		addCommunityShortsLatencyCauseEvidenceField(evidence, seenFields, field)
	}
}

func addCommunityShortsLatencyCauseSelectedEvidenceFields(
	evidence *CommunityShortsLatencyCauseEvidence,
	seenFields map[string]struct{},
	items []outbox.PostLatencyClassificationEvidence,
) bool {
	for i := range items {
		item := items[i]
		if !item.Selected {
			continue
		}
		evidence.SelectedClassificationKeys = append(evidence.SelectedClassificationKeys, item.Key)
		addCommunityShortsLatencyCauseEvidenceField(evidence, seenFields, communityShortsLatencyCauseEvidenceFieldForKey(item.Key))
	}
	return len(evidence.SelectedClassificationKeys) > 0
}

func communityShortsLatencyCauseEvidenceFieldForKey(key outbox.PostLatencyClassificationEvidenceKey) string {
	return communityShortsLatencyCauseEvidenceKeyFields[key]
}
