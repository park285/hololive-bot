package workerapp

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/util"
)

type memberCelebrationRepository interface {
	FindMembersWithBirthdayOn(ctx context.Context, month, day int) ([]*domain.Member, error)
	FindMembersWithAnniversaryOn(ctx context.Context, month, day, referenceYear int) ([]*domain.Member, error)
}

type alarmRoomRepository interface {
	GetAllDistinctRoomIDs(ctx context.Context) ([]string, error)
}

type celebrationPublisher interface {
	PublishDispatchBatch(ctx context.Context, envelopes []domain.AlarmQueueEnvelope) (dispatchoutbox.PublishBatchResult, error)
}

type celebrationRunner struct {
	memberRepo   memberCelebrationRepository
	alarmRepo    alarmRoomRepository
	publisher    celebrationPublisher
	logger       *slog.Logger
	checkHourKST int
	runInterval  time.Duration
	now          func() time.Time
	sleep        func(context.Context, time.Duration) bool
}

func (r *celebrationRunner) Start(ctx context.Context) error {
	if r.checkHourKST >= 0 {
		return r.startScheduled(ctx)
	}

	for {
		r.logRunFailure(r.RunOnce(ctx))
		if !r.effectiveSleep()(ctx, r.effectiveInterval()) {
			return nil
		}
	}
}

func (r *celebrationRunner) startScheduled(ctx context.Context) error {
	var lastScheduledAt time.Time
	for {
		now := r.effectiveNow()
		scheduledAt := nextCelebrationRunAt(r.scheduleFrom(now, lastScheduledAt), r.checkHourKST)
		if !r.effectiveSleep()(ctx, scheduledAt.Sub(now)) {
			return nil
		}
		r.logRunFailure(r.runAt(ctx, scheduledAt))
		lastScheduledAt = scheduledAt
	}
}

func (r *celebrationRunner) scheduleFrom(now, lastScheduledAt time.Time) time.Time {
	if !lastScheduledAt.IsZero() && !now.After(lastScheduledAt) {
		return lastScheduledAt.Add(time.Nanosecond)
	}
	return now
}

func (r *celebrationRunner) logRunFailure(err error) {
	if err == nil || r.logger == nil {
		return
	}
	r.logger.Warn("Celebration runner failed", slog.Any("error", err))
}

func (r *celebrationRunner) RunOnce(ctx context.Context) error {
	now := r.effectiveNow()
	if !r.shouldRunAt(now) {
		return nil
	}
	return r.runAt(ctx, now)
}

func (r *celebrationRunner) runAt(ctx context.Context, now time.Time) error {
	month, day, year := int(now.Month()), now.Day(), now.Year()
	dateStr := now.Format("2006-01-02")

	candidates, err := r.findCelebrationCandidates(ctx, month, day, year)
	if err != nil {
		return err
	}

	allRooms, err := r.alarmRepo.GetAllDistinctRoomIDs(ctx)
	if err != nil {
		return fmt.Errorf("celebration runner: get all rooms: %w", err)
	}

	if candidates.empty() || len(allRooms) == 0 {
		return nil
	}

	envelopes := buildCelebrationEnvelopes(candidates.birthdays, candidates.anniversaries, allRooms, dateStr, year)
	if len(envelopes) == 0 {
		return nil
	}

	result, err := r.publisher.PublishDispatchBatch(ctx, envelopes)
	if err != nil {
		return fmt.Errorf("celebration runner: publish dispatch batch: %w", err)
	}

	if r.logger != nil {
		r.logger.Info("Celebration runner published",
			slog.Int("birthday_members", len(candidates.birthdays)),
			slog.Int("anniversary_members", len(candidates.anniversaries)),
			slog.Int("rooms", len(allRooms)),
			slog.Int("envelopes", len(envelopes)),
			slog.Int("inserted_events", result.InsertedEvents),
			slog.Int("inserted_deliveries", result.InsertedDeliveries),
			slog.Int("duplicate_events", result.DuplicateEvents),
		)
	}

	return nil
}

func (r *celebrationRunner) shouldRunAt(now time.Time) bool {
	return r.checkHourKST < 0 || now.Hour() == r.checkHourKST
}

func nextCelebrationRunAt(now time.Time, checkHourKST int) time.Time {
	kstNow := util.ToKST(now)
	if _, offset := now.Zone(); offset == 9*60*60 {
		kstNow = now
	}

	next := time.Date(kstNow.Year(), kstNow.Month(), kstNow.Day(), checkHourKST, 0, 0, 0, kstNow.Location())
	if kstNow.After(next) {
		next = next.AddDate(0, 0, 1)
	}
	return next
}

type celebrationCandidates struct {
	birthdays     []*domain.Member
	anniversaries []*domain.Member
}

func (c celebrationCandidates) empty() bool {
	return len(c.birthdays) == 0 && len(c.anniversaries) == 0
}

func (r *celebrationRunner) findCelebrationCandidates(ctx context.Context, month, day, year int) (celebrationCandidates, error) {
	birthdayMembers, err := r.memberRepo.FindMembersWithBirthdayOn(ctx, month, day)
	if err != nil {
		return celebrationCandidates{}, fmt.Errorf("celebration runner: find birthday members: %w", err)
	}

	anniversaryMembers, err := r.memberRepo.FindMembersWithAnniversaryOn(ctx, month, day, year)
	if err != nil {
		return celebrationCandidates{}, fmt.Errorf("celebration runner: find anniversary members: %w", err)
	}

	return celebrationCandidates{
		birthdays:     birthdayMembers,
		anniversaries: filterValidAnniversaryMembers(anniversaryMembers, year),
	}, nil
}

func (r *celebrationRunner) effectiveInterval() time.Duration {
	if r.runInterval > 0 {
		return r.runInterval
	}
	return time.Hour
}

func (r *celebrationRunner) effectiveNow() time.Time {
	if r.now != nil {
		return r.now()
	}
	return util.NowKST()
}

func (r *celebrationRunner) effectiveSleep() func(context.Context, time.Duration) bool {
	if r.sleep != nil {
		return r.sleep
	}
	return sleepContext
}

func filterValidAnniversaryMembers(members []*domain.Member, currentYear int) []*domain.Member {
	valid := make([]*domain.Member, 0, len(members))
	for _, m := range members {
		if m.DebutDate == nil {
			continue
		}
		if currentYear-m.DebutDate.Year() <= 0 {
			continue
		}
		valid = append(valid, m)
	}
	return valid
}

func buildCelebrationEnvelopes(
	birthdayMembers, anniversaryMembers []*domain.Member,
	allRooms []string,
	dateStr string,
	currentYear int,
) []domain.AlarmQueueEnvelope {
	envelopes := make([]domain.AlarmQueueEnvelope, 0, (len(birthdayMembers)+len(anniversaryMembers))*len(allRooms))
	envelopes = appendBirthdayCelebrationEnvelopes(envelopes, birthdayMembers, allRooms, dateStr, currentYear)
	return appendAnniversaryCelebrationEnvelopes(envelopes, anniversaryMembers, allRooms, dateStr, currentYear)
}

func appendBirthdayCelebrationEnvelopes(
	envelopes []domain.AlarmQueueEnvelope,
	birthdayMembers []*domain.Member,
	allRooms []string,
	dateStr string,
	currentYear int,
) []domain.AlarmQueueEnvelope {
	for _, m := range birthdayMembers {
		displayName := resolveCelebrationMemberName(m)
		for _, roomID := range allRooms {
			envelopes = append(envelopes, domain.AlarmQueueEnvelope{
				Notification: domain.AlarmNotification{
					AlarmType: domain.AlarmTypeBirthday,
					RoomID:    roomID,
					Channel:   &domain.Channel{ID: m.ChannelID, Name: displayName},
				},
				SourceKind: domain.AlarmDispatchSourceKindCelebration,
				Celebration: &domain.CelebrationDispatchPayload{
					Kind:       domain.CelebrationKindBirthday,
					MemberName: displayName,
					ChannelID:  m.ChannelID,
					Photo:      m.Photo,
					Ordinal:    birthdayCelebrationOrdinal(m.Birthday, m.DebutDate, currentYear),
					Date:       dateStr,
				},
			})
		}
	}
	return envelopes
}

func appendAnniversaryCelebrationEnvelopes(
	envelopes []domain.AlarmQueueEnvelope,
	anniversaryMembers []*domain.Member,
	allRooms []string,
	dateStr string,
	currentYear int,
) []domain.AlarmQueueEnvelope {
	for _, m := range anniversaryMembers {
		if m.DebutDate == nil {
			continue
		}
		displayName := resolveCelebrationMemberName(m)
		years := currentYear - m.DebutDate.Year()
		if years <= 0 {
			continue
		}
		for _, roomID := range allRooms {
			envelopes = append(envelopes, domain.AlarmQueueEnvelope{
				Notification: domain.AlarmNotification{
					AlarmType: domain.AlarmTypeAnniversary,
					RoomID:    roomID,
					Channel:   &domain.Channel{ID: m.ChannelID, Name: displayName},
				},
				SourceKind: domain.AlarmDispatchSourceKindCelebration,
				Celebration: &domain.CelebrationDispatchPayload{
					Kind:       domain.CelebrationKindAnniversary,
					MemberName: displayName,
					ChannelID:  m.ChannelID,
					Photo:      m.Photo,
					Years:      years,
					Date:       dateStr,
				},
			})
		}
	}
	return envelopes
}

func birthdayCelebrationOrdinal(birthday, debutDate *time.Time, currentYear int) int {
	if missingBirthdayOrdinalInput(birthday, debutDate, currentYear) {
		return 0
	}

	firstYear := debutDate.Year()
	if birthdayMonthDayBeforeDebut(birthday, debutDate) {
		firstYear++
	}
	if isLeapDay(birthday) {
		return leapDayBirthdayOrdinal(firstYear, currentYear)
	}
	if currentYear < firstYear {
		return 0
	}
	return currentYear - firstYear + 1
}

func missingBirthdayOrdinalInput(birthday, debutDate *time.Time, currentYear int) bool {
	return birthday == nil || debutDate == nil || currentYear <= 0
}

func birthdayMonthDayBeforeDebut(birthday, debutDate *time.Time) bool {
	if birthday.Month() != debutDate.Month() {
		return birthday.Month() < debutDate.Month()
	}
	return birthday.Day() < debutDate.Day()
}

func isLeapDay(date *time.Time) bool {
	return date.Month() == time.February && date.Day() == 29
}

func leapDayBirthdayOrdinal(firstYear, currentYear int) int {
	firstYear = nextLeapYear(firstYear)
	if currentYear < firstYear {
		return 0
	}
	return countLeapYears(firstYear, currentYear)
}

func nextLeapYear(year int) int {
	for !isLeapYear(year) {
		year++
	}
	return year
}

func countLeapYears(startYear, endYear int) int {
	count := 0
	for year := startYear; year <= endYear; year++ {
		if isLeapYear(year) {
			count++
		}
	}
	return count
}

func isLeapYear(year int) bool {
	return year%400 == 0 || (year%4 == 0 && year%100 != 0)
}

func resolveCelebrationMemberName(m *domain.Member) string {
	if m.ShortKoreanName != "" {
		return m.ShortKoreanName
	}
	if m.NameKo != "" {
		return m.NameKo
	}
	return m.Name
}
