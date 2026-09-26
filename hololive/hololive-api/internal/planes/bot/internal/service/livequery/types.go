// Package livequery는 확정된 YouTube 방송과 소비된 coverage의 읽기 계약을 소유한다.
package livequery

import (
	"context"
	"time"
)

const MaxItems = 100

type Scope string

const (
	All    Scope = "all"
	Member Scope = "member"
)

type Request struct {
	Scope      Scope
	ChannelID  string
	MemberName string
	Limit      int
}

type Status string

const (
	Complete    Status = "complete"
	Partial     Status = "partial"
	Unavailable Status = "unavailable"
)

type Reason string

const (
	Covered           Reason = "covered"
	InvalidTarget     Reason = "invalid_target"
	InvalidProjection Reason = "invalid_projection"
	Uncollected       Reason = "uncollected"
	Incomplete        Reason = "incomplete"
	Stale             Reason = "stale"
	InvalidClock      Reason = "invalid_clock"
	Inconsistent      Reason = "inconsistent"
	ConfirmingEnd     Reason = "confirming_end"
)

type Item struct {
	VideoID     string     `json:"video_id"`
	ChannelID   string     `json:"channel_id"`
	ChannelName string     `json:"channel_name"`
	Org         string     `json:"org"`
	Title       string     `json:"title"`
	StartedAt   *time.Time `json:"started_at"`
	ObservedAt  time.Time  `json:"observed_at"`
}

type Channel struct {
	ChannelID string     `json:"channel_id"`
	Reason    Reason     `json:"reason"`
	CoveredAt *time.Time `json:"covered_at"`
}

type Result struct {
	Items     []Item
	AsOf      time.Time
	Status    Status
	Channels  []Channel
	Truncated bool
}

// ReasonCounts는 채널별 조회 사유를 운영 진단용 개수로 모은다.
func (r Result) ReasonCounts() map[Reason]int {
	counts := make(map[Reason]int, len(r.Channels))
	for _, channel := range r.Channels {
		counts[channel.Reason]++
	}

	return counts
}

type Reader interface {
	Query(ctx context.Context, request Request) (Result, error)
}
