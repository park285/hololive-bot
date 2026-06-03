package telemetry

import "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/timeline"

type PostDeliveryTimeline = timeline.PostDeliveryTimeline
type PostTrackingIdentity = timeline.PostTrackingIdentity
type PostDelaySource = timeline.PostDelaySource
type PostInternalDelayCause = timeline.PostInternalDelayCause
type PostLatencyClassificationStatus = timeline.PostLatencyClassificationStatus

const (
	PostDelaySourceNone                                 = timeline.PostDelaySourceNone
	PostInternalDelayCauseNone                          = timeline.PostInternalDelayCauseNone
	PostLatencyClassificationStatusInsufficientEvidence = timeline.PostLatencyClassificationStatusInsufficientEvidence
)
