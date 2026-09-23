package xspaces

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
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
	PublishDispatchBatch(context.Context, []domain.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error)
}

// Runner는 세션 검증, 현재 방송 조회와 LIVE 구독 방 발송 의도를 순차적으로 처리한다.
// 실제 egress·재시도·결과 불명 처리는 기존 dispatch 원장만 소유한다.
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

// NewRunner는 외부 조회를 시작하지 않고 명시적 수집 범위와 의존성을 검증한다.
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

// Start는 DB의 다음 조회 시각을 존중하면서 새 후보를 15초마다 확인한다.
// 조회는 겹치지 않으며 취소 시 helper 회수가 끝난 뒤 반환한다.
func (r *Runner) Start(ctx context.Context) error {
	for ctx.Err() == nil {
		if err := r.RunOnce(ctx); err != nil && ctx.Err() == nil {
			r.logger.Warn("X spaces cycle failed", slog.Any("error", err))
		}

		select {
		case <-ctx.Done():
			return nil
		case <-time.After(15 * time.Second):
		}
	}

	return nil
}

// RunOnce는 검증 성공한 후보만 승격하고 새 세대가 아닌 결과는 폐기한다.
// 활성 세션의 인증 거부는 다음 조회 주기에 한 번 더 확인한 뒤 재연결을 요구한다.
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

	// 같은 만료 세션으로 반복 로그인하거나 계속 요청하지 않는다. 관리자 재연결을 기다린다.
	if snapshot.Active == nil || snapshot.State == "auth_required" {
		return nil
	}

	observations, collectErr := r.collector.Collect(ctx, *snapshot.Active, userIDs)
	// 단일 401/403 뒤 같은 쿠키가 정상 동작한 사례가 있어, 첫 거부만으로 수집을 멈추지 않는다.
	// DB에 대기를 남겨 재시작도 확인 횟수를 초기화하지 못하게 한다.
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
			ChannelID: target.ChannelID, MemberName: target.MemberName, Title: observation.Title, StartedAt: observation.StartedAt,
		}

		if err := payload.Validate(); err != nil {
			return fmt.Errorf("validate X observation: %w", err)
		}

		age := r.now().Sub(payload.StartedAt)
		// 장시간 장애 뒤 오래된 방송을 새 시작 알림으로 보내지 않는다.
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

	envelopes := make([]domain.AlarmQueueEnvelope, 0, len(rooms))
	for _, room := range rooms {
		envelopes = append(envelopes, domain.AlarmQueueEnvelope{
			Notification: domain.AlarmNotification{RoomID: room, AlarmType: domain.AlarmTypeLive},
			SourceKind:   domain.AlarmDispatchSourceKindXSpace, XSpace: &stored,
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
