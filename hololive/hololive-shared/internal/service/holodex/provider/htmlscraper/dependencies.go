package htmlscraper

import (
	"context"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/parser"
)

// YouTube 채널 일정·통계·snippet 조회는 Holodex 채널·일정 조회의 보조 원천이었다. 그 폴백을
// DEC-20260926-hololive-source-fallbacks-retirement로 삭제해 이 facade는 호출하지 않는다.
type youTubeVideoClient interface {
	GetRecentVideos(context.Context, string, int) ([]*parser.Video, error)
	GetPopularVideos(context.Context, string, int) ([]*parser.Video, error)
}

// YouTubeClient는 htmlscraper facade가 사용하는 YouTube 조회 계약이다. Scraper proxy 토글은
// DEC-20260926-hololive-legacy-env-config-retirement로 지웠다.
type YouTubeClient interface {
	youTubeVideoClient
}

// ServiceDependencies는 Service가 직접 호출하는 외부 client를 담는다.
type ServiceDependencies struct {
	YouTube YouTubeClient
	HTTP    *http.Client
}
