package holodex

import (
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/internal/holodexprovider"
	"github.com/kapu/hololive-shared/pkg/service/holodex/internal/holodexprovider/apiclient"
)

type Requester = apiclient.Requester

type APIClient = apiclient.APIClient

type APIError = apiclient.APIError

type KeyRotationError = apiclient.KeyRotationError

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

var NewHolodexAPIClient = apiclient.NewHolodexAPIClient

var NewAPIError = apiclient.NewAPIError

var NewKeyRotationError = apiclient.NewKeyRotationError

var NewHolodexService = holodexprovider.NewHolodexService

var NewScraperService = holodexprovider.NewScraperService

var NewScraperServiceWithYouTubeProducer = holodexprovider.NewScraperServiceWithYouTubeProducer

var IsStructureError = holodexprovider.IsStructureError

var NewCacheManager = holodexprovider.NewCacheManager

var NewStreamFilter = holodexprovider.NewStreamFilter

var NewStreamMapper = holodexprovider.NewStreamMapper

var NewPhotoSyncService = holodexprovider.NewPhotoSyncService

var SupportedStreamOrgParams = holodexprovider.SupportedStreamOrgParams
