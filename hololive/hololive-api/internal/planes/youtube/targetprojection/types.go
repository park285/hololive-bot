package targetprojection

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/dbx"
)

const (
	MaxTargetCount       = contract.MaxProjectionTargetCount
	MaxReasonCount       = 50_000
	MaxInputChannelCount = 10_000
	MinValidity          = 5 * time.Second
	MaxValidity          = 24 * time.Hour

	// MaxInputLiveCheckVideoCount는 한 projection이 받는 영상 확인 구조 membership 상한입니다.
	// 초과는 임의 절단 없이 입력 이상으로 거부하고 last-good generation을 유지합니다.
	MaxInputLiveCheckVideoCount = 1_000

	// LIVE positive 신선도 예산은 min(5분, 2×live_snapshot poll interval+30초)이며 LiveQuery와 같은 경계입니다.
	maxLiveFreshnessBudget   = 5 * time.Minute
	liveFreshnessBudgetSlack = 30 * time.Second

	liveCheckVideoReasonKind = "live_session_check"
)

var (
	ErrInvalidProjection = errors.New("youtube target projection is invalid")
	ErrInputRead         = errors.New("youtube target projection input read failed")
)

// TargetSpec의 scheduling 필드(priority, poll interval, enabled)는 projection identity입니다.
// NotBefore는 identity·hash에 포함하지 않는 다음 신규 확인 가능 시각이며, zero 값은 NULL(즉시 가능)입니다.
type TargetSpec struct {
	SubjectKey      string
	ObservationKind contract.ObservationKind
	Priority        int16
	PollInterval    time.Duration
	Enabled         bool
	NotBefore       time.Time
}

type TargetReason struct {
	SubjectKey      string
	ObservationKind contract.ObservationKind
	ReasonKind      string
	ReasonKey       string
}

type Builder interface {
	Build(ctx context.Context, tx dbx.Tx, now time.Time) ([]TargetSpec, []TargetReason, error)
}

type Schedule struct {
	Priority     int16
	PollInterval time.Duration
	Enabled      bool
}

type PolicyInputs struct {
	NotificationChannelIDs []string
	OperationalChannelIDs  []string
	LiveCheckVideos        []LiveCheckVideo
}

// LiveCheckVideo는 운영 roster의 LIVE 또는 지난 일정·출처 미상 UPCOMING 영상의 구조 membership입니다.
// 신선도는 membership을 바꾸지 않고 NotBefore로만 표현합니다. NotBefore가 zero면 받아들인 신선도 근거가 없다는 뜻입니다.
type LiveCheckVideo struct {
	VideoID    string
	ChannelID  string
	IsUpcoming bool
	NotBefore  time.Time
}

// LiveCheckVideoQuery는 같은 projection transaction에서 읽은 운영 roster와 신선도 예산을 전달합니다.
// 판정 시각은 조회 statement의 DB 시각이며, 그보다 미래인 근거 시각은 신선도로 받아들이지 않습니다.
type LiveCheckVideoQuery struct {
	OperationalChannelIDs []string
	FreshnessBudget       time.Duration
}

type InputReader interface {
	NotificationChannelIDs(ctx context.Context, tx dbx.Tx) ([]string, error)
	OperationalChannelIDs(ctx context.Context, tx dbx.Tx) ([]string, error)
	LiveCheckVideos(ctx context.Context, tx dbx.Tx, query LiveCheckVideoQuery) ([]LiveCheckVideo, error)
}

type PolicyBuilder struct {
	Reader    InputReader
	Schedules map[contract.ObservationKind]Schedule
}

// Build는 영상 확인 신선도를 조회 시점 DB 시각에 맡기므로 refresh 시각을 입력으로 쓰지 않습니다.
func (b PolicyBuilder) Build(ctx context.Context, tx dbx.Tx, _ time.Time) ([]TargetSpec, []TargetReason, error) {
	if b.Reader == nil {
		return nil, nil, fmt.Errorf("%w: input reader is not configured", ErrInputRead)
	}

	liveSchedule, ok := b.Schedules[contract.KindLiveSnapshot]
	if !ok {
		return nil, nil, fmt.Errorf("%w: schedule for %s is missing", ErrInvalidProjection, contract.KindLiveSnapshot)
	}

	notification, err := b.Reader.NotificationChannelIDs(ctx, tx)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: load notification channels: %w", ErrInputRead, err)
	}

	operational, err := b.Reader.OperationalChannelIDs(ctx, tx)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: load operational channels: %w", ErrInputRead, err)
	}

	videos, err := b.Reader.LiveCheckVideos(ctx, tx, LiveCheckVideoQuery{
		OperationalChannelIDs: operational,
		FreshnessBudget:       LiveFreshnessBudget(liveSchedule.PollInterval),
	})
	if err != nil {
		return nil, nil, fmt.Errorf("%w: load live check videos: %w", ErrInputRead, err)
	}

	out1, out2, err := BuildPolicyTargets(PolicyInputs{
		NotificationChannelIDs: notification,
		OperationalChannelIDs:  operational,
		LiveCheckVideos:        videos,
	}, b.Schedules)
	if err != nil {
		return out1, out2, fmt.Errorf("build policy targets: %w", err)
	}

	return out1, out2, nil
}

// LiveFreshnessBudget는 live_snapshot poll interval에서 LIVE positive 신선도 예산을 계산합니다.
func LiveFreshnessBudget(livePollInterval time.Duration) time.Duration {
	return min(maxLiveFreshnessBudget, 2*livePollInterval+liveFreshnessBudgetSlack)
}

func BuildPolicyTargets(inputs PolicyInputs, schedules map[contract.ObservationKind]Schedule) ([]TargetSpec, []TargetReason, error) {
	if policyInputOverflow(inputs) {
		return nil, nil, fmt.Errorf("%w: input channel count exceeds %d", ErrInvalidProjection, MaxInputChannelCount)
	}

	if len(inputs.LiveCheckVideos) > MaxInputLiveCheckVideoCount {
		return nil, nil, fmt.Errorf("%w: live check video count exceeds %d", ErrInvalidProjection, MaxInputLiveCheckVideoCount)
	}

	builder := newPolicyTargetBuilder(schedules)
	if err := builder.appendGroup(inputs.NotificationChannelIDs, notificationPolicyKinds(), "notification_target"); err != nil {
		return nil, nil, fmt.Errorf("append group: %w", err)
	}

	if err := builder.appendGroup(inputs.OperationalChannelIDs, operationalPolicyKinds(), "operational_roster"); err != nil {
		return nil, nil, fmt.Errorf("append group: %w", err)
	}

	if err := builder.appendLiveCheckVideos(inputs.LiveCheckVideos, inputs.OperationalChannelIDs); err != nil {
		return nil, nil, fmt.Errorf("append live check videos: %w", err)
	}

	if err := builder.appendGlobalSchedule(); err != nil {
		return nil, nil, fmt.Errorf("append global schedule: %w", err)
	}

	return builder.targets, builder.reasons, nil
}

func policyInputOverflow(inputs PolicyInputs) bool {
	return len(inputs.NotificationChannelIDs) > MaxInputChannelCount ||
		len(inputs.OperationalChannelIDs) > MaxInputChannelCount
}

func notificationPolicyKinds() []contract.ObservationKind {
	return []contract.ObservationKind{
		contract.KindCommunityPage,
		contract.KindVideoList,
		contract.KindShortsList,
	}
}

func operationalPolicyKinds() []contract.ObservationKind {
	return []contract.ObservationKind{
		contract.KindLiveSnapshot,
		contract.KindChannelLiveCheck,
		contract.KindChannelProfile,
		contract.KindChannelPhoto,
	}
}

type policyTargetBuilder struct {
	schedules map[contract.ObservationKind]Schedule
	targets   []TargetSpec
	reasons   []TargetReason
}

func newPolicyTargetBuilder(schedules map[contract.ObservationKind]Schedule) *policyTargetBuilder {
	return &policyTargetBuilder{schedules: schedules, targets: make([]TargetSpec, 0), reasons: make([]TargetReason, 0)}
}

func (b *policyTargetBuilder) appendGroup(subjectIDs []string, kinds []contract.ObservationKind, reasonKind string) error {
	for _, rawSubject := range subjectIDs {
		subject := strings.TrimSpace(rawSubject)
		if subject == "" {
			return fmt.Errorf("%w: %s subject is empty", ErrInvalidProjection, reasonKind)
		}

		if err := b.appendSubjectKinds(subject, kinds, reasonKind); err != nil {
			return fmt.Errorf("append subject kinds: %w", err)
		}
	}

	return nil
}

func (b *policyTargetBuilder) appendSubjectKinds(subject string, kinds []contract.ObservationKind, reasonKind string) error {
	for _, kind := range kinds {
		if err := b.appendTarget(subject, kind, reasonKind, subject); err != nil {
			return fmt.Errorf("append target: %w", err)
		}
	}

	return nil
}

// appendLiveCheckVideos는 운영 roster 채널의 영상별 수명 확인 target을 만듭니다.
// 근거 key는 영상이 속한 canonical 채널이며, roster 밖 채널의 영상은 입력 오류로 거부합니다.
// 신선도 근거는 NotBefore로만 전달해 확인 직후의 출입이 projection identity를 바꾸지 않게 합니다.
func (b *policyTargetBuilder) appendLiveCheckVideos(videos []LiveCheckVideo, operationalChannelIDs []string) error {
	if len(videos) == 0 {
		return nil
	}

	roster := make(map[string]struct{}, len(operationalChannelIDs))
	for _, channelID := range operationalChannelIDs {
		roster[strings.TrimSpace(channelID)] = struct{}{}
	}

	for _, video := range videos {
		videoID := strings.TrimSpace(video.VideoID)
		channelID := strings.TrimSpace(video.ChannelID)

		if videoID == "" || channelID == "" {
			return fmt.Errorf("%w: live check video identity is empty", ErrInvalidProjection)
		}

		if _, ok := roster[channelID]; !ok {
			return fmt.Errorf("%w: live check video %s channel is outside the operational roster", ErrInvalidProjection, videoID)
		}

		if err := b.appendTarget(videoID, contract.KindVideoLiveCheck, liveCheckVideoReasonKind, channelID); err != nil {
			return fmt.Errorf("append target: %w", err)
		}

		target := &b.targets[len(b.targets)-1]

		target.NotBefore = video.NotBefore

		if video.IsUpcoming {
			// 같은 영상 확인 budget에서 LIVE가 먼저 실행되도록 UPCOMING 대상만 낮춘다.
			// LIVE↔UPCOMING 전환은 실제 상태 변경이므로 이 target의 membership을 새로 시작한다.
			target.Priority = max(target.Priority-1, 0)
		}
	}

	return nil
}

func (b *policyTargetBuilder) appendGlobalSchedule() error {
	const globalScheduleSubject = "global:hololive-schedule"

	if err := b.appendTarget(globalScheduleSubject, contract.KindSchedule, "fixed_global", globalScheduleSubject); err != nil {
		return fmt.Errorf("append target: %w", err)
	}

	return nil
}

func (b *policyTargetBuilder) appendTarget(subject string, kind contract.ObservationKind, reasonKind, reasonKey string) error {
	schedule, ok := b.schedules[kind]
	if !ok {
		return fmt.Errorf("%w: schedule for %s is missing", ErrInvalidProjection, kind)
	}

	b.targets = append(b.targets, TargetSpec{
		SubjectKey: subject, ObservationKind: kind,
		Priority: schedule.Priority, PollInterval: schedule.PollInterval, Enabled: schedule.Enabled,
	})
	b.reasons = append(b.reasons, TargetReason{
		SubjectKey: subject, ObservationKind: kind,
		ReasonKind: reasonKind, ReasonKey: reasonKey,
	})

	return nil
}
