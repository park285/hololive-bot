// Package livequery는 확정된 YouTube 방송과 검증된 채널 확인 사실의 읽기 계약을 소유한다.
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

// Diagnostics는 현재 후보에서 제외하되 운영 조사에 보존하는 종료 증거 개수다.
type Diagnostics struct {
	RetainedOrphanEnds  int `json:"retained_orphan_ends"`
	EndedPendingEnds    int `json:"ended_pending_ends"`
	EndedHeadMismatches int `json:"ended_head_mismatches"`
}

type Channel struct {
	ChannelID   string      `json:"channel_id"`
	Reason      Reason      `json:"reason"`
	CoveredAt   *time.Time  `json:"covered_at"`
	Diagnostics Diagnostics `json:"diagnostics"`
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

// DiagnosticCounts는 완전성 판정을 바꾸지 않는 보존 증거를 채널 간 합산한다.
// 채널이 어긋난 ENDED pending은 두 관련 채널에 각각 남으므로 합계는 고유 DB 행 수가 아니다.
func (r Result) DiagnosticCounts() Diagnostics {
	var counts Diagnostics

	for _, channel := range r.Channels {
		counts.RetainedOrphanEnds += channel.Diagnostics.RetainedOrphanEnds
		counts.EndedPendingEnds += channel.Diagnostics.EndedPendingEnds
		counts.EndedHeadMismatches += channel.Diagnostics.EndedHeadMismatches
	}

	return counts
}

type Reader interface {
	Query(ctx context.Context, request Request) (Result, error)
}
