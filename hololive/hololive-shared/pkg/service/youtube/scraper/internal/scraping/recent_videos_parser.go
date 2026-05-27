package scraping

import (
	"github.com/tidwall/gjson"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/internal/scraping/parser"
)

var (
	parseVideosFromInitialDataWithoutTabs = parser.ParseVideosFromInitialDataWithoutTabs
	findVideosTabContent                  = parser.FindVideosTabContent
	collectVideoRenderers                 = parser.CollectVideoRenderers
)

const maxVideoRendererFallbackNodes = parser.MaxVideoRendererFallbackNodes

func parseVideosFromInitialData(
	data gjson.Result,
	channelID string,
	maxResults int,
	videoParser func(gjson.Result, string) *Video,
) ([]*Video, error) {
	if err := checkAlerts(data); err != nil {
		return nil, err
	}
	return parser.ParseVideosFromInitialData(data, channelID, maxResults, videoParser)
}
