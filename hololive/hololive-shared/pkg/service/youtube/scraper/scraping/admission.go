package scraping

import (
	youtubeadmission "github.com/kapu/hololive-shared/pkg/service/youtube/admission"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping/ratelimiter"
)

func newRateLimitAdmissionDeferredError(bucket string, decision ratelimiter.AdmissionDecision) error {
	return youtubeadmission.NewDeferredError(
		"youtube_scraper_rate_limit",
		bucket,
		decision.Reason,
		decision.RetryAfter,
		youtubeadmission.ErrDeferred,
	)
}
