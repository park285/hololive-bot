package celebration

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/util"

	"github.com/park285/shared-go/pkg/retry"
)

const birthdayStreamMaxPublishedPerMemberDay = 3

type BirthdayMemberRepository interface {
	FindMembersWithBirthdayOn(ctx context.Context, month, day int) ([]*domain.Member, error)
}

type birthdayStreamSession struct {
	VideoID        string
	ChannelID      string
	Title          string
	Status         string
	ScheduledStart *time.Time
	StartedAt      *time.Time
}

type BirthdayStreamRunner struct {
	memberRepo       BirthdayMemberRepository
	alarmRepo        AlarmRoomRepository
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
	alarmRepo AlarmRoomRepository,
	sessions birthdayStreamSessionStore,
	publisher Publisher,
	logger *slog.Logger,
	config BirthdayStreamRunnerConfig,
) *BirthdayStreamRunner {
	return &BirthdayStreamRunner{
		memberRepo:       memberRepo,
		alarmRepo:        alarmRepo,
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
		return err
	}
	if len(sessions) == 0 {
		return nil
	}

	dateStr := kstDay.Format("2006-01-02")
	candidates, err := r.selectPublishableSessions(ctx, members, sessions, dateStr)
	if err != nil {
		return err
	}
	if len(candidates) == 0 {
		return nil
	}

	return r.publishCandidates(ctx, candidates, dateStr, len(members))
}

func (r *BirthdayStreamRunner) publishCandidates(
	ctx context.Context,
	candidates []birthdayStreamCandidate,
	dateStr string,
	memberCount int,
) error {
	allRooms, err := r.alarmRepo.GetAllDistinctRoomIDs(ctx)
	if err != nil {
		return fmt.Errorf("birthday stream runner: get all rooms: %w", err)
	}
	if len(allRooms) == 0 {
		return nil
	}

	envelopes := buildBirthdayStreamEnvelopes(candidates, allRooms, dateStr)
	result, err := r.publisher.PublishDispatchBatch(ctx, envelopes)
	if err != nil {
		return fmt.Errorf("birthday stream runner: publish dispatch batch: %w", err)
	}

	if r.logger != nil {
		r.logger.Info("Birthday stream runner published",
			slog.String("date", dateStr),
			slog.Int("birthday_members", memberCount),
			slog.Int("videos", len(candidates)),
			slog.Int("rooms", len(allRooms)),
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
	session birthdayStreamSession
}

func (r *BirthdayStreamRunner) selectPublishableSessions(
	ctx context.Context,
	members []*domain.Member,
	sessions []birthdayStreamSession,
	dateStr string,
) ([]birthdayStreamCandidate, error) {
	byChannel := make(map[string][]birthdayStreamSession, len(members))
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
			return nil, err
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
	sessions []birthdayStreamSession,
	dateStr string,
) ([]birthdayStreamSession, error) {
	publishedKeys, err := r.sessions.ListPublishedEventKeys(ctx, birthdayStreamEventKeyPrefix(channelID, dateStr))
	if err != nil {
		return nil, err
	}
	remaining := birthdayStreamMaxPublishedPerMemberDay - len(publishedKeys)
	if remaining <= 0 {
		return nil, nil
	}

	published := make(map[string]struct{}, len(publishedKeys))
	for _, key := range publishedKeys {
		published[key] = struct{}{}
	}

	fresh := make([]birthdayStreamSession, 0, len(sessions))
	for _, session := range sessions {
		if _, ok := published[birthdayStreamEventKey(channelID, dateStr, session.VideoID)]; ok {
			continue
		}
		fresh = append(fresh, session)
	}
	sortBirthdayStreamSessions(fresh)
	if len(fresh) > remaining {
		fresh = fresh[:remaining]
	}
	return fresh, nil
}

func birthdayStreamEventKey(channelID, dateStr, videoID string) string {
	payload := domain.CelebrationDispatchPayload{
		Kind:      domain.CelebrationKindBirthdayStream,
		ChannelID: channelID,
		Date:      dateStr,
		VideoID:   videoID,
	}
	return dispatchoutbox.BuildEventKey(&dispatchoutbox.DedupeInput{
		SourceKind:     domain.AlarmDispatchSourceKindCelebration,
		SourceIdentity: payload.Identity(),
	})
}

func birthdayStreamEventKeyPrefix(channelID, dateStr string) string {
	return birthdayStreamEventKey(channelID, dateStr, "") + ":"
}

func sortBirthdayStreamSessions(sessions []birthdayStreamSession) {
	sort.SliceStable(sessions, func(i, j int) bool {
		a, b := birthdayStreamEffectiveStart(&sessions[i]), birthdayStreamEffectiveStart(&sessions[j])
		if !a.Equal(b) {
			return a.Before(b)
		}
		return sessions[i].VideoID < sessions[j].VideoID
	})
}

func birthdayStreamEffectiveStart(session *birthdayStreamSession) time.Time {
	if start := util.FirstNonNilTime(session.StartedAt, session.ScheduledStart); start != nil {
		return *start
	}
	return time.Time{}
}

func buildBirthdayStreamEnvelopes(
	candidates []birthdayStreamCandidate,
	allRooms []string,
	dateStr string,
) []domain.AlarmQueueEnvelope {
	envelopes := make([]domain.AlarmQueueEnvelope, 0, len(candidates)*len(allRooms))
	for _, c := range candidates {
		displayName := resolveCelebrationMemberName(c.member)
		for _, roomID := range allRooms {
			envelopes = append(envelopes, domain.AlarmQueueEnvelope{
				Notification: domain.AlarmNotification{
					AlarmType: domain.AlarmTypeBirthday,
					RoomID:    roomID,
					Channel:   &domain.Channel{ID: c.member.ChannelID, Name: displayName},
				},
				SourceKind: domain.AlarmDispatchSourceKindCelebration,
				Celebration: &domain.CelebrationDispatchPayload{
					Kind:              domain.CelebrationKindBirthdayStream,
					MemberName:        displayName,
					ChannelID:         c.member.ChannelID,
					Photo:             c.member.Photo,
					Date:              dateStr,
					VideoID:           c.session.VideoID,
					StreamTitle:       c.session.Title,
					StreamURL:         domain.YouTubeWatchURL(c.session.VideoID),
					ScheduledStartKST: birthdayStreamScheduledStartKST(&c.session),
				},
			})
		}
	}
	return envelopes
}

func birthdayStreamScheduledStartKST(session *birthdayStreamSession) string {
	start := util.FirstNonNilTime(session.ScheduledStart, session.StartedAt)
	if start == nil {
		return ""
	}
	return util.FormatKST(*start, "15:04")
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
