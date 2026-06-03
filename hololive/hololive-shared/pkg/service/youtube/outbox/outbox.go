package outbox

import (
	delivery "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/store"
)

type DeliveryTelemetryRepository = delivery.DeliveryTelemetryRepository

type MessageFormatter = delivery.MessageFormatter

type PostSendCount = delivery.PostSendCount

type ChannelPostDeliverySummary = delivery.ChannelPostDeliverySummary

type PostDeliveryPathUsage = delivery.PostDeliveryPathUsage

type DispatchPayloadFormatter = delivery.DispatchPayloadFormatter

type PostDelaySource = delivery.PostDelaySource

type PostInternalDelayCause = delivery.PostInternalDelayCause

type PostLatencyClassificationStatus = delivery.PostLatencyClassificationStatus

type PostLatencyClassificationEvidenceKey = delivery.PostLatencyClassificationEvidenceKey

type PostLatencyClassificationEvidence = delivery.PostLatencyClassificationEvidence

type PostLatencyClassificationResult = delivery.PostLatencyClassificationResult

type PostLatencyReasonCode = delivery.PostLatencyReasonCode

type PostTrackingIdentity = delivery.PostTrackingIdentity

type PostDeliveryTimeline = delivery.PostDeliveryTimeline

type DeliveryRepository = store.DeliveryRepository

type Config = delivery.Config

type Dispatcher = delivery.Dispatcher

type PostLatencyPeriod = delivery.PostLatencyPeriod

type PostLatencyPeriodSummary = delivery.PostLatencyPeriodSummary

const (
	PostDelaySourceNone               = delivery.PostDelaySourceNone
	PostDelaySourceExternalCollection = delivery.PostDelaySourceExternalCollection
	PostDelaySourceInternalDelivery   = delivery.PostDelaySourceInternalDelivery
	PostDelaySourceMixed              = delivery.PostDelaySourceMixed

	PostInternalDelayCauseNone              = delivery.PostInternalDelayCauseNone
	PostInternalDelayCauseQueueWait         = delivery.PostInternalDelayCauseQueueWait
	PostInternalDelayCauseRetryAccumulation = delivery.PostInternalDelayCauseRetryAccumulation
	PostInternalDelayCauseJobFailure        = delivery.PostInternalDelayCauseJobFailure

	PostLatencyClassificationStatusInsufficientEvidence = delivery.PostLatencyClassificationStatusInsufficientEvidence
	PostLatencyClassificationStatusWithinTarget         = delivery.PostLatencyClassificationStatusWithinTarget
	PostLatencyClassificationStatusExceeded             = delivery.PostLatencyClassificationStatusExceeded

	PostLatencyClassificationEvidenceKeyAlarmLatency      = delivery.PostLatencyClassificationEvidenceKeyAlarmLatency
	PostLatencyClassificationEvidenceKeyPublishToDetect   = delivery.PostLatencyClassificationEvidenceKeyPublishToDetect
	PostLatencyClassificationEvidenceKeyInternalLatency   = delivery.PostLatencyClassificationEvidenceKeyInternalLatency
	PostLatencyClassificationEvidenceKeyQueueWait         = delivery.PostLatencyClassificationEvidenceKeyQueueWait
	PostLatencyClassificationEvidenceKeyRetryAccumulation = delivery.PostLatencyClassificationEvidenceKeyRetryAccumulation
	PostLatencyClassificationEvidenceKeyJobFailure        = delivery.PostLatencyClassificationEvidenceKeyJobFailure

	PostLatencyReasonCodeNone                 = delivery.PostLatencyReasonCodeNone
	PostLatencyReasonCodeExternalCollection   = delivery.PostLatencyReasonCodeExternalCollection
	PostLatencyReasonCodeInternalDelivery     = delivery.PostLatencyReasonCodeInternalDelivery
	PostLatencyReasonCodeMixed                = delivery.PostLatencyReasonCodeMixed
	PostLatencyReasonCodeQueueWait            = delivery.PostLatencyReasonCodeQueueWait
	PostLatencyReasonCodeRetryAccumulation    = delivery.PostLatencyReasonCodeRetryAccumulation
	PostLatencyReasonCodeJobFailure           = delivery.PostLatencyReasonCodeJobFailure
	PostLatencyReasonCodeInsufficientEvidence = delivery.PostLatencyReasonCodeInsufficientEvidence
)

var ErrDeliveryDedupeKeyRequired = delivery.ErrDeliveryDedupeKeyRequired

var NewDeliveryTelemetryRepository = delivery.NewDeliveryTelemetryRepository

var BuildChannelPostDeliverySummaries = delivery.BuildChannelPostDeliverySummaries

var FormatYouTubeOutboxPayload = delivery.FormatYouTubeOutboxPayload

var NewDeliveryRepository = store.NewDeliveryRepository

var DefaultConfig = delivery.DefaultConfig

var NewDispatcher = delivery.NewDispatcher

var BuildPostLatencyClassification = delivery.BuildPostLatencyClassification

var BuildPostLatencyPeriodSummaries = delivery.BuildPostLatencyPeriodSummaries
