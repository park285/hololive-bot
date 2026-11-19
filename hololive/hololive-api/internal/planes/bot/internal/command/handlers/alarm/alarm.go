package alarm

import (
	"context"
	stdErrors "errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/botcommand"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/domain/mekparkhost"
	sharedprivacylog "github.com/kapu/hololive-shared/pkg/privacylog"
)

type AlarmCommand struct {
	handlercore.BaseCommand
}

type alarmTarget struct {
	ChannelID string
	HostID    string
	Name      string
}

type alarmActionHandler func(context.Context, *botcommand.CommandContext, map[string]any) error

func NewAlarmCommand(deps *handlercore.Dependencies) *AlarmCommand {
	return &AlarmCommand{BaseCommand: handlercore.NewBaseCommand(deps)}
}

func (c *AlarmCommand) Name() string {
	return "alarm"
}

func (c *AlarmCommand) Description() string {
	return "방송 알람 관리"
}

func (c *AlarmCommand) Execute(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any) error {
	if err := c.executeAction(ctx, cmdCtx, params, alarmAction(params)); err != nil {
		return fmt.Errorf("execute action: %w", err)
	}

	return nil
}

func alarmAction(params map[string]any) string {
	action, hasAction := params["action"].(string)
	if !hasAction {
		return "list"
	}

	return action
}

func (c *AlarmCommand) executeAction(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any, action string) error {
	handler, ok := c.alarmActionHandlers()[action]
	if !ok {
		if err := c.Deps().SendMessage(ctx, cmdCtx.Room, c.Deps().Formatter.FormatHelp(ctx)); err != nil {
			return fmt.Errorf("send message: %w", err)
		}

		return nil
	}

	if err := handler(ctx, cmdCtx, params); err != nil {
		return fmt.Errorf("handler: %w", err)
	}

	return nil
}

func (c *AlarmCommand) alarmActionHandlers() map[string]alarmActionHandler {
	return map[string]alarmActionHandler{
		"set":     c.handleAdd,
		"add":     c.handleAdd,
		"remove":  c.handleRemove,
		"delete":  c.handleRemove,
		"list":    c.handleListAction,
		"clear":   c.handleClearAction,
		"invalid": c.handleInvalid,
	}
}

func (c *AlarmCommand) handleListAction(ctx context.Context, cmdCtx *botcommand.CommandContext, _ map[string]any) error {
	c.Deps().Logger.Debug("Alarm list requested")

	if err := c.handleList(ctx, cmdCtx); err != nil {
		return fmt.Errorf("handle list: %w", err)
	}

	return nil
}

func (c *AlarmCommand) handleClearAction(ctx context.Context, cmdCtx *botcommand.CommandContext, _ map[string]any) error {
	if err := c.handleClear(ctx, cmdCtx); err != nil {
		return fmt.Errorf("handle clear: %w", err)
	}

	return nil
}

func (c *AlarmCommand) handleInvalid(ctx context.Context, cmdCtx *botcommand.CommandContext, _ map[string]any) error {
	c.Deps().Logger.Info("Invalid alarm command received",
		sharedprivacylog.RoomIDAttr(cmdCtx.Room),
	)

	if err := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrInvalidAlarmUsage); err != nil {
		return fmt.Errorf("send error: %w", err)
	}

	return nil
}

func (c *AlarmCommand) CheckDependencies() error {
	if err := c.EnsureBaseDeps(); err != nil {
		return fmt.Errorf("ensure base dependencies: %w", err)
	}

	if c.Deps().Alarm == nil || c.Deps().Matcher == nil || c.Deps().Formatter == nil {
		return stdErrors.New("alarm command services not configured")
	}

	return nil
}

type alarmMutation struct {
	needMemberNameMessage string
	requestedLog          string
	resolve               func(ctx context.Context, room, memberName string) (*alarmTarget, error)
	apply                 func(ctx context.Context, cmdCtx *botcommand.CommandContext, channel *alarmTarget, alarmTypes domain.AlarmTypes) error
}

func (c *AlarmCommand) handleAdd(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any) error {
	return c.handleAlarmMutation(ctx, cmdCtx, params, alarmMutation{
		needMemberNameMessage: messaging.ErrAlarmNeedMemberNameAdd,
		requestedLog:          "Alarm add requested",
		resolve:               c.resolveAlarmAddMember,
		apply:                 c.addAlarmAndReply,
	})
}

func (c *AlarmCommand) handleRemove(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any) error {
	return c.handleAlarmMutation(ctx, cmdCtx, params, alarmMutation{
		needMemberNameMessage: messaging.ErrAlarmNeedMemberNameRemove,
		requestedLog:          "Alarm remove requested",
		resolve:               c.resolveAlarmMember,
		apply:                 c.removeAlarmAndReply,
	})
}

func (c *AlarmCommand) handleAlarmMutation(ctx context.Context, cmdCtx *botcommand.CommandContext, params map[string]any, mutation alarmMutation) error {
	memberName, err := c.requiredAlarmMemberName(ctx, cmdCtx.Room, params, mutation.needMemberNameMessage)
	if err != nil {
		return fmt.Errorf("require alarm member name: %w", err)
	}

	if memberName == "" {
		return nil
	}

	alarmTypes := c.parseAlarmTypes(params)

	if logger := c.Deps().Logger; logger.Enabled(ctx, slog.LevelDebug) {
		logger.Debug(mutation.requestedLog,
			sharedprivacylog.RoomIDAttr(cmdCtx.Room),
			slog.Group("types", slog.Int("count", len(alarmTypes))))
	}

	channel, err := mutation.resolve(ctx, cmdCtx.Room, memberName)
	if stdErrors.Is(err, handlercore.ErrMemberLookupHandled) {
		return nil
	}

	if err != nil {
		return fmt.Errorf("resolve alarm member: %w", err)
	}

	if channel == nil {
		return nil
	}

	if err := mutation.apply(ctx, cmdCtx, channel, alarmTypes); err != nil {
		return fmt.Errorf("apply alarm mutation: %w", err)
	}

	return nil
}

func (c *AlarmCommand) requiredAlarmMemberName(ctx context.Context, room string, params map[string]any, message string) (string, error) {
	memberName, hasMember := params["member"].(string)
	if hasMember && memberName != "" {
		return memberName, nil
	}

	if err := c.Deps().SendError(ctx, room, message); err != nil {
		return "", fmt.Errorf("send error: %w", err)
	}

	return "", nil
}

func (c *AlarmCommand) resolveAlarmAddMember(ctx context.Context, room, memberName string) (*alarmTarget, error) {
	channel, err := c.resolveAlarmMember(ctx, room, memberName)
	if err != nil {
		return nil, fmt.Errorf("%w", err)
	}

	if channel == nil {
		return nil, handlercore.ErrMemberLookupHandled
	}

	blocked, err := handlercore.BlockGraduatedMember(ctx, c.Deps(), room, channel.ChannelID)
	if err != nil {
		return nil, fmt.Errorf("block graduated member: %w", err)
	}

	if blocked {
		return nil, handlercore.ErrMemberLookupHandled
	}

	return channel, nil
}

func (c *AlarmCommand) addAlarmAndReply(ctx context.Context, cmdCtx *botcommand.CommandContext, channel *alarmTarget, alarmTypes domain.AlarmTypes) error {
	added, err := c.Deps().Alarm.AddAlarm(ctx, &domain.AddAlarmRequest{
		RoomID:     cmdCtx.Room,
		ChannelID:  channel.ChannelID,
		HostID:     channel.HostID,
		AlarmTypes: alarmTypes,
	})
	if err != nil {
		c.Deps().Logger.Error("Failed to add alarm",
			slog.String("channel", channel.Name),
			slog.Any("error", err),
		)

		if sendErr := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrAlarmAddFailed); sendErr != nil {
			return fmt.Errorf("send error: %w", sendErr)
		}

		return nil
	}

	message := c.Deps().Formatter.FormatAlarmAdded(ctx, channel.Name, added)

	if err := c.Deps().SendMessage(ctx, cmdCtx.Room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (c *AlarmCommand) resolveAlarmMember(ctx context.Context, room, memberName string) (*alarmTarget, error) {
	if channelID, host, ok := mekparkhost.FindSubscriptionMember(memberName); ok {
		return &alarmTarget{ChannelID: channelID, HostID: host.ID, Name: host.Name}, nil
	}

	channel, err := handlercore.FindMemberWithCandidatesOrError(ctx, c.Deps(), room, memberName, "알람 추가")
	if err != nil {
		return nil, fmt.Errorf("find alarm member: %w", err)
	}

	return &alarmTarget{ChannelID: channel.ID, Name: channel.Name}, nil
}

func (c *AlarmCommand) removeAlarmAndReply(ctx context.Context, cmdCtx *botcommand.CommandContext, channel *alarmTarget, alarmTypes domain.AlarmTypes) error {
	var (
		removed bool
		err     error
	)

	if channel.HostID == "" {
		removed, err = c.Deps().Alarm.RemoveAlarm(ctx, cmdCtx.Room, channel.ChannelID, alarmTypes)
	} else {
		removed, err = c.Deps().Alarm.RemoveHostAlarm(ctx, cmdCtx.Room, channel.ChannelID, channel.HostID, alarmTypes)
	}

	if err != nil {
		c.Deps().Logger.Error("Failed to remove alarm",
			slog.String("channel", channel.Name),
			slog.Any("error", err),
		)

		if err := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrAlarmRemoveFailed); err != nil {
			return fmt.Errorf("send error: %w", err)
		}

		return nil
	}

	message := c.Deps().Formatter.FormatAlarmRemoved(ctx, channel.Name, removed)

	if err := c.Deps().SendMessage(ctx, cmdCtx.Room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (c *AlarmCommand) handleList(ctx context.Context, cmdCtx *botcommand.CommandContext) error {
	entries, err := c.Deps().Alarm.ListRoomAlarmsView(ctx, cmdCtx.Room)
	if err != nil {
		if err := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrAlarmListFailed); err != nil {
			return fmt.Errorf("send error: %w", err)
		}

		return nil
	}

	alarmInfos := make([]formatter.AlarmListEntry, 0, len(entries))
	for _, entry := range entries {
		alarmInfos = append(alarmInfos, formatter.AlarmListEntry{
			MemberName: entry.MemberName,
			AlarmTypes: entry.AlarmTypes,
		})
	}

	message := c.Deps().Formatter.FormatAlarmList(ctx, alarmInfos)

	if err := c.Deps().SendMessage(ctx, cmdCtx.Room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (c *AlarmCommand) handleClear(ctx context.Context, cmdCtx *botcommand.CommandContext) error {
	count, err := c.Deps().Alarm.ClearRoomAlarms(ctx, cmdCtx.Room)
	if err != nil {
		c.Deps().Logger.Error("Failed to clear alarms", slog.Any("error", err))

		if err := c.Deps().SendError(ctx, cmdCtx.Room, messaging.ErrAlarmClearFailed); err != nil {
			return fmt.Errorf("send error: %w", err)
		}

		return nil
	}

	message := c.Deps().Formatter.FormatAlarmCleared(ctx, count)

	if err := c.Deps().SendMessage(ctx, cmdCtx.Room, message); err != nil {
		return fmt.Errorf("send message: %w", err)
	}

	return nil
}

func (c *AlarmCommand) parseAlarmTypes(params map[string]any) domain.AlarmTypes {
	typeStr, hasType := params["type"].(string)
	if !hasType || typeStr == "" {
		return domain.DefaultAlarmTypes
	}

	if alarmTypes, ok := alarmTypesByName[typeStr]; ok {
		return alarmTypes
	}

	return domain.DefaultAlarmTypes
}

var alarmTypesByName = map[string]domain.AlarmTypes{
	"방송":   {domain.AlarmTypeLive},
	"커뮤니티": {domain.AlarmTypeCommunity},
	"쇼츠":   {domain.AlarmTypeShorts},
	"전체":   domain.AllAlarmTypes,
}
