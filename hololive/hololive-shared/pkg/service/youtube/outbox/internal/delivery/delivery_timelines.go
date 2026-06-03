package delivery

import (
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/timeline"
)

type PostDelaySource = timeline.PostDelaySource
type PostInternalDelayCause = timeline.PostInternalDelayCause
type PostLatencyClassificationStatus = timeline.PostLatencyClassificationStatus
type PostLatencyClassificationEvidenceKey = timeline.PostLatencyClassificationEvidenceKey
type PostLatencyClassificationEvidence = timeline.PostLatencyClassificationEvidence
type PostLatencyClassificationResult = timeline.PostLatencyClassificationResult
type PostLatencyReasonCode = timeline.PostLatencyReasonCode
type PostTrackingIdentity = timeline.PostTrackingIdentity
type PostDeliveryTimeline = timeline.PostDeliveryTimeline

const (
	PostDelaySourceNone               = timeline.PostDelaySourceNone
	PostDelaySourceExternalCollection = timeline.PostDelaySourceExternalCollection
	PostDelaySourceInternalDelivery   = timeline.PostDelaySourceInternalDelivery
	PostDelaySourceMixed              = timeline.PostDelaySourceMixed

	PostInternalDelayCauseNone              = timeline.PostInternalDelayCauseNone
	PostInternalDelayCauseQueueWait         = timeline.PostInternalDelayCauseQueueWait
	PostInternalDelayCauseRetryAccumulation = timeline.PostInternalDelayCauseRetryAccumulation
	PostInternalDelayCauseJobFailure        = timeline.PostInternalDelayCauseJobFailure

	PostLatencyClassificationStatusInsufficientEvidence = timeline.PostLatencyClassificationStatusInsufficientEvidence
	PostLatencyClassificationStatusWithinTarget         = timeline.PostLatencyClassificationStatusWithinTarget
	PostLatencyClassificationStatusExceeded             = timeline.PostLatencyClassificationStatusExceeded

	PostLatencyClassificationEvidenceKeyAlarmLatency      = timeline.PostLatencyClassificationEvidenceKeyAlarmLatency
	PostLatencyClassificationEvidenceKeyPublishToDetect   = timeline.PostLatencyClassificationEvidenceKeyPublishToDetect
	PostLatencyClassificationEvidenceKeyInternalLatency   = timeline.PostLatencyClassificationEvidenceKeyInternalLatency
	PostLatencyClassificationEvidenceKeyQueueWait         = timeline.PostLatencyClassificationEvidenceKeyQueueWait
	PostLatencyClassificationEvidenceKeyRetryAccumulation = timeline.PostLatencyClassificationEvidenceKeyRetryAccumulation
	PostLatencyClassificationEvidenceKeyJobFailure        = timeline.PostLatencyClassificationEvidenceKeyJobFailure

	PostLatencyReasonCodeNone                 = timeline.PostLatencyReasonCodeNone
	PostLatencyReasonCodeExternalCollection   = timeline.PostLatencyReasonCodeExternalCollection
	PostLatencyReasonCodeInternalDelivery     = timeline.PostLatencyReasonCodeInternalDelivery
	PostLatencyReasonCodeMixed                = timeline.PostLatencyReasonCodeMixed
	PostLatencyReasonCodeQueueWait            = timeline.PostLatencyReasonCodeQueueWait
	PostLatencyReasonCodeRetryAccumulation    = timeline.PostLatencyReasonCodeRetryAccumulation
	PostLatencyReasonCodeJobFailure           = timeline.PostLatencyReasonCodeJobFailure
	PostLatencyReasonCodeInsufficientEvidence = timeline.PostLatencyReasonCodeInsufficientEvidence
)

var BuildPostLatencyClassification = timeline.BuildPostLatencyClassification

const postLatencyExceededThresholdMillis = timeline.PostLatencyExceededThresholdMillis

var derivePostDeliveryTimelineMetrics = timeline.DeriveMetrics
