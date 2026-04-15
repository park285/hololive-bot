package outbox

import "time"

func buildPostDeliveryTimelinesFromScanRows(scanned []postDeliveryTimelineScanRow) []PostDeliveryTimeline {
	rows := make([]PostDeliveryTimeline, 0, len(scanned))
	for i := range scanned {
		row := PostDeliveryTimeline{
			OutboxID:                   scanned[i].OutboxID,
			OutboxKind:                 scanned[i].OutboxKind,
			AlarmType:                  scanned[i].AlarmType,
			ChannelID:                  scanned[i].ChannelID,
			PostID:                     scanned[i].PostID,
			ContentID:                  scanned[i].ContentID,
			ActualPublishedAt:          scanned[i].ActualPublishedAt.Ptr(),
			DetectedAt:                 scanned[i].DetectedAt.Ptr(),
			QueueEnqueuedAt:            scanned[i].QueueEnqueuedAt.Ptr(),
			FirstAttemptStartedAt:      scanned[i].FirstAttemptStartedAt.Ptr(),
			LastAttemptStartedAt:       scanned[i].LastAttemptStartedAt.Ptr(),
			FirstAttemptFinishedAt:     scanned[i].FirstAttemptFinishedAt.Ptr(),
			LastAttemptFinishedAt:      scanned[i].LastAttemptFinishedAt.Ptr(),
			AlarmSentAt:                scanned[i].AlarmSentAt.Ptr(),
			FirstSuccessAt:             scanned[i].FirstSuccessAt.Ptr(),
			LastSuccessAt:              scanned[i].LastSuccessAt.Ptr(),
			LastFailureAt:              scanned[i].LastFailureAt.Ptr(),
			NextRetryAt:                scanned[i].NextRetryAt.Ptr(),
			SuccessSendCount:           scanned[i].SuccessSendCount,
			FailedAttemptCount:         scanned[i].FailedAttemptCount,
			MaxAttemptOrdinal:          scanned[i].MaxAttemptOrdinal,
			AlarmLatencyMillis:         scanned[i].AlarmLatencyMillis,
			AlarmLatencyExceeded:       scanned[i].AlarmLatencyExceeded.Ptr(),
			StoredClassificationStatus: scanned[i].StoredClassificationStatus,
			StoredDelaySource:          scanned[i].StoredDelaySource,
			StoredInternalDelayCause:   scanned[i].StoredInternalDelayCause,
		}
		derivePostDeliveryTimelineMetrics(&row)
		rows = append(rows, row)
	}
	return rows
}

func derivePostDeliveryTimelineMetrics(row *PostDeliveryTimeline) {
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
		row.InternalLatencyExceeded = boolPtr(*row.InternalLatencyMillis > postLatencyExceededThresholdMillis)
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

	if row.AlarmLatencyExceeded != nil {
		if !*row.AlarmLatencyExceeded {
			if row.InternalLatencyExceeded != nil && *row.InternalLatencyExceeded {
				return selectDominantDelaySource(externalMillis, hasExternal, internalMillis, hasInternal)
			}
			return PostDelaySourceNone
		}
		return selectDominantDelaySource(externalMillis, hasExternal, internalMillis, hasInternal)
	}

	if row.InternalLatencyExceeded != nil && *row.InternalLatencyExceeded {
		return selectDominantDelaySource(externalMillis, hasExternal, internalMillis, hasInternal)
	}
	if row.PublishToDetectMillis != nil && *row.PublishToDetectMillis > postLatencyExceededThresholdMillis {
		return selectDominantDelaySource(externalMillis, hasExternal, internalMillis, hasInternal)
	}

	return PostDelaySourceNone
}

func positiveDurationMillis(value *int64) (int64, bool) {
	if value == nil || *value <= 0 {
		return 0, false
	}
	return *value, true
}

func selectDominantDelaySource(externalMillis int64, hasExternal bool, internalMillis int64, hasInternal bool) PostDelaySource {
	switch {
	case hasExternal && !hasInternal:
		return PostDelaySourceExternalCollection
	case !hasExternal && hasInternal:
		return PostDelaySourceInternalDelivery
	case !hasExternal && !hasInternal:
		return PostDelaySourceNone
	case externalMillis > internalMillis:
		return PostDelaySourceExternalCollection
	case internalMillis > externalMillis:
		return PostDelaySourceInternalDelivery
	default:
		return PostDelaySourceMixed
	}
}

func classifyPrimaryInternalDelayCause(row *PostDeliveryTimeline) PostInternalDelayCause {
	if row == nil {
		return PostInternalDelayCauseNone
	}

	if row.JobFailureDetected {
		return PostInternalDelayCauseJobFailure
	}

	candidates := []postInternalDelayCauseCandidate{
		{
			cause:     PostInternalDelayCauseRetryAccumulation,
			millis:    row.RetryAccumulationMillis,
			priority:  postInternalDelayCausePriorityRetryAccumulation,
			available: row.RetryAccumulationMillis != nil && *row.RetryAccumulationMillis > 0,
		},
		{
			cause:     PostInternalDelayCauseQueueWait,
			millis:    row.QueueWaitMillis,
			priority:  postInternalDelayCausePriorityQueueWait,
			available: row.QueueWaitMillis != nil && *row.QueueWaitMillis > 0,
		},
	}

	selected := postInternalDelayCauseCandidate{cause: PostInternalDelayCauseNone}
	for i := range candidates {
		if !candidates[i].available {
			continue
		}
		if selected.cause == PostInternalDelayCauseNone {
			selected = candidates[i]
			continue
		}
		if *candidates[i].millis > *selected.millis {
			selected = candidates[i]
			continue
		}
		if *candidates[i].millis == *selected.millis && candidates[i].priority > selected.priority {
			selected = candidates[i]
		}
	}

	return selected.cause
}

func BuildPostLatencyClassification(row PostDeliveryTimeline) PostLatencyClassificationResult {
	return buildPostLatencyClassification(&row)
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
		ThresholdMillis:    postLatencyExceededThresholdMillis,
		DelaySource:        delaySource,
		InternalDelayCause: internalDelayCause,
		Evidence:           buildPostLatencyClassificationEvidence(row),
	}
}

func classifyPostLatencyReasonCode(classification PostLatencyClassificationResult) PostLatencyReasonCode {
	switch classification.DelaySource {
	case PostDelaySourceExternalCollection:
		return PostLatencyReasonCodeExternalCollection
	case PostDelaySourceMixed:
		return PostLatencyReasonCodeMixed
	}

	switch classification.InternalDelayCause {
	case PostInternalDelayCauseQueueWait:
		return PostLatencyReasonCodeQueueWait
	case PostInternalDelayCauseRetryAccumulation:
		return PostLatencyReasonCodeRetryAccumulation
	case PostInternalDelayCauseJobFailure:
		return PostLatencyReasonCodeJobFailure
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
	if row.InternalLatencyExceeded != nil && *row.InternalLatencyExceeded {
		return PostLatencyClassificationStatusExceeded
	}
	if row.PublishToDetectMillis != nil && *row.PublishToDetectMillis > postLatencyExceededThresholdMillis {
		return PostLatencyClassificationStatusExceeded
	}
	if row.QueueWaitMillis != nil && *row.QueueWaitMillis > postLatencyExceededThresholdMillis {
		return PostLatencyClassificationStatusExceeded
	}
	if row.RetryAccumulationMillis != nil && *row.RetryAccumulationMillis > postLatencyExceededThresholdMillis {
		return PostLatencyClassificationStatusExceeded
	}
	return PostLatencyClassificationStatusInsufficientEvidence
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
			Bool:     boolPtr(row.JobFailureDetected),
			Selected: row.InternalDelayCause == PostInternalDelayCauseJobFailure,
		},
	}
}

func clonePostLatencyInt64(value *int64) *int64 {
	if value == nil {
		return nil
	}
	cloned := *value
	return &cloned
}
