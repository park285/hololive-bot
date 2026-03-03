package command

import (
	"context"
	stdErrors "errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
)

// AlarmCommand: 알람 설정 및 관리를 담당하는 커맨드 핸들러
type AlarmCommand struct {
	BaseCommand
}

// NewAlarmCommand: 알람 관리 커맨드 핸들러를 생성합니다.
func NewAlarmCommand(deps *Dependencies) *AlarmCommand {
	return &AlarmCommand{BaseCommand: NewBaseCommand(deps)}
}

// Name: 커맨드의 이름("alarm")을 반환합니다.
func (c *AlarmCommand) Name() string {
	return "alarm"
}

// Description: 커맨드에 대한 설명을 반환합니다.
func (c *AlarmCommand) Description() string {
	return "방송 알람 관리"
}

// Execute: 알람 추가, 삭제, 목록 조회 등의 작업을 수행합니다.
func (c *AlarmCommand) Execute(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	if err := c.ensureDeps(); err != nil {
		return fmt.Errorf("failed to ensure dependencies: %w", err)
	}

	if c.Deps().Alarm == nil {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrAlarmServiceNotInitialized)
	}

	action, hasAction := params["action"].(string)
	if !hasAction {
		action = "list"
	}

	switch action {
	case "set", "add":
		return c.handleAdd(ctx, cmdCtx, params)
	case "remove", "delete":
		return c.handleRemove(ctx, cmdCtx, params)
	case "list":
		c.Deps().Logger.Info("Alarm list requested")
		return c.handleList(ctx, cmdCtx)
	case "clear":
		return c.handleClear(ctx, cmdCtx)
	case "invalid":
		subCmd, _ := params["sub_command"].(string)
		memberName, _ := params["member"].(string)
		c.Deps().Logger.Info("Invalid alarm command received",
			slog.String("room", cmdCtx.Room),
			slog.String("sender", cmdCtx.UserName),
			slog.String("sub_command", subCmd),
			slog.String("member", memberName),
		)
		return c.Deps().SendError(ctx, cmdCtx.Room, c.Deps().Formatter.InvalidAlarmUsage())
	default:
		return c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatHelp(ctx))
	}
}

func (c *AlarmCommand) ensureDeps() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("failed to ensure base dependencies: %w", err)
	}

	if c.Deps().Matcher == nil || c.Deps().Formatter == nil {
		return fmt.Errorf("alarm command services not configured")
	}

	return nil
}

func (c *AlarmCommand) handleAdd(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	memberName, hasMember := params["member"].(string)
	if !hasMember || memberName == "" {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrAlarmNeedMemberNameAdd)
	}

	alarmTypes := c.parseAlarmTypes(params)

	c.Deps().Logger.Info("Alarm add requested",
		slog.String("member", memberName),
		slog.Any("types", alarmTypes))

	channel, err := c.Deps().Matcher.FindBestMatchWithCandidates(ctx, memberName)
	if err != nil {
		var ambiguousErr *matcher.AmbiguousMatchError
		if stdErrors.As(err, &ambiguousErr) {
			// 동명이인 발견 시 선택 리스트 메시지 반환
			message := c.Deps().Formatter.FormatAmbiguousMembers(ambiguousErr.Candidates)
			return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
		}
		return c.Deps().SendError(ctx, cmdCtx.Room, c.Deps().Formatter.MemberNotFound(memberName))
	}
	if channel == nil {
		return c.Deps().SendError(ctx, cmdCtx.Room, c.Deps().Formatter.MemberNotFound(memberName))
	}

	// 졸업 멤버 체크 (기존 로직 유지)
	if c.Deps().Matcher != nil {
		if member := c.Deps().Matcher.GetMemberByChannelID(ctx, channel.ID); member != nil && member.IsGraduated {
			return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrGraduatedMemberBlocked)
		}
	}

	added, err := c.Deps().Alarm.AddAlarm(ctx, domain.AddAlarmRequest{
		RoomID:     cmdCtx.Room,
		UserID:     cmdCtx.UserID,
		ChannelID:  channel.ID,
		MemberName: channel.Name,
		RoomName:   cmdCtx.RoomName,
		UserName:   cmdCtx.UserName,
		AlarmTypes: alarmTypes,
	})
	if err != nil {
		c.Deps().Logger.Error("Failed to add alarm",
			slog.String("channel", channel.Name),
			slog.Any("error", err),
		)
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrAlarmAddFailed)
	}

	nextStreamInfo, _ := c.Deps().Alarm.GetNextStreamInfo(ctx, channel.ID)

	message := c.Deps().Formatter.FormatAlarmAdded(ctx, channel.Name, added, nextStreamInfo)
	return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
}

func (c *AlarmCommand) handleRemove(ctx context.Context, cmdCtx *domain.CommandContext, params map[string]any) error {
	memberName, hasMember := params["member"].(string)
	if !hasMember || memberName == "" {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrAlarmNeedMemberNameRemove)
	}

	alarmTypes := c.parseAlarmTypes(params)

	c.Deps().Logger.Info("Alarm remove requested",
		slog.String("member", memberName),
		slog.Any("types", alarmTypes))

	channel, err := c.Deps().Matcher.FindBestMatchWithCandidates(ctx, memberName)
	if err != nil {
		var ambiguousErr *matcher.AmbiguousMatchError
		if stdErrors.As(err, &ambiguousErr) {
			message := c.Deps().Formatter.FormatAmbiguousMembers(ambiguousErr.Candidates)
			return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
		}
		return c.Deps().SendError(ctx, cmdCtx.Room, c.Deps().Formatter.MemberNotFound(memberName))
	}
	if channel == nil {
		return c.Deps().SendError(ctx, cmdCtx.Room, c.Deps().Formatter.MemberNotFound(memberName))
	}

	removed, err := c.Deps().Alarm.RemoveAlarm(ctx, cmdCtx.Room, channel.ID, alarmTypes)
	if err != nil {
		c.Deps().Logger.Error("Failed to remove alarm",
			slog.String("channel", channel.Name),
			slog.Any("error", err),
		)
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrAlarmRemoveFailed)
	}

	message := c.Deps().Formatter.FormatAlarmRemoved(ctx, channel.Name, removed)
	return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
}

func (c *AlarmCommand) handleList(ctx context.Context, cmdCtx *domain.CommandContext) error {
	alarms, err := c.Deps().Alarm.GetRoomAlarmsWithTypes(ctx, cmdCtx.Room)
	if err != nil {
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrAlarmListFailed)
	}

	alarmInfos := make([]adapter.AlarmListEntry, 0, len(alarms))
	for _, alarm := range alarms {
		memberName := c.Deps().Alarm.GetMemberNameWithFallback(ctx, alarm.ChannelID)
		nextStreamInfo, _ := c.Deps().Alarm.GetNextStreamInfo(ctx, alarm.ChannelID)
		alarmInfos = append(alarmInfos, adapter.AlarmListEntry{
			MemberName: memberName,
			AlarmTypes: alarm.AlarmTypes,
			NextStream: nextStreamInfo,
		})
	}

	message := c.Deps().Formatter.FormatAlarmList(ctx, alarmInfos)
	return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
}

func (c *AlarmCommand) handleClear(ctx context.Context, cmdCtx *domain.CommandContext) error {
	count, err := c.Deps().Alarm.ClearRoomAlarms(ctx, cmdCtx.Room)
	if err != nil {
		c.Deps().Logger.Error("Failed to clear alarms", slog.Any("error", err))
		return c.Deps().SendError(ctx, cmdCtx.Room, adapter.ErrAlarmClearFailed)
	}

	message := c.Deps().Formatter.FormatAlarmCleared(ctx, count)
	return c.Deps().SendMessage(ctx, cmdCtx.Room, message)
}

func (c *AlarmCommand) parseAlarmTypes(params map[string]any) domain.AlarmTypes {
	typeStr, hasType := params["type"].(string)
	if !hasType || typeStr == "" {
		return domain.DefaultAlarmTypes
	}

	switch typeStr {
	case "방송", "라이브", "live":
		return domain.AlarmTypes{domain.AlarmTypeLive}
	case "커뮤니티", "community":
		return domain.AlarmTypes{domain.AlarmTypeCommunity}
	case "쇼츠", "shorts":
		return domain.AlarmTypes{domain.AlarmTypeShorts}
	case "전체", "all":
		return domain.AllAlarmTypes
	default:
		return domain.DefaultAlarmTypes
	}
}
