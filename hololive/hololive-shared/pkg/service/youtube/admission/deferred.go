package admission

import (
	"errors"
	"strings"
	"time"
)

var ErrDeferred = errors.New("youtube admission deferred")

type DeferredError struct {
	Source     string
	Bucket     string
	Reason     string
	RetryAfter time.Duration
	Cause      error
}

func NewDeferredError(source, bucket, reason string, retryAfter time.Duration, cause error) *DeferredError {
	return &DeferredError{
		Source:     strings.TrimSpace(source),
		Bucket:     strings.TrimSpace(bucket),
		Reason:     strings.TrimSpace(reason),
		RetryAfter: retryAfter,
		Cause:      cause,
	}
}

func (e *DeferredError) Error() string {
	if e == nil {
		return ErrDeferred.Error()
	}

	parts := []string{ErrDeferred.Error()}
	appendKV := func(key, value string) {
		value = strings.TrimSpace(value)
		if value != "" {
			parts = append(parts, key+"="+value)
		}
	}
	appendKV("source", e.Source)
	appendKV("bucket", e.Bucket)
	appendKV("reason", e.Reason)
	if e.RetryAfter > 0 {
		parts = append(parts, "retry_after="+e.RetryAfter.Round(time.Millisecond).String())
	}

	message := strings.Join(parts, " ")
	if e.Cause == nil || errors.Is(e.Cause, ErrDeferred) {
		return message
	}
	return message + ": " + e.Cause.Error()
}

func (e *DeferredError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Cause
}

func (e *DeferredError) Is(target error) bool {
	return target == ErrDeferred
}

func (e *DeferredError) RetryDelay() time.Duration {
	if e == nil || e.RetryAfter <= 0 {
		return 0
	}
	return e.RetryAfter
}

func IsDeferred(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrDeferred) {
		return true
	}
	var deferred *DeferredError
	return errors.As(err, &deferred)
}

func RetryAfter(err error) (time.Duration, bool) {
	if err == nil {
		return 0, false
	}
	var deferred *DeferredError
	if !errors.As(err, &deferred) || deferred == nil || deferred.RetryAfter <= 0 {
		return 0, false
	}
	return deferred.RetryAfter, true
}
