package latencycause

import (
	"strings"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox"
)

var (
	internalDelaySourceBasis = map[outbox.PostDelaySource]string{
		outbox.PostDelaySourceInternalDelivery: "delay_source=internal_delivery",
		outbox.PostDelaySourceMixed:            "delay_source=mixed",
	}

	delaySourceFields = map[outbox.PostDelaySource][]string{
		outbox.PostDelaySourceExternalCollection: {"publish_to_detect_millis"},
		outbox.PostDelaySourceInternalDelivery:   {"internal_latency_millis"},
		outbox.PostDelaySourceMixed:              {"publish_to_detect_millis", "internal_latency_millis"},
	}

	internalCauseFields = map[outbox.PostInternalDelayCause]string{
		outbox.PostInternalDelayCauseQueueWait:         "queue_wait_millis",
		outbox.PostInternalDelayCauseRetryAccumulation: "retry_accumulation_millis",
		outbox.PostInternalDelayCauseJobFailure:        "job_failure_detected",
	}

	evidenceKeyFields = map[outbox.PostLatencyClassificationEvidenceKey]string{
		outbox.PostLatencyClassificationEvidenceKeyAlarmLatency:      "alarm_latency_millis",
		outbox.PostLatencyClassificationEvidenceKeyPublishToDetect:   "publish_to_detect_millis",
		outbox.PostLatencyClassificationEvidenceKeyInternalLatency:   "internal_latency_millis",
		outbox.PostLatencyClassificationEvidenceKeyQueueWait:         "queue_wait_millis",
		outbox.PostLatencyClassificationEvidenceKeyRetryAccumulation: "retry_accumulation_millis",
		outbox.PostLatencyClassificationEvidenceKeyJobFailure:        "job_failure_detected",
	}
)

func classifyInternalJudgment(row Row) (InternalCauseJudgment, string) {
	if row.DelaySource == outbox.PostDelaySourceExternalCollection {
		return InternalCauseJudgmentNonInternal, "delay_source=external_collection"
	}

	if hasInternalDelayCause(row.InternalDelayCause) {
		return InternalCauseJudgmentInternalSystem, buildInternalCauseBasis(row.DelaySource, row.InternalDelayCause)
	}

	if basis, ok := internalDelaySourceBasis[row.DelaySource]; ok {
		return InternalCauseJudgmentInternalSystem, basis
	}

	if row.LatencyClassification.Status == outbox.PostLatencyClassificationStatusInsufficientEvidence {
		return InternalCauseJudgmentNonInternal, "latency_classification=insufficient_evidence"
	}

	return InternalCauseJudgmentNonInternal, "delay_source=none"
}

func hasInternalDelayCause(cause outbox.PostInternalDelayCause) bool {
	return cause != "" && cause != outbox.PostInternalDelayCauseNone
}

func buildInternalCauseBasis(
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

func buildEvidence(row Row) Evidence {
	evidence := Evidence{}
	seenFields := make(map[string]struct{}, 6)

	addEvidenceField(&evidence, seenFields, "delay_source")
	addEvidenceField(&evidence, seenFields, "internal_delay_cause")
	addEvidenceFields(&evidence, seenFields, delaySourceFields[row.DelaySource])
	addEvidenceField(&evidence, seenFields, internalCauseFields[row.InternalDelayCause])

	if row.LatencyClassification.Status == outbox.PostLatencyClassificationStatusInsufficientEvidence {
		addEvidenceField(&evidence, seenFields, "latency_classification.status")
	}

	if addSelectedEvidenceFields(&evidence, seenFields, row.LatencyClassification.Evidence) {
		addEvidenceField(&evidence, seenFields, "latency_classification.evidence")
	}

	return evidence
}

func addEvidenceField(evidence *Evidence, seenFields map[string]struct{}, field string) {
	if field == "" {
		return
	}
	if _, exists := seenFields[field]; exists {
		return
	}
	seenFields[field] = struct{}{}
	evidence.Fields = append(evidence.Fields, field)
}

func addEvidenceFields(evidence *Evidence, seenFields map[string]struct{}, fields []string) {
	for _, field := range fields {
		addEvidenceField(evidence, seenFields, field)
	}
}

func addSelectedEvidenceFields(
	evidence *Evidence,
	seenFields map[string]struct{},
	items []outbox.PostLatencyClassificationEvidence,
) bool {
	for i := range items {
		if !items[i].Selected {
			continue
		}
		evidence.SelectedClassificationKeys = append(evidence.SelectedClassificationKeys, items[i].Key)
		addEvidenceField(evidence, seenFields, evidenceKeyFields[items[i].Key])
	}
	return len(evidence.SelectedClassificationKeys) > 0
}
