package livestatus

import (
	"errors"
	"fmt"
	"strings"
)

var ErrDeferred = errors.New("live status deferred")

type DeferredReason string

const (
	DeferredReasonUnknown                       DeferredReason = "unknown"
	DeferredReasonPerCycleCap                   DeferredReason = "per_cycle_cap"
	DeferredReasonWallClockBudget               DeferredReason = "wall_clock_budget"
	DeferredReasonContextDone                   DeferredReason = "context_done"
	DeferredReasonYouTubeCooldown               DeferredReason = "youtube_cooldown"
	DeferredReasonAdmissionDeferred             DeferredReason = "admission_deferred"
	DeferredReasonDistributedLimiterUnavailable DeferredReason = "distributed_limiter_unavailable"
)

type DeferredError struct {
	Reason    DeferredReason
	ChannelID string
	Err       error
}

func NewDeferred(reason DeferredReason, channelID string, err error) error {
	if reason == "" {
		reason = DeferredReasonUnknown
	}
	return &DeferredError{
		Reason:    reason,
		ChannelID: strings.TrimSpace(channelID),
		Err:       err,
	}
}

func (e *DeferredError) Error() string {
	if e == nil {
		return ErrDeferred.Error()
	}
	if e.ChannelID != "" && e.Err != nil {
		return fmt.Sprintf("%s: channel=%s reason=%s: %v", ErrDeferred, e.ChannelID, e.Reason, e.Err)
	}
	if e.ChannelID != "" {
		return fmt.Sprintf("%s: channel=%s reason=%s", ErrDeferred, e.ChannelID, e.Reason)
	}
	if e.Err != nil {
		return fmt.Sprintf("%s: reason=%s: %v", ErrDeferred, e.Reason, e.Err)
	}
	return fmt.Sprintf("%s: reason=%s", ErrDeferred, e.Reason)
}

func (e *DeferredError) Unwrap() error {
	if e == nil {
		return nil
	}
	return e.Err
}

func (e *DeferredError) Is(target error) bool {
	return target == ErrDeferred
}

func (e *DeferredError) LiveStatusDeferred() bool {
	return true
}

type deferredMarker interface {
	LiveStatusDeferred() bool
}

func IsDeferred(err error) bool {
	if err == nil {
		return false
	}
	if errors.Is(err, ErrDeferred) {
		return true
	}
	var marker deferredMarker
	if !errors.As(err, &marker) || marker == nil {
		return false
	}
	return marker.LiveStatusDeferred()
}

func ReasonOf(err error) DeferredReason {
	var deferred *DeferredError
	if errors.As(err, &deferred) && deferred != nil && deferred.Reason != "" {
		return deferred.Reason
	}
	if IsDeferred(err) {
		return DeferredReasonUnknown
	}
	return ""
}
