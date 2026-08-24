package scraping

import (
	"fmt"

	"github.com/tidwall/gjson"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

var (
	findVideosTabContent  = parser.FindVideosTabContent
	collectVideoRenderers = parser.CollectVideoRenderers
)

const maxVideoRendererFallbackNodes = parser.MaxVideoRendererFallbackNodes

func parseVideosFromInitialData(
	data *gjson.Result,
	channelID string,
	maxResults int,
	videoParser func(*gjson.Result, string) *parser.Video,
) ([]*parser.Video, error) {
	if err := checkAlerts(data); err != nil {
		return nil, fmt.Errorf("check alerts: %w", err)
	}

	out, err := parser.ParseVideosFromInitialData(data, channelID, maxResults, videoParser)
	if err != nil {
		return out, fmt.Errorf("parse videos from initial data: %w", err)
	}

	return out, nil
}
