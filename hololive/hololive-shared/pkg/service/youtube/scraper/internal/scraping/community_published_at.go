package scraping

import (
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/internal/scraping/parser"
)

var (
	ErrPublishedAtNotFound              = parser.ErrPublishedAtNotFound
	ErrCommunityPublishedAtNotFound     = parser.ErrCommunityPublishedAtNotFound
	normalizePublishedAtCandidate       = parser.NormalizePublishedAtCandidate
	extractPublishedAtFromHTML          = parser.ExtractPublishedAtFromHTML
	extractCommunityPublishedAtFromHTML = parser.ExtractCommunityPublishedAtFromHTML
)
