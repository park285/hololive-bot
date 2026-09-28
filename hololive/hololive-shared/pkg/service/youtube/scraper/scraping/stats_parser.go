package scraping

import (
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

var (
	parseShortNumber = parser.ParseShortNumber
	parseViewCount   = parser.ParseViewCount
	parseVideoCount  = parser.ParseVideoCount
)
