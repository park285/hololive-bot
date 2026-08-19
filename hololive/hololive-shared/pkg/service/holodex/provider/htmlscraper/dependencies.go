package htmlscraper

import (
	"context"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

// YouTubeClient는 htmlscraper facade가 사용하는 YouTube 조회·proxy 제어 계약이다.
type YouTubeClient interface {
	GetUpcomingEvents(context.Context, string) ([]*parser.UpcomingEvent, error)
	GetUpcomingEventsWaitAdmission(context.Context, string) ([]*parser.UpcomingEvent, error)
	GetRecentVideos(context.Context, string, int) ([]*parser.Video, error)
	GetPopularVideos(context.Context, string, int) ([]*parser.Video, error)
	GetChannelStats(context.Context, string) (*parser.ChannelStats, error)
	GetChannelSnippet(context.Context, string) (*parser.ChannelSnippet, error)
	SetProxyEnabled(bool) bool
	ProxyEnabled() bool
}

// ServiceDependencies는 Service가 직접 호출하는 외부 client를 담는다.
type ServiceDependencies struct {
	YouTube YouTubeClient
	HTTP    *http.Client
}
