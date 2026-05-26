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
}

func (r *celebrationRunner) Start(ctx context.Context) error {
	for {
		if err := r.RunOnce(ctx); err != nil {
			if r.logger != nil {
				r.logger.Warn("Celebration runner failed", slog.Any("error", err))
			}
		}
		if !sleepContext(ctx, r.effectiveInterval()) {
			return nil
		}
	}
}

func (r *celebrationRunner) RunOnce(ctx context.Context) error {
	now := util.NowKST()
	if r.checkHourKST >= 0 && now.Hour() != r.checkHourKST {
		return nil
	}
	month, day, year := int(now.Month()), now.Day(), now.Year()
	dateStr := now.Format("2006-01-02")

	birthdayMembers, err := r.memberRepo.FindMembersWithBirthdayOn(ctx, month, day)
	if err != nil {
		return fmt.Errorf("celebration runner: find birthday members: %w", err)
	}

	anniversaryMembers, err := r.memberRepo.FindMembersWithAnniversaryOn(ctx, month, day, year)
	if err != nil {
		return fmt.Errorf("celebration runner: find anniversary members: %w", err)
	}

	anniversaryMembers = filterValidAnniversaryMembers(anniversaryMembers, year)

	allRooms, err := r.alarmRepo.GetAllDistinctRoomIDs(ctx)
	if err != nil {
		return fmt.Errorf("celebration runner: get all rooms: %w", err)
	}

	if len(birthdayMembers) == 0 && len(anniversaryMembers) == 0 {
		return nil
	}
	if len(allRooms) == 0 {
		return nil
	}

	envelopes := buildCelebrationEnvelopes(birthdayMembers, anniversaryMembers, allRooms, dateStr, year)
	if len(envelopes) == 0 {
		return nil
	}

	result, err := r.publisher.PublishDispatchBatch(ctx, envelopes)
	if err != nil {
		return fmt.Errorf("celebration runner: publish dispatch batch: %w", err)
	}

	if r.logger != nil {
		r.logger.Info("Celebration runner published",
			slog.Int("birthday_members", len(birthdayMembers)),
			slog.Int("anniversary_members", len(anniversaryMembers)),
			slog.Int("rooms", len(allRooms)),
			slog.Int("envelopes", len(envelopes)),
			slog.Int("inserted_events", result.InsertedEvents),
			slog.Int("inserted_deliveries", result.InsertedDeliveries),
			slog.Int("duplicate_events", result.DuplicateEvents),
		)
	}

	return nil
}

func (r *celebrationRunner) effectiveInterval() time.Duration {
	if r.runInterval > 0 {
		return r.runInterval
	}
	return time.Hour
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
	if birthday == nil || debutDate == nil || currentYear <= 0 {
		return 0
	}

	firstYear := debutDate.Year()
	if birthdayMonthDayBeforeDebut(birthday, debutDate) {
		firstYear++
	}
	if birthday.Month() == time.February && birthday.Day() == 29 {
		firstYear = nextLeapYear(firstYear)
		if currentYear < firstYear {
			return 0
		}
		return countLeapYears(firstYear, currentYear)
	}
	if currentYear < firstYear {
		return 0
	}
	return currentYear - firstYear + 1
}

func birthdayMonthDayBeforeDebut(birthday, debutDate *time.Time) bool {
	if birthday.Month() != debutDate.Month() {
		return birthday.Month() < debutDate.Month()
	}
	return birthday.Day() < debutDate.Day()
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
