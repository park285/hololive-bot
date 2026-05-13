package youtube

import (
	"errors"
	"net/http"

	"google.golang.org/api/googleapi"

	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
)

func summarizeUpcomingScrapeFailures(failures []upcomingScrapeFailure) map[string]int {
	summary := make(map[string]int)
	for _, failure := range failures {
		key := failure.Source + ":" + failure.Reason
		summary[key]++
	}
	return summary
}

func upcomingFailureByChannel(failures []upcomingScrapeFailure) map[string]upcomingScrapeFailure {
	out := make(map[string]upcomingScrapeFailure, len(failures))
	for _, failure := range failures {
		if failure.ChannelID == "" {
			continue
		}
		out[failure.ChannelID] = failure
	}
	return out
}

func classifyYouTubeAPIFailure(err error) scraper.FailureDetail {
	detail := scraper.FailureDetail{
		Reason:  scraper.FailureReasonUnknown,
		Source:  scraper.FailureSourceAPI,
		Message: errString(err),
	}
	var apiErr *googleapi.Error
	if errors.As(err, &apiErr) {
		detail.StatusCode = apiErr.Code
		detail.Reason = apiFailureReason(apiErr.Code)
		return detail
	}
	var quotaErr *QuotaExceededError
	if errors.As(err, &quotaErr) {
		detail.StatusCode = http.StatusForbidden
		detail.Reason = scraper.FailureReasonForbidden
		return detail
	}
	return scraper.ClassifyFailure(err, scraper.FailureSourceAPI)
}

func apiFailureReason(statusCode int) scraper.FailureReason {
	switch statusCode {
	case http.StatusForbidden:
		return scraper.FailureReasonForbidden
	case http.StatusTooManyRequests:
		return scraper.FailureReasonRateLimited
	case http.StatusRequestTimeout:
		return scraper.FailureReasonTimeout
	default:
		return scraper.FailureReasonHTTPStatus
	}
}

func errString(err error) string {
	if err == nil {
		return ""
	}
	return err.Error()
}
