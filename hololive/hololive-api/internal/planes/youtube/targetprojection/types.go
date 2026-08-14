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

func (b PolicyBuilder) Build(ctx context.Context, tx dbx.Tx, now time.Time) ([]TargetSpec, []TargetReason, error) {
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
	return BuildPolicyTargets(PolicyInputs{
		NotificationChannelIDs: notification,
		OperationalChannelIDs:  operational,
		ViewerVideoIDs:         videos,
	}, b.Schedules)
}

func BuildPolicyTargets(inputs PolicyInputs, schedules map[contract.ObservationKind]Schedule) ([]TargetSpec, []TargetReason, error) {
	if len(inputs.NotificationChannelIDs) > MaxInputChannelCount ||
		len(inputs.OperationalChannelIDs) > MaxInputChannelCount ||
		len(inputs.ViewerVideoIDs) > MaxInputChannelCount {
		return nil, nil, fmt.Errorf("%w: input channel count exceeds %d", ErrInvalidProjection, MaxInputChannelCount)
	}
	notificationKinds := []contract.ObservationKind{
		contract.KindCommunityPage,
		contract.KindVideoList,
		contract.KindShortsList,
	}
	operationalKinds := []contract.ObservationKind{
		contract.KindLiveSnapshot,
		contract.KindChannelStats,
		contract.KindChannelProfile,
		contract.KindChannelPhoto,
	}
	targets := make([]TargetSpec, 0, len(inputs.NotificationChannelIDs)*len(notificationKinds)+len(inputs.OperationalChannelIDs)*len(operationalKinds)+len(inputs.ViewerVideoIDs)+1)
	reasons := make([]TargetReason, 0, cap(targets))
	appendGroup := func(subjectIDs []string, kinds []contract.ObservationKind, reasonKind string) error {
		for _, rawSubject := range subjectIDs {
			subject := strings.TrimSpace(rawSubject)
			if subject == "" {
				return fmt.Errorf("%w: %s subject is empty", ErrInvalidProjection, reasonKind)
			}
			for _, kind := range kinds {
				schedule, ok := schedules[kind]
				if !ok {
					return fmt.Errorf("%w: schedule for %s is missing", ErrInvalidProjection, kind)
				}
				targets = append(targets, TargetSpec{
					SubjectKey: subject, ObservationKind: kind,
					Priority: schedule.Priority, PollInterval: schedule.PollInterval, Enabled: schedule.Enabled,
				})
				reasons = append(reasons, TargetReason{
					SubjectKey: subject, ObservationKind: kind,
					ReasonKind: reasonKind, ReasonKey: subject,
				})
			}
		}
		return nil
	}
	if err := appendGroup(inputs.NotificationChannelIDs, notificationKinds, "notification_target"); err != nil {
		return nil, nil, err
	}
	if err := appendGroup(inputs.OperationalChannelIDs, operationalKinds, "operational_roster"); err != nil {
		return nil, nil, err
	}
	for _, rawVideoID := range inputs.ViewerVideoIDs {
		if looksLikeYouTubeChannelID(rawVideoID) {
			return nil, nil, fmt.Errorf("%w: viewer_sample subject %q is a channel id", ErrInvalidProjection, strings.TrimSpace(rawVideoID))
		}
	}
	if err := appendGroup(inputs.ViewerVideoIDs, []contract.ObservationKind{contract.KindViewerSample}, "viewer_roster"); err != nil {
		return nil, nil, err
	}
	schedule, ok := schedules[contract.KindSchedule]
	if !ok {
		return nil, nil, fmt.Errorf("%w: schedule for %s is missing", ErrInvalidProjection, contract.KindSchedule)
	}
	const globalScheduleSubject = "global:hololive-schedule"
	targets = append(targets, TargetSpec{
		SubjectKey: globalScheduleSubject, ObservationKind: contract.KindSchedule,
		Priority: schedule.Priority, PollInterval: schedule.PollInterval, Enabled: schedule.Enabled,
	})
	reasons = append(reasons, TargetReason{
		SubjectKey: globalScheduleSubject, ObservationKind: contract.KindSchedule,
		ReasonKind: "fixed_global", ReasonKey: globalScheduleSubject,
	})
	return targets, reasons, nil
}

func looksLikeYouTubeChannelID(value string) bool {
	id := strings.TrimSpace(value)
	return strings.HasPrefix(id, "UC") && len(id) >= 22
}
