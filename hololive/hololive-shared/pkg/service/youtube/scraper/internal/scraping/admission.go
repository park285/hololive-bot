package scraping

import (
	youtubeadmission "github.com/kapu/hololive-shared/pkg/service/youtube/internal/admission"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper/internal/scraping/ratelimiter"
)

type AdmissionDeferredError = youtubeadmission.DeferredError

var ErrAdmissionDeferred = youtubeadmission.ErrDeferred

func IsAdmissionDeferred(err error) bool {
	return youtubeadmission.IsDeferred(err)
}

func newRateLimitAdmissionDeferredError(bucket string, decision ratelimiter.AdmissionDecision) error {
	return youtubeadmission.NewDeferredError(
		"youtube_scraper_rate_limit",
		bucket,
		decision.Reason,
		decision.RetryAfter,
		ErrAdmissionDeferred,
	)
}
