package delivery

import "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/format"

type videoPayload = format.VideoPayload
type communityPayload = format.CommunityPayload
type milestonePayload = format.MilestonePayload
type groupedItemData = format.GroupedItemData
type groupedTemplateData = format.GroupedTemplateData
type templateData = format.TemplateData

var buildGroupedItemData = format.BuildGroupedItemData
var videoTemplateURL = format.VideoTemplateURL
