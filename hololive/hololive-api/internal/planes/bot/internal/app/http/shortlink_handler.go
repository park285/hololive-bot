package apphttp

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	shortlinkcontracts "github.com/kapu/hololive-shared/pkg/contracts/shortlink"
	"github.com/kapu/hololive-shared/pkg/domain"
	shortlinkservice "github.com/kapu/hololive-shared/pkg/service/shortlink"
)

const kakaoTalkScraperUserAgentMarker = "kakaotalk-scrap/"

func registerShortLinkRoutes(router gin.IRoutes) {
	if router == nil {
		return
	}
	router.GET(shortlinkcontracts.YouTubeRoute, handleYouTubeShortLink)
	router.HEAD(shortlinkcontracts.YouTubeRoute, handleYouTubeShortLink)
}

func handleYouTubeShortLink(c *gin.Context) {
	setShortLinkResponseHeaders(c)

	videoID := c.Param("videoID")
	if !shortlinkservice.ValidYouTubeVideoID(videoID) {
		c.Status(http.StatusNotFound)
		return
	}
	if isKakaoTalkScraper(c.GetHeader("User-Agent")) {
		c.Status(http.StatusForbidden)
		return
	}

	c.Redirect(http.StatusFound, domain.YouTubeWatchURL(videoID))
}

func setShortLinkResponseHeaders(c *gin.Context) {
	c.Header("Cache-Control", "no-store, max-age=0")
	c.Header("Pragma", "no-cache")
	c.Header("Referrer-Policy", "no-referrer")
	c.Header("Vary", "User-Agent")
	c.Header("X-Robots-Tag", "noindex, nofollow, noarchive, nosnippet, noimageindex")
}

func isKakaoTalkScraper(userAgent string) bool {
	return strings.Contains(strings.ToLower(userAgent), kakaoTalkScraperUserAgentMarker)
}
