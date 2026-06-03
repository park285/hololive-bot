// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package delivery

import (
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/dispatch"
)

type DeliveryTelemetryRepository = dispatch.DeliveryTelemetryRepository

type MessageFormatter = dispatch.MessageFormatter

type PostSendCount = dispatch.PostSendCount

type ChannelPostDeliverySummary = dispatch.ChannelPostDeliverySummary

type PostDeliveryPathUsage = dispatch.PostDeliveryPathUsage

type DispatchPayloadFormatter = dispatch.DispatchPayloadFormatter

type PostDelaySource = dispatch.PostDelaySource

type PostInternalDelayCause = dispatch.PostInternalDelayCause

type PostLatencyClassificationStatus = dispatch.PostLatencyClassificationStatus

type PostLatencyClassificationEvidenceKey = dispatch.PostLatencyClassificationEvidenceKey

type PostLatencyClassificationEvidence = dispatch.PostLatencyClassificationEvidence

type PostLatencyClassificationResult = dispatch.PostLatencyClassificationResult

type PostLatencyReasonCode = dispatch.PostLatencyReasonCode

type PostTrackingIdentity = dispatch.PostTrackingIdentity

type PostDeliveryTimeline = dispatch.PostDeliveryTimeline

type Config = dispatch.Config

type Dispatcher = dispatch.Dispatcher

type PostLatencyPeriod = dispatch.PostLatencyPeriod

type PostLatencyPeriodSummary = dispatch.PostLatencyPeriodSummary

const (
	PostDelaySourceNone               = dispatch.PostDelaySourceNone
	PostDelaySourceExternalCollection = dispatch.PostDelaySourceExternalCollection
	PostDelaySourceInternalDelivery   = dispatch.PostDelaySourceInternalDelivery
	PostDelaySourceMixed              = dispatch.PostDelaySourceMixed

	PostInternalDelayCauseNone              = dispatch.PostInternalDelayCauseNone
	PostInternalDelayCauseQueueWait         = dispatch.PostInternalDelayCauseQueueWait
	PostInternalDelayCauseRetryAccumulation = dispatch.PostInternalDelayCauseRetryAccumulation
	PostInternalDelayCauseJobFailure        = dispatch.PostInternalDelayCauseJobFailure

	PostLatencyClassificationStatusInsufficientEvidence = dispatch.PostLatencyClassificationStatusInsufficientEvidence
	PostLatencyClassificationStatusWithinTarget         = dispatch.PostLatencyClassificationStatusWithinTarget
	PostLatencyClassificationStatusExceeded             = dispatch.PostLatencyClassificationStatusExceeded

	PostLatencyClassificationEvidenceKeyAlarmLatency      = dispatch.PostLatencyClassificationEvidenceKeyAlarmLatency
	PostLatencyClassificationEvidenceKeyPublishToDetect   = dispatch.PostLatencyClassificationEvidenceKeyPublishToDetect
	PostLatencyClassificationEvidenceKeyInternalLatency   = dispatch.PostLatencyClassificationEvidenceKeyInternalLatency
	PostLatencyClassificationEvidenceKeyQueueWait         = dispatch.PostLatencyClassificationEvidenceKeyQueueWait
	PostLatencyClassificationEvidenceKeyRetryAccumulation = dispatch.PostLatencyClassificationEvidenceKeyRetryAccumulation
	PostLatencyClassificationEvidenceKeyJobFailure        = dispatch.PostLatencyClassificationEvidenceKeyJobFailure

	PostLatencyReasonCodeNone                 = dispatch.PostLatencyReasonCodeNone
	PostLatencyReasonCodeExternalCollection   = dispatch.PostLatencyReasonCodeExternalCollection
	PostLatencyReasonCodeInternalDelivery     = dispatch.PostLatencyReasonCodeInternalDelivery
	PostLatencyReasonCodeMixed                = dispatch.PostLatencyReasonCodeMixed
	PostLatencyReasonCodeQueueWait            = dispatch.PostLatencyReasonCodeQueueWait
	PostLatencyReasonCodeRetryAccumulation    = dispatch.PostLatencyReasonCodeRetryAccumulation
	PostLatencyReasonCodeJobFailure           = dispatch.PostLatencyReasonCodeJobFailure
	PostLatencyReasonCodeInsufficientEvidence = dispatch.PostLatencyReasonCodeInsufficientEvidence
)

var ErrDeliveryDedupeKeyRequired = dispatch.ErrDeliveryDedupeKeyRequired

var NewDeliveryTelemetryRepository = dispatch.NewDeliveryTelemetryRepository

var BuildChannelPostDeliverySummaries = dispatch.BuildChannelPostDeliverySummaries

var FormatYouTubeOutboxPayload = dispatch.FormatYouTubeOutboxPayload

var DefaultConfig = dispatch.DefaultConfig

var NewDispatcher = dispatch.NewDispatcher

var BuildPostLatencyClassification = dispatch.BuildPostLatencyClassification

var BuildPostLatencyPeriodSummaries = dispatch.BuildPostLatencyPeriodSummaries
