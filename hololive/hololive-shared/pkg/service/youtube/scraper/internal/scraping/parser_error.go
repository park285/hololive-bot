package scraping

import (
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/internal/scraping/parser"
)

type ParserDriftError = parser.ParserDriftError

var (
	ErrParserDrift      = parser.ErrParserDrift
	NewParserDriftError = parser.NewParserDriftError
	IsParserDriftError  = parser.IsParserDriftError
)
