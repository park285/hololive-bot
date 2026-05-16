package holodex

import holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/internal/holodexprovider"

type Requester = holodexprovider.Requester

type APIClient = holodexprovider.APIClient

type APIError = holodexprovider.APIError

type KeyRotationError = holodexprovider.KeyRotationError

type ChannelRaw = holodexprovider.ChannelRaw

type StreamRaw = holodexprovider.StreamRaw

type Service = holodexprovider.Service

type ScraperService = holodexprovider.ScraperService

type StructureChangedError = holodexprovider.StructureChangedError

type CacheManager = holodexprovider.CacheManager

type StreamFilter = holodexprovider.StreamFilter

type StreamMapper = holodexprovider.StreamMapper

type PhotoSyncService = holodexprovider.PhotoSyncService

var ErrInvalidStreamOrg = holodexprovider.ErrInvalidStreamOrg

var NewHolodexAPIClient = holodexprovider.NewHolodexAPIClient

var NewAPIError = holodexprovider.NewAPIError

var NewKeyRotationError = holodexprovider.NewKeyRotationError

var NewHolodexService = holodexprovider.NewHolodexService

var NewScraperService = holodexprovider.NewScraperService

var NewScraperServiceWithYouTubeScraper = holodexprovider.NewScraperServiceWithYouTubeScraper

var IsStructureError = holodexprovider.IsStructureError

var NewCacheManager = holodexprovider.NewCacheManager

var NewStreamFilter = holodexprovider.NewStreamFilter

var NewStreamMapper = holodexprovider.NewStreamMapper

var NewPhotoSyncService = holodexprovider.NewPhotoSyncService

var SupportedStreamOrgParams = holodexprovider.SupportedStreamOrgParams
