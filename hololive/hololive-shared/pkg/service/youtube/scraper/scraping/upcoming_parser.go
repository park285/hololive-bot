package scraping

import (
	"github.com/tidwall/gjson"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

func parseUpcomingEventsFromInitialData(data *gjson.Result) ([]*parser.UpcomingEvent, error) {
	if err := checkAlerts(data); err != nil {
		return nil, err
	}
	return parser.ParseUpcomingEventsFromInitialData(data), nil
}
