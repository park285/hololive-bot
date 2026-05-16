package scraping

import (
	"context"
	"errors"
	"net"
	"net/http"
	"net/url"
	"strings"
	"time"
)

type FailureReason string

const (
	FailureReasonNone               FailureReason = "none"
	FailureReasonRateLimited        FailureReason = "rate_limited"
	FailureReasonForbidden          FailureReason = "forbidden"
	FailureReasonCooldown           FailureReason = "cooldown"
	FailureReasonTimeout            FailureReason = "timeout"
	FailureReasonTransport          FailureReason = "transport"
	FailureReasonHTTPStatus         FailureReason = "http_status"
	FailureReasonParserDrift        FailureReason = "parser_drift"
	FailureReasonEmptyResponse      FailureReason = "empty_response"
	FailureReasonChannelNotFound    FailureReason = "channel_not_found"
	FailureReasonChannelUnavailable FailureReason = "channel_unavailable"
	FailureReasonContextCanceled    FailureReason = "context_canceled"
	FailureReasonUnknown            FailureReason = "unknown"
)

type FailureSource string

const (
	FailureSourceHTML            FailureSource = "html"
	FailureSourceRSS             FailureSource = "rss"
	FailureSourceAPI             FailureSource = "api"
	FailureSourceBrowserSnapshot FailureSource = "browser_snapshot"
)

type FailureDetail struct {
	Reason     FailureReason
	Source     FailureSource
	StatusCode int
	RetryAfter time.Duration
	Message    string
}

func ClassifyFailure(err error, source FailureSource) FailureDetail {
	if err == nil {
		return FailureDetail{Reason: FailureReasonNone, Source: source}
	}
	detail := FailureDetail{Reason: FailureReasonUnknown, Source: source, Message: err.Error()}
	for _, classify := range []func(error, *FailureDetail) bool{
		classifyContextFailure,
		classifyYouTubeSentinelFailure,
		classifyHTTPFailure,
		classifyNetworkFailure,
		classifyParserFailure,
	} {
		if classify(err, &detail) {
			return detail
		}
	}
	return detail
}

func classifyContextFailure(err error, detail *FailureDetail) bool {
	if !errors.Is(err, context.Canceled) {
		return false
	}
	detail.Reason = FailureReasonContextCanceled
	return true
}

func classifyYouTubeSentinelFailure(err error, detail *FailureDetail) bool {
	if errors.Is(err, ErrRateLimited) {
		detail.Reason = FailureReasonRateLimited
		detail.StatusCode = http.StatusTooManyRequests
		detail.RetryAfter = extractHTTPRetryAfter(err)
		return true
	}
	if errors.Is(err, ErrForbidden) {
		detail.Reason = FailureReasonForbidden
		detail.StatusCode = http.StatusForbidden
		detail.RetryAfter = extractHTTPRetryAfter(err)
		return true
	}
	if IsParserDriftError(err) {
		detail.Reason = FailureReasonParserDrift
		return true
	}
	var cooldown *CooldownError
	if errors.As(err, &cooldown) {
		detail.Reason = FailureReasonCooldown
		detail.RetryAfter = cooldown.RetryDelay()
		return true
	}
	return classifyChannelFailure(err, detail)
}

func classifyChannelFailure(err error, detail *FailureDetail) bool {
	switch {
	case errors.Is(err, ErrChannelNotFound):
		detail.Reason = FailureReasonChannelNotFound
	case errors.Is(err, ErrChannelUnavailable):
		detail.Reason = FailureReasonChannelUnavailable
	default:
		return false
	}
	return true
}

func classifyHTTPFailure(err error, detail *FailureDetail) bool {
	var statusErr *httpStatusError
	if !errors.As(err, &statusErr) {
		return false
	}
	detail.Reason = FailureReasonHTTPStatus
	detail.StatusCode = statusErr.code
	detail.RetryAfter = statusErr.retryAfter
	return true
}

func classifyNetworkFailure(err error, detail *FailureDetail) bool {
	if isTimeoutFailure(err) {
		detail.Reason = FailureReasonTimeout
		return true
	}
	if isRetryableTransportError(err) {
		detail.Reason = FailureReasonTransport
		return true
	}
	return false
}

func classifyParserFailure(err error, detail *FailureDetail) bool {
	if IsParserDriftError(err) {
		detail.Reason = FailureReasonParserDrift
		return true
	}
	if strings.Contains(strings.ToLower(err.Error()), "empty response") {
		detail.Reason = FailureReasonEmptyResponse
		return true
	}
	return false
}

func isTimeoutFailure(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return true
	}
	var netErr net.Error
	if errors.As(err, &netErr) && netErr.Timeout() {
		return true
	}
	var urlErr *url.Error
	if errors.As(err, &urlErr) {
		if errors.Is(urlErr.Err, context.DeadlineExceeded) {
			return true
		}
		var nestedNetErr net.Error
		return errors.As(urlErr.Err, &nestedNetErr) && nestedNetErr.Timeout()
	}
	return false
}
