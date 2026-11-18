package xspaces

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/park285/shared-go/v2/pkg/retry"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
	sessions "github.com/kapu/hololive-shared/pkg/service/xspaces"
)

type sessionStore interface {
	Snapshot(context.Context) (sessions.Snapshot, error)
	ResolveCandidate(context.Context, int64, string) (bool, error)
	CandidateFailure(context.Context, int64, string, time.Time) error
	Observe(context.Context, int64, string, time.Time) (bool, error)
}
type startStore interface {
	Remember(context.Context, domain.XSpaceDispatchPayload) (domain.XSpaceDispatchPayload, error)
	Prune(context.Context) error
}
type publisher interface {
	PublishDispatchBatch(context.Context, []dispatchoutbox.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error)
}

type Runner struct {
	config    Config
	sessions  sessionStore
	starts    startStore
	collector Collector
	publisher publisher
	rooms     func(context.Context, string) ([]string, error)
	logger    *slog.Logger
	now       func() time.Time
}

func NewRunner(config Config, store sessionStore, starts startStore, collector Collector, pub publisher,
	rooms func(context.Context, string) ([]string, error), logger *slog.Logger,
) (*Runner, error) {
	if err := config.Validate(); err != nil {
		return nil, err
	}

	if store == nil || starts == nil || collector == nil || pub == nil || rooms == nil || logger == nil {
		return nil, errors.New("x space runner dependencies are required")
	}

	return &Runner{config: config, sessions: store, starts: starts, collector: collector, publisher: pub, rooms: rooms, logger: logger, now: time.Now}, nil
}

func (r *Runner) Start(ctx context.Context) error {
	for ctx.Err() == nil {
		if err := r.RunOnce(ctx); err != nil && ctx.Err() == nil {
			r.logger.Warn("X spaces cycle failed", slog.Any("error", err))
		}

		if !retry.Sleep(ctx, 15*time.Second) {
			return nil
		}
	}

	return nil
}

func (r *Runner) RunOnce(ctx context.Context) error {
	snapshot, err := r.sessions.Snapshot(ctx)
	if err != nil {
		return fmt.Errorf("load X session: %w", err)
	}

	now := r.now()
	if snapshot.NextCheckAt != nil && now.Before(*snapshot.NextCheckAt) {
		return nil
	}

	userIDs := make([]string, 0, len(r.config.Targets))
	for _, target := range r.config.Targets {
		userIDs = append(userIDs, target.UserID)
	}

	if snapshot.Candidate != nil {
		done, err := r.tryCandidate(ctx, snapshot, userIDs)
		if done || err != nil {
			return err
		}
	}

	if snapshot.Active == nil || snapshot.State == "auth_required" {
		return nil
	}

	observations, collectErr := r.collector.Collect(ctx, *snapshot.Active, userIDs)
	if failure, ok := errors.AsType[*CollectionError](collectErr); ok && failure.Code == "authentication" && snapshot.LastError != "authentication_pending" {
		pending := *failure

		pending.Code = "authentication_pending"
		collectErr = &pending
	}

	return r.finish(ctx, snapshot.ActiveRevision, observations, collectErr)
}

func (r *Runner) tryCandidate(ctx context.Context, snapshot sessions.Snapshot, userIDs []string) (bool, error) {
	observations, collectErr := r.collector.Collect(ctx, *snapshot.Candidate, userIDs)
	if collectErr == nil {
		promoted, err := r.sessions.ResolveCandidate(ctx, snapshot.Revision, "")
		if err != nil {
			return true, fmt.Errorf("promote X session: %w", err)
		}

		if !promoted {
			return true, nil
		}

		return true, r.finish(ctx, snapshot.Revision, observations, nil)
	}

	code, cooldown := collectionFailure(collectErr)
	if code != "authentication" {
		return true, errors.Join(collectErr, r.sessions.CandidateFailure(ctx, snapshot.Revision, code, r.now().Add(max(r.interval(), cooldown))))
	}

	resolved, err := r.sessions.ResolveCandidate(ctx, snapshot.Revision, code)
	if err != nil {
		return true, fmt.Errorf("reject X session candidate: %w", err)
	}

	if !resolved {
		return true, nil
	}

	return false, nil
}

func (r *Runner) interval() time.Duration { return time.Duration(r.config.PollSeconds) * time.Second }

func (r *Runner) finish(ctx context.Context, revision int64, observations []Observation, collectErr error) error {
	code, cooldown := "", time.Duration(0)

	if collectErr != nil {
		code, cooldown = collectionFailure(collectErr)
	}

	current, err := r.sessions.Observe(ctx, revision, code, r.now().Add(max(r.interval(), cooldown)))
	if err != nil {
		return fmt.Errorf("record X observation: %w", err)
	}

	if !current {
		return nil
	}

	if collectErr != nil {
		return collectErr
	}

	if err := r.publish(ctx, observations); err != nil {
		return err
	}

	if err := r.starts.Prune(ctx); err != nil {
		return fmt.Errorf("prune X start snapshots: %w", err)
	}

	return nil
}

func (r *Runner) publish(ctx context.Context, observations []Observation) error {
	targets := make(map[string]Target, len(r.config.Targets))
	for _, target := range r.config.Targets {
		targets[target.UserID] = target
	}

	seen := make(map[string]bool, len(observations))
	for _, observation := range observations {
		target, ok := targets[observation.CreatorID]
		if !ok || seen[observation.SpaceID] {
			return errors.New("x observation contains unexpected or duplicate identity")
		}

		seen[observation.SpaceID] = true

		payload := domain.XSpaceDispatchPayload{
			SpaceID: observation.SpaceID, CreatorID: observation.CreatorID,
			ChannelID: target.ChannelID, Title: observation.Title, StartedAt: observation.StartedAt,
		}

		if err := payload.Validate(); err != nil {
			return fmt.Errorf("validate X observation: %w", err)
		}

		age := r.now().Sub(payload.StartedAt)
		if age < -time.Minute || age > 15*time.Minute {
			continue
		}

		if err := r.publishStart(ctx, payload); err != nil {
			return err
		}
	}

	return nil
}

func (r *Runner) publishStart(ctx context.Context, payload domain.XSpaceDispatchPayload) error {
	stored, err := r.starts.Remember(ctx, payload)
	if err != nil {
		return fmt.Errorf("remember X observation: %w", err)
	}

	rooms, err := r.rooms(ctx, stored.ChannelID)
	if err != nil {
		return fmt.Errorf("resolve X subscription rooms: %w", err)
	}

	envelopes := make([]dispatchoutbox.AlarmQueueEnvelope, 0, len(rooms))
	for _, room := range rooms {
		envelopes = append(envelopes, dispatchoutbox.AlarmQueueEnvelope{
			Notification: domain.AlarmNotification{RoomID: room, AlarmType: domain.AlarmTypeLive},
			SourceKind:   domain.AlarmDispatchSourceKindXSpace,
			XSpace:       &stored,
			Version:      contractsalarm.QueueEnvelopeVersionV1,
		})
	}

	if len(envelopes) == 0 {
		return nil
	}

	result, err := r.publisher.PublishDispatchBatch(ctx, envelopes)
	if err != nil {
		return fmt.Errorf("publish X space start: %w", err)
	}

	r.logger.Info("X space start admitted", slog.Int("inserted_deliveries", result.InsertedDeliveries), slog.Int("duplicate_events", result.DuplicateEvents))

	return nil
}
