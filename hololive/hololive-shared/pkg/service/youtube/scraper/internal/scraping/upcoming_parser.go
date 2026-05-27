package scraping

import (
	"github.com/tidwall/gjson"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/internal/scraping/parser"
)

func parseUpcomingEventsFromInitialData(data gjson.Result) ([]*UpcomingEvent, error) {
	if err := checkAlerts(data); err != nil {
		return nil, err
	}
	return parser.ParseUpcomingEventsFromInitialData(data), nil
}
