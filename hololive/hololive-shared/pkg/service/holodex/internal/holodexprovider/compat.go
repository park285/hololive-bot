package holodexprovider

import (
	"github.com/kapu/hololive-shared/pkg/service/holodex/internal/holodexprovider/apiclient"
	"github.com/kapu/hololive-shared/pkg/service/holodex/internal/holodexprovider/htmlscraper"
	"github.com/kapu/hololive-shared/pkg/service/holodex/internal/holodexprovider/streammapping"
)

type ScraperService = htmlscraper.Service
type StructureChangedError = htmlscraper.StructureChangedError
type StreamRaw = streammapping.StreamRaw
type ChannelRaw = streammapping.ChannelRaw
type StreamMapper = streammapping.StreamMapper
type StreamFilter = streammapping.StreamFilter
type Requester = apiclient.Requester
type APIError = apiclient.APIError

var (
	NewStreamMapper                      = streammapping.NewStreamMapper
	NewStreamFilter                      = streammapping.NewStreamFilter
	NewScraperService                    = htmlscraper.NewService
	NewScraperServiceWithYouTubeProducer = htmlscraper.NewServiceWithYouTubeProducer
	IsStructureError                     = htmlscraper.IsStructureError
)
