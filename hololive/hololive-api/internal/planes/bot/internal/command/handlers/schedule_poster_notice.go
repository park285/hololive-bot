package handlers

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	handlercore "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-api/internal/service/scheduleposter/cardtemplate"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedprivacylog "github.com/kapu/hololive-shared/pkg/privacylog"
)

const (
	scheduleNoticeInterval   = 30 * time.Second
	scheduleNoticeLease      = 5 * time.Minute
	scheduleNoticeDeadline   = 4 * time.Minute
	scheduleNoticeMaxAge     = 6 * time.Hour
	scheduleNoticeRenderTime = time.Minute
)

type ScheduleNoticeStore interface {
	MemberPosterRows(ctx context.Context, posterID int64, memberID int, channelID string, day time.Time) (scheduleposter.PublishedSchedule, cardtemplate.ID, bool, error)
	ClaimNotice(ctx context.Context, lease time.Duration) (scheduleposter.Notice, bool, error)
	NoticePostCurrent(ctx context.Context, notice scheduleposter.Notice) (bool, error)
	SettleNotice(ctx context.Context, notice scheduleposter.Notice, status scheduleposter.NoticeStatus, rooms int) error
}

type LiveRoomLister interface {
	LiveSubscriberRooms(ctx context.Context, channelID string) ([]string, error)
}

type SchedulePosterNotifier struct {
	deps    *handlercore.Dependencies
	notices ScheduleNoticeStore
	rooms   LiveRoomLister
}

func NewSchedulePosterNotifier(deps *handlercore.Dependencies, notices ScheduleNoticeStore, rooms LiveRoomLister) *SchedulePosterNotifier {
	return &SchedulePosterNotifier{deps: deps, notices: notices, rooms: rooms}
}

func (n *SchedulePosterNotifier) Run(ctx context.Context) {
	ticker := time.NewTicker(scheduleNoticeInterval)
	defer ticker.Stop()

	for {
		n.drain(ctx)

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (n *SchedulePosterNotifier) drain(ctx context.Context) {
	for ctx.Err() == nil {
		handled, err := n.NotifyNext(ctx)
		if err != nil {
			n.deps.Logger.WarnContext(ctx, "Schedule poster notice failed", slog.Any("error", err))

			return
		}

		if !handled {
			return
		}
	}
}

func (n *SchedulePosterNotifier) NotifyNext(ctx context.Context) (bool, error) {
	notice, ok, err := n.notices.ClaimNotice(ctx, scheduleNoticeLease)
	if err != nil {
		return false, fmt.Errorf("claim notice: %w", err)
	}

	if !ok {
		return false, nil
	}

	notifyCtx, cancel := context.WithTimeout(ctx, scheduleNoticeDeadline)
	defer cancel()

	if err := n.notify(notifyCtx, notice); err != nil {
		return true, fmt.Errorf("notice %d: %w", notice.ID, err)
	}

	return true, nil
}

func (n *SchedulePosterNotifier) notify(ctx context.Context, notice scheduleposter.Notice) error {
	if notice.ClaimedAt.Sub(notice.CreatedAt) > scheduleNoticeMaxAge {
		return n.skip(ctx, notice, "expired")
	}

	member, err := n.deps.MembersData.FindMemberByChannelID(ctx, notice.ChannelID)
	if errors.Is(err, domain.ErrMemberNotFound) {
		return n.skip(ctx, notice, "member_not_found")
	}

	if err != nil {
		return fmt.Errorf("find member: %w", err)
	}

	schedule, template, reason, err := n.load(ctx, notice, member)
	if err != nil {
		return err
	}

	if reason != "" {
		return n.skip(ctx, notice, reason)
	}

	rooms, err := n.rooms.LiveSubscriberRooms(ctx, notice.ChannelID)
	if err != nil {
		return fmt.Errorf("live subscriber rooms: %w", err)
	}

	if len(rooms) > 0 {
		if err := n.send(ctx, notice, member, &schedule, template, rooms); err != nil {
			return err
		}
	}

	if err := n.notices.SettleNotice(ctx, notice, scheduleposter.NoticeSent, len(rooms)); err != nil {
		return fmt.Errorf("settle sent: %w", err)
	}

	n.deps.Logger.InfoContext(ctx, "Schedule poster notice sent", slog.Int64("notice_id", notice.ID), slog.String("kind", string(notice.Kind)),
		slog.Int64("poster_id", notice.PosterID), slog.String("channel_id", notice.ChannelID), slog.Int("rooms", len(rooms)))

	return nil
}

func (n *SchedulePosterNotifier) load(ctx context.Context, notice scheduleposter.Notice, member *domain.Member) (scheduleposter.PublishedSchedule, cardtemplate.ID, string, error) {
	switch notice.Kind {
	case scheduleposter.NoticePublished:
	case scheduleposter.NoticeChanged:
		current, err := n.notices.NoticePostCurrent(ctx, notice)
		if err != nil {
			return scheduleposter.PublishedSchedule{}, "", "", fmt.Errorf("check post: %w", err)
		}

		if !current {
			return scheduleposter.PublishedSchedule{}, "", "post_not_applied", nil
		}
	default:
		return scheduleposter.PublishedSchedule{}, "", "", fmt.Errorf("unknown notice kind %q", notice.Kind)
	}

	schedule, template, found, err := n.notices.MemberPosterRows(ctx, notice.PosterID, member.ID, notice.ChannelID, jstDay(notice.ClaimedAt))
	if err != nil {
		return scheduleposter.PublishedSchedule{}, "", "", fmt.Errorf("load poster rows: %w", err)
	}

	if !found {
		return scheduleposter.PublishedSchedule{}, "", "poster_unavailable", nil
	}

	return schedule, template, "", nil
}

func (n *SchedulePosterNotifier) send(ctx context.Context, notice scheduleposter.Notice, member *domain.Member, schedule *scheduleposter.PublishedSchedule,
	template cardtemplate.ID, rooms []string,
) error {
	renderCtx, cancel := context.WithTimeout(ctx, scheduleNoticeRenderTime)
	reply, err := renderScheduleReply(renderCtx, n.deps, member, schedule, template)

	cancel()

	if err != nil {
		return err
	}

	headline := scheduleNoticeHeadline(notice, member.DisplayName(), schedule)

	var errs []error

	for _, room := range rooms {
		roomCtx := transport.WithReplyIdentity(ctx, scheduleNoticeReplyIdentity(notice.ID, room))

		if err := n.deps.SendMessage(roomCtx, room, headline); err != nil {
			errs = append(errs, fmt.Errorf("room %s: send headline: %w", sharedprivacylog.Pseudonym(room), err))

			continue
		}

		if err := sendScheduleReply(roomCtx, n.deps, room, reply); err != nil {
			errs = append(errs, fmt.Errorf("room %s: %w", sharedprivacylog.Pseudonym(room), err))
		}
	}

	return errors.Join(errs...)
}

func scheduleNoticeReplyIdentity(noticeID int64, room string) string {
	return "schedule-notice:" + strconv.FormatInt(noticeID, 10) + ":" + room
}

func (n *SchedulePosterNotifier) skip(ctx context.Context, notice scheduleposter.Notice, reason string) error {
	if err := n.notices.SettleNotice(ctx, notice, scheduleposter.NoticeSkipped, 0); err != nil {
		return fmt.Errorf("settle skipped: %w", err)
	}

	n.deps.Logger.InfoContext(ctx, "Schedule poster notice skipped", slog.Int64("notice_id", notice.ID), slog.String("kind", string(notice.Kind)),
		slog.Int64("poster_id", notice.PosterID), slog.String("channel_id", notice.ChannelID), slog.String("reason", reason))

	return nil
}

func scheduleNoticeHeadline(notice scheduleposter.Notice, name string, schedule *scheduleposter.PublishedSchedule) string {
	if notice.Kind == scheduleposter.NoticeChanged {
		return "🔄 " + name + " 일정이 변경되었습니다.\nhttps://x.com/i/status/" + strconv.FormatInt(notice.PostID, 10)
	}

	return "📅 " + name + " 새 주간 일정표 (" + schedule.WeekStart.AddDate(0, 0, schedule.RowsDayShift).Format("1/2") + " ~ " + schedule.WeekEnd.AddDate(0, 0, schedule.RowsDayShift).Format("1/2") + ")"
}

func jstDay(at time.Time) time.Time {
	local := at.In(schedulePosterZone)

	return time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, time.UTC)
}

var errScheduleNoticeUnconfigured = errors.New("schedule poster notifier is not configured")

func (n *SchedulePosterNotifier) CheckDependencies() error {
	if n == nil || n.notices == nil || n.rooms == nil || n.deps == nil || n.deps.MembersData == nil || n.deps.ScheduleCards == nil ||
		n.deps.SendMessage == nil || n.deps.SendImages == nil || n.deps.SendTextFile == nil || n.deps.Logger == nil {
		return errScheduleNoticeUnconfigured
	}

	return nil
}
