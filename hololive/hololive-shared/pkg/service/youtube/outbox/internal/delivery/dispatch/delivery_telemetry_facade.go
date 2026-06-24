package dispatch

import (
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/analytics"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/telemetry"
)

type DeliveryTelemetryRepository = telemetry.Repository

var NewDeliveryTelemetryRepository = telemetry.NewRepository

type PostSendCount = analytics.PostSendCount
type ChannelPostDeliverySummary = analytics.ChannelPostDeliverySummary
type PostDeliveryPathUsage = analytics.PostDeliveryPathUsage
type PostLatencyPeriod = analytics.PostLatencyPeriod
type PostLatencyPeriodSummary = analytics.PostLatencyPeriodSummary

var BuildChannelPostDeliverySummaries = analytics.BuildChannelPostDeliverySummaries
var BuildPostLatencyPeriodSummaries = analytics.BuildPostLatencyPeriodSummaries

const communityShortsDeliveryPath = telemetry.CommunityShortsDeliveryPath
