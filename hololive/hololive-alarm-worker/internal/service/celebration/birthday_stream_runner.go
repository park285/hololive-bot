package celebration

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/retry"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/util"
)

const birthdayStreamMaxPublishedPerMemberDay = 3

type BirthdayMemberRepository interface {
	FindMembersWithBirthdayOn(ctx context.Context, month, day int) ([]*domain.Member, error)
}

type BirthdayStreamSession struct {
	VideoID        string
	ChannelID      string
	Title          string
	Status         string
	ScheduledStart *time.Time
	StartedAt      *time.Time
}

type BirthdayStreamRunner struct {
	memberRepo       BirthdayMemberRepository
	sessions         birthdayStreamSessionStore
	publisher        Publisher
	logger           *slog.Logger
	runInterval      time.Duration
	sessionFreshness time.Duration
	now              func() time.Time
	sleep            func(context.Context, time.Duration) bool
}

type BirthdayStreamRunnerConfig struct {
	RunInterval      time.Duration
	SessionFreshness time.Duration
}

func NewBirthdayStreamRunner(
	memberRepo BirthdayMemberRepository,
	sessions birthdayStreamSessionStore,
	publisher Publisher,
	logger *slog.Logger,
	config BirthdayStreamRunnerConfig,
) *BirthdayStreamRunner {
	return &BirthdayStreamRunner{
		memberRepo:       memberRepo,
		sessions:         sessions,
		publisher:        publisher,
		logger:           logger,
		runInterval:      config.RunInterval,
		sessionFreshness: config.SessionFreshness,
	}
}

func (r *BirthdayStreamRunner) Start(ctx context.Context) error {
	for {
		r.logRunFailure(r.RunOnce(ctx))

		if !r.effectiveSleep()(ctx, r.effectiveInterval()) {
			return nil
		}
	}
}

func (r *BirthdayStreamRunner) logRunFailure(err error) {
	if err == nil || r.logger == nil {
		return
	}

	r.logger.Warn("Birthday stream runner failed", slog.Any("error", err))
}

func (r *BirthdayStreamRunner) RunOnce(ctx context.Context) error {
	now := r.effectiveNow()
	dates := birthdayStreamEvaluationDates(now, r.effectiveInterval())
	errs := make([]error, 0, len(dates))

	for _, kstDay := range dates {
		if err := r.runForDate(ctx, now, kstDay); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

func birthdayStreamEvaluationDates(now time.Time, tickInterval time.Duration) []time.Time {
	current := kstDayStart(now)
	previous := kstDayStart(now.Add(-tickInterval))

	if previous.Equal(current) {
		return []time.Time{current}
	}

	return []time.Time{previous, current}
}

func kstDayStart(t time.Time) time.Time {
	kst := util.ToKST(t)
	return time.Date(kst.Year(), kst.Month(), kst.Day(), 0, 0, 0, 0, kst.Location())
}

func (r *BirthdayStreamRunner) runForDate(ctx context.Context, now, kstDay time.Time) error {
	members, err := r.memberRepo.FindMembersWithBirthdayOn(ctx, int(kstDay.Month()), kstDay.Day())
	if err != nil {
		return fmt.Errorf("birthday stream runner: find birthday members: %w", err)
	}

	members = r.filterMembersWithChannel(members)
	if len(members) == 0 {
		return nil
	}

	sessions, err := r.sessions.FindBirthdaySessions(
		ctx,
		memberChannelIDs(members),
		kstDay.UTC(),
		kstDay.Add(24*time.Hour).UTC(),
		now.Add(-r.effectiveFreshness()),
	)
	if err != nil {
		return fmt.Errorf("find birthday sessions: %w", err)
	}

	if len(sessions) == 0 {
		return nil
	}

	dateStr := kstDay.Format(time.DateOnly)

	candidates, err := r.selectPublishableSessions(ctx, members, sessions, dateStr)
	if err != nil {
		return fmt.Errorf("select publishable sessions: %w", err)
	}

	if len(candidates) == 0 {
		return nil
	}

	if err := r.publishCandidates(ctx, candidates, dateStr, len(members)); err != nil {
		return fmt.Errorf("publish candidates: %w", err)
	}

	return nil
}

func (r *BirthdayStreamRunner) publishCandidates(
	ctx context.Context,
	candidates []birthdayStreamCandidate,
	dateStr string,
	memberCount int,
) error {
	birthdayEventKeys := birthdayGreetingEventKeys(candidates, dateStr)

	roomsByEventKey, err := r.sessions.FindSentRoomsByEventKeys(ctx, birthdayEventKeys)
	if err != nil {
		return fmt.Errorf("birthday stream runner: find sent birthday rooms: %w", err)
	}

	if len(roomsByEventKey) == 0 {
		return nil
	}

	envelopes := buildBirthdayStreamEnvelopes(candidates, roomsByEventKey, dateStr)
	if len(envelopes) == 0 {
		return nil
	}

	result, err := r.publisher.PublishDispatchBatch(ctx, envelopes)
	if err != nil {
		return fmt.Errorf("birthday stream runner: publish dispatch batch: %w", err)
	}

	if r.logger != nil {
		r.logger.Info("Birthday stream runner published",
			slog.String("date", dateStr),
			slog.Int("birthday_members", memberCount),
			slog.Int("videos", len(candidates)),
			slog.Int("eligible_rooms", countBirthdayStreamAudienceRooms(roomsByEventKey)),
			slog.Int("envelopes", len(envelopes)),
			slog.Int("inserted_events", result.InsertedEvents),
			slog.Int("inserted_deliveries", result.InsertedDeliveries),
			slog.Int("duplicate_events", result.DuplicateEvents),
		)
	}

	return nil
}

func (r *BirthdayStreamRunner) filterMembersWithChannel(members []*domain.Member) []*domain.Member {
	withChannel := make([]*domain.Member, 0, len(members))
	for _, m := range members {
		if strings.TrimSpace(m.ChannelID) == "" {
			if r.logger != nil {
				r.logger.Debug("Birthday stream runner skipped member without YouTube channel",
					slog.String("member", resolveCelebrationMemberName(m)))
			}

			continue
		}

		withChannel = append(withChannel, m)
	}

	return withChannel
}

func memberChannelIDs(members []*domain.Member) []string {
	channelIDs := make([]string, 0, len(members))
	for _, m := range members {
		channelIDs = append(channelIDs, m.ChannelID)
	}

	return channelIDs
}

type birthdayStreamCandidate struct {
	member  *domain.Member
	session BirthdayStreamSession
}

func (r *BirthdayStreamRunner) selectPublishableSessions(
	ctx context.Context,
	members []*domain.Member,
	sessions []BirthdayStreamSession,
	dateStr string,
) ([]birthdayStreamCandidate, error) {
	byChannel := make(map[string][]BirthdayStreamSession, len(members))

	for _, session := range sessions {
		byChannel[session.ChannelID] = append(byChannel[session.ChannelID], session)
	}

	candidates := make([]birthdayStreamCandidate, 0, len(sessions))

	for _, m := range members {
		channelSessions := byChannel[m.ChannelID]
		if len(channelSessions) == 0 {
			continue
		}

		selected, err := r.selectNewSessionsForMember(ctx, m.ChannelID, channelSessions, dateStr)
		if err != nil {
			return nil, fmt.Errorf("select new sessions for member: %w", err)
		}

		for _, session := range selected {
			candidates = append(candidates, birthdayStreamCandidate{member: m, session: session})
		}
	}

	return candidates, nil
}

func (r *BirthdayStreamRunner) selectNewSessionsForMember(
	ctx context.Context,
	channelID string,
	sessions []BirthdayStreamSession,
	dateStr string,
) ([]BirthdayStreamSession, error) {
	publishedKeys, err := r.sessions.ListPublishedEventKeys(ctx, birthdayStreamEventKeyPrefix(channelID, dateStr))
	if err != nil {
		return nil, fmt.Errorf("list published event keys: %w", err)
	}

	published := make(map[string]struct{}, len(publishedKeys))
	for _, key := range publishedKeys {
		published[key] = struct{}{}
	}

	return selectBirthdayStreamSessionsWithinDailyCap(channelID, dateStr, sessions, published), nil
}

func selectBirthdayStreamSessionsWithinDailyCap(
	channelID string,
	dateStr string,
	sessions []BirthdayStreamSession,
	published map[string]struct{},
) []BirthdayStreamSession {
	remaining := max(birthdayStreamMaxPublishedPerMemberDay-len(published), 0)

	ordered := append([]BirthdayStreamSession(nil), sessions...)
	sortBirthdayStreamSessions(ordered)

	selected := make([]BirthdayStreamSession, 0, min(len(ordered), birthdayStreamMaxPublishedPerMemberDay))
	for _, session := range ordered {
		if _, ok := published[birthdayStreamEventKey(channelID, dateStr, session.VideoID)]; ok {
			if len(selected) < birthdayStreamMaxPublishedPerMemberDay {
				selected = append(selected, session)
			}

			continue
		}

		if remaining > 0 && len(selected) < birthdayStreamMaxPublishedPerMemberDay {
			selected = append(selected, session)
			remaining--
		}
	}

	return selected
}

func sortBirthdayStreamSessions(sessions []BirthdayStreamSession) {
	slices.SortStableFunc(sessions, func(left, right BirthdayStreamSession) int {
		return cmp.Or(
			birthdayStreamEffectiveStart(&left).Compare(birthdayStreamEffectiveStart(&right)),
			cmp.Compare(left.VideoID, right.VideoID),
		)
	})
}

func birthdayStreamEffectiveStart(session *BirthdayStreamSession) time.Time {
	if start := util.FirstNonNilTime(session.StartedAt, session.ScheduledStart); start != nil {
		return *start
	}

	return time.Time{}
}

func (r *BirthdayStreamRunner) effectiveInterval() time.Duration {
	if r.runInterval > 0 {
		return r.runInterval
	}

	return 30 * time.Minute
}

func (r *BirthdayStreamRunner) effectiveFreshness() time.Duration {
	if r.sessionFreshness > 0 {
		return r.sessionFreshness
	}

	return 30 * time.Minute
}

func (r *BirthdayStreamRunner) effectiveNow() time.Time {
	if r.now != nil {
		return r.now()
	}

	return util.NowKST()
}

func (r *BirthdayStreamRunner) effectiveSleep() func(context.Context, time.Duration) bool {
	if r.sleep != nil {
		return r.sleep
	}

	return retry.Sleep
}
