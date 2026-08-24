package scraping

import (
	"fmt"

	"github.com/tidwall/gjson"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

func parseUpcomingEventsFromInitialData(data *gjson.Result) ([]*parser.UpcomingEvent, error) {
	if err := checkAlerts(data); err != nil {
		return nil, fmt.Errorf("check alerts: %w", err)
	}

	return parser.ParseUpcomingEventsFromInitialData(data), nil
}
