package scraping

import initialdata "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/internal/initialdata"

var ErrYtInitialDataNotFound = initialdata.ErrNotFound

func extractYtInitialData(html string) (string, error) {
	return initialdata.Extract(html)
}
