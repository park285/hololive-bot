package timeline

import (
	"slices"
	"time"
)

var postLatencyDelaySourceReasonCodes = map[PostDelaySource]PostLatencyReasonCode{
	PostDelaySourceExternalCollection: PostLatencyReasonCodeExternalCollection,
	PostDelaySourceMixed:              PostLatencyReasonCodeMixed,
}

var postLatencyInternalCauseReasonCodes = map[PostInternalDelayCause]PostLatencyReasonCode{
	PostInternalDelayCauseQueueWait:         PostLatencyReasonCodeQueueWait,
	PostInternalDelayCauseRetryAccumulation: PostLatencyReasonCodeRetryAccumulation,
	PostInternalDelayCauseJobFailure:        PostLatencyReasonCodeJobFailure,
}

func DeriveMetrics(row *PostDeliveryTimeline) {
	if row == nil {
		return
	}

	if row.MaxAttemptOrdinal > 1 {
		row.RetryAttemptCount = row.MaxAttemptOrdinal - 1
	}
	row.PublishToDetectMillis = durationMillisBetween(row.ActualPublishedAt, row.DetectedAt)
	row.DetectToQueueMillis = durationMillisBetween(row.DetectedAt, row.QueueEnqueuedAt)
	row.QueueToFirstAttemptMillis = durationMillisBetween(row.QueueEnqueuedAt, row.FirstAttemptStartedAt)
	row.FirstAttemptToFinishMillis = durationMillisBetween(row.FirstAttemptStartedAt, row.FirstAttemptFinishedAt)
	row.FirstAttemptToSuccessMillis = durationMillisBetween(row.FirstAttemptStartedAt, row.FirstSuccessAt)
	row.InternalLatencyMillis = durationMillisBetween(row.DetectedAt, row.AlarmSentAt)
	if row.InternalLatencyMillis != nil {
		row.InternalLatencyExceeded = new(*row.InternalLatencyMillis > PostLatencyExceededThresholdMillis)
	}
	row.DelaySource = classifyDelaySource(row)
	row.QueueWaitMillis = sumDurationMillis(row.DetectToQueueMillis, row.QueueToFirstAttemptMillis)
	row.RetryAccumulationMillis = deriveRetryAccumulationMillis(row)
	row.JobFailureDetected = isJobFailureDetected(row)
	row.InternalDelayCause = classifyPrimaryInternalDelayCause(row)
	row.LatencyClassification = buildPostLatencyClassification(row)
}

func durationMillisBetween(start, end *time.Time) *int64 {
	if start == nil || end == nil {
		return nil
	}

	startUTC := start.UTC()
	endUTC := end.UTC()
	millis := endUTC.Sub(startUTC).Milliseconds()
	return &millis
}

func sumDurationMillis(values ...*int64) *int64 {
	var total int64
	hasValue := false
	for i := range values {
		if values[i] == nil {
			continue
		}
		total += *values[i]
		hasValue = true
	}
	if !hasValue {
		return nil
	}
	return &total
}

func deriveRetryAccumulationMillis(row *PostDeliveryTimeline) *int64 {
	if row == nil || row.FailedAttemptCount <= 0 || row.FirstAttemptFinishedAt == nil {
		return nil
	}

	endAt := resolveRetryAccumulationEnd(row)
	if endAt == nil {
		return nil
	}

	millis := durationMillisBetween(row.FirstAttemptFinishedAt, endAt)
	if millis == nil || *millis <= 0 {
		return nil
	}
	return millis
}

func resolveRetryAccumulationEnd(row *PostDeliveryTimeline) *time.Time {
	for _, candidate := range []*time.Time{row.AlarmSentAt, row.FirstSuccessAt, row.NextRetryAt, row.LastAttemptFinishedAt, row.LastFailureAt} {
		if candidate == nil || candidate.IsZero() {
			continue
		}
		if candidate.UTC().After(row.FirstAttemptFinishedAt.UTC()) {
			resolved := candidate.UTC()
			return &resolved
		}
	}
	return nil
}

func isJobFailureDetected(row *PostDeliveryTimeline) bool {
	if row == nil || row.LastFailureAt == nil {
		return false
	}
	if row.AlarmSentAt != nil || row.FirstSuccessAt != nil || row.LastSuccessAt != nil {
		return false
	}
	return true
}

func classifyDelaySource(row *PostDeliveryTimeline) PostDelaySource {
	if row == nil {
		return PostDelaySourceNone
	}

	externalMillis, hasExternal := positiveDurationMillis(row.PublishToDetectMillis)
	internalMillis, hasInternal := positiveDurationMillis(row.InternalLatencyMillis)

	if postLatencyDelaySourceEligible(row) {
		return selectDominantDelaySource(externalMillis, hasExternal, internalMillis, hasInternal)
	}

	return PostDelaySourceNone
}

func postLatencyDelaySourceEligible(row *PostDeliveryTimeline) bool {
	if row.AlarmLatencyExceeded != nil {
		return *row.AlarmLatencyExceeded || boolPtrTrue(row.InternalLatencyExceeded)
	}
	return boolPtrTrue(row.InternalLatencyExceeded) || postLatencyMillisExceeded(row.PublishToDetectMillis)
}

func positiveDurationMillis(value *int64) (int64, bool) {
	if value == nil || *value <= 0 {
		return 0, false
	}
	return *value, true
}

func selectDominantDelaySource(externalMillis int64, hasExternal bool, internalMillis int64, hasInternal bool) PostDelaySource {
	if !hasExternal && !hasInternal {
		return PostDelaySourceNone
	}
	if !hasExternal {
		return PostDelaySourceInternalDelivery
	}
	if !hasInternal {
		return PostDelaySourceExternalCollection
	}
	if externalMillis == internalMillis {
		return PostDelaySourceMixed
	}
	if externalMillis > internalMillis {
		return PostDelaySourceExternalCollection
	}
	return PostDelaySourceInternalDelivery
}

func classifyPrimaryInternalDelayCause(row *PostDeliveryTimeline) PostInternalDelayCause {
	if row == nil {
		return PostInternalDelayCauseNone
	}

	if row.JobFailureDetected {
		return PostInternalDelayCauseJobFailure
	}

	selected := PostInternalDelayCauseCandidate{Cause: PostInternalDelayCauseNone}
	for _, candidate := range postInternalDelayCauseCandidates(row) {
		selected = selectInternalDelayCauseCandidate(selected, candidate)
	}

	return selected.Cause
}

func postInternalDelayCauseCandidates(row *PostDeliveryTimeline) []PostInternalDelayCauseCandidate {
	return []PostInternalDelayCauseCandidate{
		{
			Cause:     PostInternalDelayCauseRetryAccumulation,
			Millis:    row.RetryAccumulationMillis,
			Priority:  PostInternalDelayCausePriorityRetryAccumulation,
			Available: postLatencyPositiveMillis(row.RetryAccumulationMillis),
		},
		{
			Cause:     PostInternalDelayCauseQueueWait,
			Millis:    row.QueueWaitMillis,
			Priority:  PostInternalDelayCausePriorityQueueWait,
			Available: postLatencyPositiveMillis(row.QueueWaitMillis),
		},
	}
}

func selectInternalDelayCauseCandidate(selected, candidate PostInternalDelayCauseCandidate) PostInternalDelayCauseCandidate {
	if !candidate.Available {
		return selected
	}
	if selected.Cause == PostInternalDelayCauseNone || internalDelayCandidateBeatsSelected(candidate, selected) {
		return candidate
	}
	return selected
}

func internalDelayCandidateBeatsSelected(candidate, selected PostInternalDelayCauseCandidate) bool {
	return *candidate.Millis > *selected.Millis ||
		(*candidate.Millis == *selected.Millis && candidate.Priority > selected.Priority)
}

func BuildPostLatencyClassification(row *PostDeliveryTimeline) PostLatencyClassificationResult {
	return buildPostLatencyClassification(row)
}

func buildPostLatencyClassification(row *PostDeliveryTimeline) PostLatencyClassificationResult {
	delaySource := PostDelaySourceNone
	internalDelayCause := PostInternalDelayCauseNone
	if row != nil {
		if row.DelaySource != "" {
			delaySource = row.DelaySource
		}
		if row.InternalDelayCause != "" {
			internalDelayCause = row.InternalDelayCause
		}
	}

	return PostLatencyClassificationResult{
		Status:             classifyPostLatencyClassificationStatus(row),
		ThresholdMillis:    PostLatencyExceededThresholdMillis,
		DelaySource:        delaySource,
		InternalDelayCause: internalDelayCause,
		Evidence:           buildPostLatencyClassificationEvidence(row),
	}
}

func ClassifyPostLatencyReasonCode(classification *PostLatencyClassificationResult) PostLatencyReasonCode {
	if reasonCode, ok := postLatencyDelaySourceReasonCodes[classification.DelaySource]; ok {
		return reasonCode
	}
	if reasonCode, ok := postLatencyInternalCauseReasonCodes[classification.InternalDelayCause]; ok {
		return reasonCode
	}
	if classification.DelaySource == PostDelaySourceInternalDelivery {
		return PostLatencyReasonCodeInternalDelivery
	}
	if classification.Status == PostLatencyClassificationStatusInsufficientEvidence {
		return PostLatencyReasonCodeInsufficientEvidence
	}
	return PostLatencyReasonCodeNone
}

func classifyPostLatencyClassificationStatus(row *PostDeliveryTimeline) PostLatencyClassificationStatus {
	if row == nil {
		return PostLatencyClassificationStatusInsufficientEvidence
	}
	if row.AlarmLatencyExceeded != nil {
		if *row.AlarmLatencyExceeded {
			return PostLatencyClassificationStatusExceeded
		}
		return PostLatencyClassificationStatusWithinTarget
	}
	if postLatencyDerivedMetricsExceeded(row) {
		return PostLatencyClassificationStatusExceeded
	}
	return PostLatencyClassificationStatusInsufficientEvidence
}

func postLatencyDerivedMetricsExceeded(row *PostDeliveryTimeline) bool {
	if boolPtrTrue(row.InternalLatencyExceeded) {
		return true
	}
	return slices.ContainsFunc([]*int64{row.PublishToDetectMillis, row.QueueWaitMillis, row.RetryAccumulationMillis}, postLatencyMillisExceeded)
}

func boolPtrTrue(value *bool) bool {
	return value != nil && *value
}

func postLatencyPositiveMillis(value *int64) bool {
	return value != nil && *value > 0
}

func postLatencyMillisExceeded(value *int64) bool {
	return value != nil && *value > PostLatencyExceededThresholdMillis
}

func buildPostLatencyClassificationEvidence(row *PostDeliveryTimeline) []PostLatencyClassificationEvidence {
	if row == nil {
		return []PostLatencyClassificationEvidence{}
	}

	selectExternal := row.DelaySource == PostDelaySourceExternalCollection || row.DelaySource == PostDelaySourceMixed
	selectInternal := row.DelaySource == PostDelaySourceInternalDelivery || row.DelaySource == PostDelaySourceMixed
	if row.InternalLatencyExceeded != nil && *row.InternalLatencyExceeded {
		selectInternal = true
	}

	return []PostLatencyClassificationEvidence{
		{
			Key:      PostLatencyClassificationEvidenceKeyAlarmLatency,
			Millis:   clonePostLatencyInt64(row.AlarmLatencyMillis),
			Selected: row.AlarmLatencyExceeded != nil,
		},
		{
			Key:      PostLatencyClassificationEvidenceKeyPublishToDetect,
			Millis:   clonePostLatencyInt64(row.PublishToDetectMillis),
			Selected: selectExternal,
		},
		{
			Key:      PostLatencyClassificationEvidenceKeyInternalLatency,
			Millis:   clonePostLatencyInt64(row.InternalLatencyMillis),
			Selected: selectInternal,
		},
		{
			Key:      PostLatencyClassificationEvidenceKeyQueueWait,
			Millis:   clonePostLatencyInt64(row.QueueWaitMillis),
			Selected: row.InternalDelayCause == PostInternalDelayCauseQueueWait,
		},
		{
			Key:      PostLatencyClassificationEvidenceKeyRetryAccumulation,
			Millis:   clonePostLatencyInt64(row.RetryAccumulationMillis),
			Selected: row.InternalDelayCause == PostInternalDelayCauseRetryAccumulation,
		},
		{
			Key:      PostLatencyClassificationEvidenceKeyJobFailure,
			Bool:     new(row.JobFailureDetected),
			Selected: row.InternalDelayCause == PostInternalDelayCauseJobFailure,
		},
	}
}

func clonePostLatencyInt64(value *int64) *int64 {
	return ClonePostLatencyInt64(value)
}

func ClonePostLatencyInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
