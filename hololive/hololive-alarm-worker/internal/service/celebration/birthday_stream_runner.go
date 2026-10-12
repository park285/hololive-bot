package celebration

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"strings"
	"time"

	"github.com/park285/shared-go/v2/pkg/retry"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/timeutil"
)

const birthdayStreamMaxPublishedPerMemberDay = 3

// 멤버 생일은 현지 날짜라서 판정 창을 KST 생일 당일 앞뒤로 넓힌다. KST보다 늦은 지역의 생일 저녁
// 방송은 KST 다음 날로 넘어가고(ID 2시간, 미국 서부 최대 17시간), KST보다 이른 지역(호주 동부 최대
// 2시간)과 KST 자정 직전 카운트다운은 전날에 시작한다. 전날 시작한 방송은 생일축하 발송 뒤에도 예정·진행
// 상태일 때만 알린다. 창이 넓어도 제목 판정과 일일 상한이 오탐을 막는다.
const (
	birthdayStreamWindowLeading  = 3 * time.Hour
	birthdayStreamWindowTrailing = 17 * time.Hour
)

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
		if err := r.runForDate(ctx, kstDay); err != nil {
			errs = append(errs, err)
		}
	}

	return errors.Join(errs...)
}

// 어제 생일의 판정 창은 오늘 KST 17시까지 이어진다. 직전 tick 이후 그 창이 열려 있었으면 어제도
// 평가해 마지막 tick 뒤에 생긴 세션을 놓치지 않는다. 내일 생일은 축하 발송 전이라 대상 방이 없으므로
// 평가하지 않는다.
func birthdayStreamEvaluationDates(now time.Time, tickInterval time.Duration) []time.Time {
	current := kstDayStart(now)

	if now.Add(-tickInterval).Before(birthdayStreamWindowEnd(current.AddDate(0, 0, -1))) {
		return []time.Time{current.AddDate(0, 0, -1), current}
	}

	return []time.Time{current}
}

func birthdayStreamWindowStart(kstDay time.Time) time.Time {
	return kstDay.Add(-birthdayStreamWindowLeading)
}

func birthdayStreamWindowEnd(kstDay time.Time) time.Time {
	return kstDay.AddDate(0, 0, 1).Add(birthdayStreamWindowTrailing)
}

func kstDayStart(t time.Time) time.Time {
	kst := timeutil.ToKST(t)
	return time.Date(kst.Year(), kst.Month(), kst.Day(), 0, 0, 0, 0, kst.Location())
}

func (r *BirthdayStreamRunner) runForDate(ctx context.Context, kstDay time.Time) error {
	members, err := r.memberRepo.FindMembersWithBirthdayOn(ctx, int(kstDay.Month()), kstDay.Day())
	if err != nil {
		return fmt.Errorf("birthday stream runner: find birthday members: %w", err)
	}

	members = r.filterMembersWithChannel(members)
	if len(members) == 0 {
		return nil
	}

	// 생일 날짜 판정 시각과 관측 조회 시각을 분리해 멤버 조회 중 갱신된 세션도 포함한다.
	observedUntil := r.effectiveNow()

	sessions, err := r.sessions.FindBirthdaySessions(
		ctx,
		memberChannelIDs(members),
		birthdayStreamWindowStart(kstDay).UTC(),
		birthdayStreamWindowEnd(kstDay).UTC(),
		observedUntil.Add(-r.effectiveFreshness()),
		observedUntil,
	)
	if err != nil {
		return fmt.Errorf("find birthday sessions: %w", err)
	}

	if len(sessions) == 0 {
		return nil
	}

	dateStr := kstDay.Format(time.DateOnly)

	candidates, publishedEvents, err := r.selectPublishableSessions(ctx, members, sessions, dateStr)
	if err != nil {
		return fmt.Errorf("select publishable sessions: %w", err)
	}

	if len(candidates) == 0 {
		return nil
	}

	if err := r.publishCandidates(ctx, candidates, publishedEvents, dateStr, len(members)); err != nil {
		return fmt.Errorf("publish candidates: %w", err)
	}

	return nil
}

func (r *BirthdayStreamRunner) publishCandidates(
	ctx context.Context,
	candidates []birthdayStreamCandidate,
	publishedEvents map[string]domain.AlarmQueueEnvelope,
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

	// audience 방은 birthday greeting event_key로 찾았으므로, 그 event의 저장 payload가 같은 MemberID 키로
	// 해석되는지 먼저 확인한다. 불일치나 decode 실패는 audience를 믿을 수 없다는 뜻이라 발행하지 않고 오류로 끝낸다.
	if _, verifyErr := r.sessions.FindPublishedCelebrationEvents(ctx, birthdayEventKeys); verifyErr != nil {
		return fmt.Errorf("birthday stream runner: verify published birthday events: %w", verifyErr)
	}

	envelopes := buildBirthdayStreamEnvelopes(candidates, roomsByEventKey, publishedEvents, dateStr)
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

// selectPublishableSessions는 후보와 함께 후보 멤버들의 기발행 event payload를 돌려준다.
// 재발행은 이 payload를 그대로 써야 같은 event key의 hash 충돌 없이 새 방 delivery만 추가된다.
func (r *BirthdayStreamRunner) selectPublishableSessions(
	ctx context.Context,
	members []*domain.Member,
	sessions []BirthdayStreamSession,
	dateStr string,
) ([]birthdayStreamCandidate, map[string]domain.AlarmQueueEnvelope, error) {
	byChannel := make(map[string][]BirthdayStreamSession, len(members))

	for _, session := range sessions {
		byChannel[session.ChannelID] = append(byChannel[session.ChannelID], session)
	}

	candidates := make([]birthdayStreamCandidate, 0, len(sessions))
	publishedEvents := make(map[string]domain.AlarmQueueEnvelope)

	for _, m := range members {
		channelSessions := byChannel[m.ChannelID]
		if len(channelSessions) == 0 {
			continue
		}

		published, err := r.sessions.ListPublishedBirthdayStreamEvents(ctx, birthdayStreamEventKeyPrefix(m.ID, dateStr))
		if err != nil {
			return nil, nil, fmt.Errorf("select birthday stream sessions: member %d: %w", m.ID, err)
		}

		maps.Copy(publishedEvents, published)

		for _, session := range selectBirthdayStreamSessionsWithinDailyCap(m.ID, dateStr, channelSessions, published) {
			candidates = append(candidates, birthdayStreamCandidate{member: m, session: session})
		}
	}

	return candidates, publishedEvents, nil
}

// 상한과 재평가 대상은 저장 제목이 생일 방송으로 판정되는 기발행 event만 센다. 저장 제목이
// 생일과 무관한 기발행 event는 같은 날의 실제 생일 방송을 막지 않도록 상한에서 빼고, 현재 제목과
// 관계없이 다시 보내지 않는다. 재발행은 저장 payload를 쓰므로 생일과 무관한 제목이 다시 나가기 때문이다.
func selectBirthdayStreamSessionsWithinDailyCap(
	memberID int,
	dateStr string,
	sessions []BirthdayStreamSession,
	published map[string]domain.AlarmQueueEnvelope,
) []BirthdayStreamSession {
	counted := 0

	for eventKey := range published {
		if isPublishedBirthdayStream(published[eventKey].Celebration) {
			counted++
		}
	}

	remaining := max(birthdayStreamMaxPublishedPerMemberDay-counted, 0)

	ordered := slices.Clone(sessions)
	sortBirthdayStreamSessions(ordered)

	selected := make([]BirthdayStreamSession, 0, min(len(ordered), birthdayStreamMaxPublishedPerMemberDay))
	for _, session := range ordered {
		eventKey := birthdayStreamEventKey(memberID, dateStr, session.VideoID)
		if _, ok := published[eventKey]; ok {
			// 이미 생일 방송으로 발행한 event는 이후 제목이 바뀌어도 늦게 생일축하를 받은 방을 위해 재평가한다.
			if isPublishedBirthdayStream(published[eventKey].Celebration) && len(selected) < birthdayStreamMaxPublishedPerMemberDay {
				selected = append(selected, session)
			}

			continue
		}

		if remaining > 0 && len(selected) < birthdayStreamMaxPublishedPerMemberDay && isBirthdayStreamTitle(session.Title) {
			selected = append(selected, session)
			remaining--
		}
	}

	return selected
}

func isPublishedBirthdayStream(payload *domain.CelebrationDispatchPayload) bool {
	return payload != nil && isBirthdayStreamTitle(payload.StreamTitle)
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
	if start := timeutil.FirstNonNilTime(session.StartedAt, session.ScheduledStart); start != nil {
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

	return timeutil.NowKST()
}

func (r *BirthdayStreamRunner) effectiveSleep() func(context.Context, time.Duration) bool {
	if r.sleep != nil {
		return r.sleep
	}

	return retry.Sleep
}
