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

	MaxInputLiveCheckVideoCount = 1_000

	maxLiveFreshnessBudget   = 5 * time.Minute
	liveFreshnessBudgetSlack = 30 * time.Second

	liveCheckVideoReasonKind   = "live_session_check"
	timeFinalizationReasonKind = "archive_time_finalization"
	timeFinalizationPriority   = 18
)

var (
	ErrInvalidProjection = errors.New("youtube target projection is invalid")
	ErrInputRead         = errors.New("youtube target projection input read failed")
)

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
	TimeFinalizationVideos []TimeFinalizationVideo
}

type LiveCheckVideo struct {
	VideoID         string
	ChannelID       string
	IsUpcoming      bool
	NotBefore       time.Time
	UnresolvableFor time.Duration
}

type TimeFinalizationVideo struct {
	VideoID   string
	ChannelID string
	NotBefore time.Time
}

type LiveCheckVideoQuery struct {
	OperationalChannelIDs []string
	FreshnessBudget       time.Duration
}

type InputReader interface {
	NotificationChannelIDs(ctx context.Context, tx dbx.Tx) ([]string, error)
	OperationalChannelIDs(ctx context.Context, tx dbx.Tx) ([]string, error)
	LiveCheckVideos(ctx context.Context, tx dbx.Tx, query LiveCheckVideoQuery) ([]LiveCheckVideo, error)
	TimeFinalizationVideos(ctx context.Context, tx dbx.Tx) ([]TimeFinalizationVideo, error)
}

type PolicyBuilder struct {
	Reader    InputReader
	Schedules map[contract.ObservationKind]Schedule
}

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

	finalizations, err := b.Reader.TimeFinalizationVideos(ctx, tx)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: load time finalization videos: %w", ErrInputRead, err)
	}

	out1, out2, err := BuildPolicyTargets(PolicyInputs{
		NotificationChannelIDs: notification,
		OperationalChannelIDs:  operational,
		LiveCheckVideos:        videos,
		TimeFinalizationVideos: finalizations,
	}, b.Schedules)
	if err != nil {
		return out1, out2, fmt.Errorf("build policy targets: %w", err)
	}

	return out1, out2, nil
}

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

	if err := builder.appendTimeFinalizationVideos(inputs.TimeFinalizationVideos, inputs.LiveCheckVideos); err != nil {
		return nil, nil, fmt.Errorf("append time finalization videos: %w", err)
	}

	if err := builder.appendGlobalSchedule(); err != nil {
		return nil, nil, fmt.Errorf("append global schedule: %w", err)
	}

	if err := validateProjectionCounts(builder.targets, builder.reasons); err != nil {
		return nil, nil, fmt.Errorf("validate policy target counts: %w", err)
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
			target.Priority = max(target.Priority-1, 0)
			target.PollInterval = unresolvablePollInterval(target.PollInterval, video.UnresolvableFor)
		}
	}

	return nil
}

func (b *policyTargetBuilder) appendTimeFinalizationVideos(videos []TimeFinalizationVideo, liveVideos []LiveCheckVideo) error {
	if len(videos) == 0 {
		return nil
	}

	liveIDs := make(map[string]struct{}, len(liveVideos))

	for _, video := range liveVideos {
		liveIDs[strings.TrimSpace(video.VideoID)] = struct{}{}
	}

	for _, video := range videos {
		videoID := strings.TrimSpace(video.VideoID)
		channelID := strings.TrimSpace(video.ChannelID)

		if videoID == "" || channelID == "" {
			return fmt.Errorf("%w: time finalization video identity is empty", ErrInvalidProjection)
		}

		if _, exists := liveIDs[videoID]; exists {
			continue
		}

		if err := b.appendTarget(videoID, contract.KindVideoLiveCheck, timeFinalizationReasonKind, channelID); err != nil {
			return fmt.Errorf("append target: %w", err)
		}

		target := &b.targets[len(b.targets)-1]

		target.Priority = timeFinalizationPriority
		target.NotBefore = video.NotBefore
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
