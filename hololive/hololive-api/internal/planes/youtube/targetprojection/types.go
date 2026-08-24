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
	MaxTargetCount       = 10_000
	MaxReasonCount       = 50_000
	MaxInputChannelCount = 10_000
	MinValidity          = 5 * time.Second
	MaxValidity          = 24 * time.Hour
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
	ViewerVideoIDs         []string
}

type InputReader interface {
	NotificationChannelIDs(ctx context.Context, tx dbx.Tx) ([]string, error)
	OperationalChannelIDs(ctx context.Context, tx dbx.Tx) ([]string, error)
	ViewerVideoIDs(ctx context.Context, tx dbx.Tx) ([]string, error)
}

type PolicyBuilder struct {
	Reader    InputReader
	Schedules map[contract.ObservationKind]Schedule
}

func (b PolicyBuilder) Build(ctx context.Context, tx dbx.Tx, _ time.Time) ([]TargetSpec, []TargetReason, error) {
	if b.Reader == nil {
		return nil, nil, fmt.Errorf("%w: input reader is not configured", ErrInputRead)
	}

	notification, err := b.Reader.NotificationChannelIDs(ctx, tx)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: load notification channels: %w", ErrInputRead, err)
	}

	operational, err := b.Reader.OperationalChannelIDs(ctx, tx)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: load operational channels: %w", ErrInputRead, err)
	}

	videos, err := b.Reader.ViewerVideoIDs(ctx, tx)
	if err != nil {
		return nil, nil, fmt.Errorf("%w: load viewer videos: %w", ErrInputRead, err)
	}

	out1, out2, err := BuildPolicyTargets(PolicyInputs{
		NotificationChannelIDs: notification,
		OperationalChannelIDs:  operational,
		ViewerVideoIDs:         videos,
	}, b.Schedules)
	if err != nil {
		return out1, out2, fmt.Errorf("build policy targets: %w", err)
	}

	return out1, out2, nil
}

func BuildPolicyTargets(inputs PolicyInputs, schedules map[contract.ObservationKind]Schedule) ([]TargetSpec, []TargetReason, error) {
	if policyInputOverflow(inputs) {
		return nil, nil, fmt.Errorf("%w: input channel count exceeds %d", ErrInvalidProjection, MaxInputChannelCount)
	}

	builder := newPolicyTargetBuilder(schedules)
	if err := builder.appendGroup(inputs.NotificationChannelIDs, notificationPolicyKinds(), "notification_target"); err != nil {
		return nil, nil, fmt.Errorf("append group: %w", err)
	}

	if err := builder.appendGroup(inputs.OperationalChannelIDs, operationalPolicyKinds(), "operational_roster"); err != nil {
		return nil, nil, fmt.Errorf("append group: %w", err)
	}

	if err := rejectChannelViewerSubjects(inputs.ViewerVideoIDs); err != nil {
		return nil, nil, fmt.Errorf("reject channel viewer subjects: %w", err)
	}

	if err := builder.appendGroup(inputs.ViewerVideoIDs, []contract.ObservationKind{contract.KindViewerSample}, "viewer_roster"); err != nil {
		return nil, nil, fmt.Errorf("append group: %w", err)
	}

	if err := builder.appendGlobalSchedule(); err != nil {
		return nil, nil, fmt.Errorf("append global schedule: %w", err)
	}

	return builder.targets, builder.reasons, nil
}

func policyInputOverflow(inputs PolicyInputs) bool {
	return len(inputs.NotificationChannelIDs) > MaxInputChannelCount ||
		len(inputs.OperationalChannelIDs) > MaxInputChannelCount ||
		len(inputs.ViewerVideoIDs) > MaxInputChannelCount
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
		contract.KindChannelStats,
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
			ReasonKind: reasonKind, ReasonKey: subject,
		})
	}

	return nil
}

func rejectChannelViewerSubjects(videoIDs []string) error {
	for _, rawVideoID := range videoIDs {
		if looksLikeYouTubeChannelID(rawVideoID) {
			return fmt.Errorf("%w: viewer_sample subject %q is a channel id", ErrInvalidProjection, strings.TrimSpace(rawVideoID))
		}
	}

	return nil
}

func (b *policyTargetBuilder) appendGlobalSchedule() error {
	schedule, ok := b.schedules[contract.KindSchedule]
	if !ok {
		return fmt.Errorf("%w: schedule for %s is missing", ErrInvalidProjection, contract.KindSchedule)
	}

	const globalScheduleSubject = "global:hololive-schedule"

	b.targets = append(b.targets, TargetSpec{
		SubjectKey: globalScheduleSubject, ObservationKind: contract.KindSchedule,
		Priority: schedule.Priority, PollInterval: schedule.PollInterval, Enabled: schedule.Enabled,
	})
	b.reasons = append(b.reasons, TargetReason{
		SubjectKey: globalScheduleSubject, ObservationKind: contract.KindSchedule,
		ReasonKind: "fixed_global", ReasonKey: globalScheduleSubject,
	})

	return nil
}

func looksLikeYouTubeChannelID(value string) bool {
	id := strings.TrimSpace(value)
	return strings.HasPrefix(id, "UC") && len(id) >= 22
}
