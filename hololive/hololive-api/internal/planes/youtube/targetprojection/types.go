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
}

type InputReader interface {
	NotificationChannelIDs(ctx context.Context, tx dbx.Tx) ([]string, error)
	OperationalChannelIDs(ctx context.Context, tx dbx.Tx) ([]string, error)
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
	return BuildPolicyTargets(PolicyInputs{
		NotificationChannelIDs: notification,
		OperationalChannelIDs:  operational,
	}, b.Schedules)
}

func BuildPolicyTargets(inputs PolicyInputs, schedules map[contract.ObservationKind]Schedule) ([]TargetSpec, []TargetReason, error) {
	if len(inputs.NotificationChannelIDs) > MaxInputChannelCount || len(inputs.OperationalChannelIDs) > MaxInputChannelCount {
		return nil, nil, fmt.Errorf("%w: input channel count exceeds %d", ErrInvalidProjection, MaxInputChannelCount)
	}
	notificationKinds := []contract.ObservationKind{
		contract.KindCommunityPage,
		contract.KindVideoList,
		contract.KindShortsList,
	}
	operationalKinds := []contract.ObservationKind{
		contract.KindLiveSnapshot,
		contract.KindViewerSample,
		contract.KindChannelStats,
		contract.KindChannelProfile,
		contract.KindChannelPhoto,
	}
	targets := make([]TargetSpec, 0, len(inputs.NotificationChannelIDs)*len(notificationKinds)+len(inputs.OperationalChannelIDs)*len(operationalKinds)+1)
	reasons := make([]TargetReason, 0, cap(targets))
	appendGroup := func(channelIDs []string, kinds []contract.ObservationKind, reasonKind string) error {
		for _, rawChannelID := range channelIDs {
			channelID := strings.TrimSpace(rawChannelID)
			if channelID == "" {
				return fmt.Errorf("%w: %s channel id is empty", ErrInvalidProjection, reasonKind)
			}
			for _, kind := range kinds {
				schedule, ok := schedules[kind]
				if !ok {
					return fmt.Errorf("%w: schedule for %s is missing", ErrInvalidProjection, kind)
				}
				targets = append(targets, TargetSpec{
					SubjectKey: channelID, ObservationKind: kind,
					Priority: schedule.Priority, PollInterval: schedule.PollInterval, Enabled: schedule.Enabled,
				})
				reasons = append(reasons, TargetReason{
					SubjectKey: channelID, ObservationKind: kind,
					ReasonKind: reasonKind, ReasonKey: channelID,
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
