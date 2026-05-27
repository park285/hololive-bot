package scraping

import (
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/internal/scraping/parser"
)

var (
	parseChannelStatsFromInitialData   = parser.ParseChannelStatsFromInitialData
	parseChannelSnippetFromInitialData = parser.ParseChannelSnippetFromInitialData
	parseShortNumber                   = parser.ParseShortNumber
	parseViewCount                     = parser.ParseViewCount
	parseVideoCount                    = parser.ParseVideoCount
	parseSubscriberCount               = parser.ParseSubscriberCount
	parseJoinedDate                    = parser.ParseJoinedDate
)
