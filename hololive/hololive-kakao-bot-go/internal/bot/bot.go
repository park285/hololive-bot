package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	appErrors "github.com/kapu/hololive-shared/pkg/errors"
	"github.com/kapu/hololive-shared/pkg/iris"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/majorevent"
	"github.com/kapu/hololive-shared/pkg/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/membernews"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
)

const (
	commandKeyAlarm            = "alarm"
	commandKeyNewsSubscription = "news_subscription"
)

var (
	numericRoomRegex = regexp.MustCompile(`^\d+$`)
)

type streamRuntime interface {
	domain.StreamProvider
	Stop()
}

// Bot: 홀로라이브 봇의 핵심 상태와 의존성(서비스, 캐시, 핸들러 등)을 관리하는 메인 구조체
type Bot struct {
	botSelfUser      string
	irisBaseURL      string
	notification     config.NotificationConfig
	logger           *slog.Logger
	irisClient       iris.Client
	messageAdapter   *adapter.MessageAdapter
	formatter        *adapter.ResponseFormatter
	cache            *cache.Service
	postgres         *database.PostgresService
	holodex          streamRuntime
	chzzk            *chzzk.Client
	twitch           *twitch.Client
	officialProfiles *member.ProfileService
	alarm            domain.AlarmCRUD
	matcher          *matcher.MemberMatcher
	commandRegistry  *command.Registry
	statsRepo        *youtube.StatsRepository
	acl              *acl.Service
	majorEventRepo   *majorevent.Repository
	memberNews       *membernews.Service
	membersData      domain.MemberDataProvider
	stopCh           chan struct{}
	doneCh           chan struct{}
	selfSender       string
	workerPool       *workerpool.Pool
}

// NewBot: 필요한 의존성(Dependencies)을 주입받아 새로운 Bot 인스턴스를 생성하고 초기화합니다.
func NewBot(deps *Dependencies) (*Bot, error) {
	if deps == nil {
		return nil, fmt.Errorf("bot dependencies are required")
	}

	deps.Logger.Info("Bot dependency snapshot", slog.Bool("stats_repo", deps.YouTubeStatsRepo != nil))
	if deps.Logger == nil {
		return nil, fmt.Errorf("logger dependency is required")
	}
	if deps.Client == nil {
		return nil, fmt.Errorf("iris client dependency is required")
	}
	if deps.MessageAdapter == nil {
		return nil, fmt.Errorf("message adapter dependency is required")
	}
	if deps.Formatter == nil {
		return nil, fmt.Errorf("response formatter dependency is required")
	}
	if deps.Cache == nil {
		return nil, fmt.Errorf("cache dependency is required")
	}
	if deps.Postgres == nil {
		return nil, fmt.Errorf("postgres dependency is required")
	}
	if deps.Holodex == nil {
		return nil, fmt.Errorf("holodex dependency is required")
	}
	if deps.Profiles == nil {
		return nil, fmt.Errorf("profile service dependency is required")
	}
	if deps.Alarm == nil {
		return nil, fmt.Errorf("alarm service dependency is required")
	}
	if deps.Matcher == nil {
		return nil, fmt.Errorf("matcher dependency is required")
	}
	if deps.MembersData == nil {
		return nil, fmt.Errorf("member data dependency is required")
	}
	if deps.YouTubeStatsRepo == nil {
		return nil, fmt.Errorf("youtube stats repository dependency is required")
	}

	holodexRuntime, ok := deps.Holodex.(streamRuntime)
	if !ok {
		return nil, fmt.Errorf("holodex dependency does not implement stream runtime interface")
	}

	bot := &Bot{
		botSelfUser:      deps.BotSelfUser,
		irisBaseURL:      deps.IrisBaseURL,
		notification:     deps.Notification,
		logger:           deps.Logger,
		irisClient:       deps.Client,
		messageAdapter:   deps.MessageAdapter,
		formatter:        deps.Formatter,
		cache:            deps.Cache,
		postgres:         deps.Postgres,
		holodex:          holodexRuntime,
		chzzk:            deps.Chzzk,
		twitch:           deps.Twitch,
		officialProfiles: deps.Profiles,
		alarm:            deps.Alarm,
		matcher:          deps.Matcher,
		statsRepo:        deps.YouTubeStatsRepo,
		acl:              deps.ACL,
		majorEventRepo:   deps.MajorEventRepo,
		memberNews:       deps.MemberNews,
		membersData:      deps.MembersData,
		workerPool:       deps.WorkerPool,
		stopCh:           make(chan struct{}),
		doneCh:           make(chan struct{}),
		selfSender:       stringutil.Normalize(deps.BotSelfUser),
	}

	bot.initializeCommands()

	return bot, nil
}

func (b *Bot) initializeCommands() {
	registry := command.NewRegistry()
	b.commandRegistry = registry

	deps := &command.Dependencies{
		Holodex:          b.holodex,
		Chzzk:            b.chzzk,
		Cache:            b.cache,
		Alarm:            b.alarm,
		Matcher:          b.matcher,
		OfficialProfiles: b.officialProfiles,
		StatsRepo:        b.statsRepo,
		MemberNews:       b.memberNews,
		MembersData:      b.membersData,
		Formatter:        b.formatter,
		SendMessage:      b.sendMessage,
		SendImage:        b.sendImage,
		SendError:        b.sendError,
		Logger:           b.logger,
	}

	deps.Dispatcher = command.NewSequentialDispatcher(registry, b.normalizeCommand)

	b.logger.Info("Stats repository detected", slog.Bool("available", deps.StatsRepo != nil))

	commandsList := []command.Command{
		command.NewHelpCommand(deps),
		command.NewLiveCommand(deps),
		command.NewUpcomingCommand(deps),
		command.NewScheduleCommand(deps),
		command.NewAlarmCommand(deps),
		command.NewMemberInfoCommand(deps),
		command.NewSubscriberCommand(deps),
		command.NewStatsCommand(deps),
	}

	if b.majorEventRepo != nil {
		b.logger.Info("MajorEvent command enabled")
		commandsList = append(commandsList, command.NewMajorEventCommand(deps, b.majorEventRepo))
	}

	if deps.MemberNews != nil {
		b.logger.Info("MemberNews commands enabled")
		commandsList = append(commandsList,
			command.NewMemberNewsCommand(deps),
			command.NewMemberNewsSubscriptionCommand(deps),
		)
	}

	for _, cmd := range commandsList {
		registry.Register(cmd)
	}

	b.logger.Info("Commands initialized", slog.Int("count", registry.Count()))
}

// Start: 봇 서비스를 시작한다. Valkey/Iris 연결 확인 후 Context가 종료될 때까지 대기합니다.
func (b *Bot) Start(ctx context.Context) error {
	b.logger.Info("Starting Hololive KakaoTalk Bot...")

	if err := b.cache.WaitUntilReady(ctx, constants.ValkeyConfig.ReadyTimeout); err != nil {
		return fmt.Errorf("valkey connection timeout: %w", err)
	}
	b.logger.Info("Valkey connected")

	if err := b.waitUntilIrisReady(
		ctx,
		constants.IrisConnection.ReadyTimeout,
		constants.IrisConnection.RetryInterval,
		constants.IrisConnection.PingTimeout,
	); err != nil {
		b.logger.Warn("Iris server not ready at startup; continuing in degraded mode",
			slog.String("base_url", b.irisBaseURL),
			slog.Any("error", err),
		)
	} else {
		b.logger.Info("Iris server connected")
	}

	b.logger.Info("Bot started successfully")

	select {
	case <-ctx.Done():
		b.logger.Info("Context canceled, shutting down...")
		return fmt.Errorf("context canceled: %w", ctx.Err())
	case <-b.stopCh:
		b.logger.Info("Stop signal received")
		return nil
	}
}

func (b *Bot) waitUntilIrisReady(ctx context.Context, timeout, retryInterval, pingTimeout time.Duration) error {
	waitCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()

	ticker := time.NewTicker(retryInterval)
	defer ticker.Stop()

	attempt := 0
	startedAt := time.Now()
	lastWarnLoggedAt := time.Time{}
	for {
		attempt++
		pingCtx, pingCancel := context.WithTimeout(waitCtx, pingTimeout)
		ready := b.irisClient.Ping(pingCtx)
		pingCancel()

		if ready {
			if attempt > 1 {
				b.logger.Info("Iris server became ready after retry",
					slog.Int("attempt", attempt),
					slog.Duration("elapsed", time.Since(startedAt)),
				)
			}
			return nil
		}

		now := time.Now()
		// 과도한 경고 로그를 줄이기 위해 최초 1회 + 이후 분당 1회만 기록
		if attempt == 1 || lastWarnLoggedAt.IsZero() || now.Sub(lastWarnLoggedAt) >= time.Minute {
			b.logger.Warn("Iris server not ready, retrying",
				slog.Int("attempt", attempt),
				slog.Duration("retry_interval", retryInterval),
				slog.Duration("elapsed", now.Sub(startedAt)),
			)
			lastWarnLoggedAt = now
		}

		select {
		case <-waitCtx.Done():
			if errors.Is(waitCtx.Err(), context.DeadlineExceeded) {
				return fmt.Errorf("timeout after %s", timeout)
			}
			return fmt.Errorf("canceled: %w", waitCtx.Err())
		case <-ticker.C:
		}
	}
}

// HandleMessage: Iris webhook으로부터 수신한 메시지를 처리합니다.
// HTTP webhook 핸들러에서 호출하기 위해 public으로 노출됩니다.
func (b *Bot) HandleMessage(ctx context.Context, message *iris.Message) {
	commandType := "unknown"

	isNumericRoom := message.Room != "" && numericRoomRegex.MatchString(message.Room)
	chatID := message.Room
	if !isNumericRoom && message.JSON != nil {
		chatID = message.JSON.ChatID
	}

	// 한글 방 이름 유지
	roomName := message.Room

	// userID와 userName 분리
	userID := "unknown"
	userName := userID // 기본값

	if message.JSON != nil && message.JSON.UserID != "" {
		userID = message.JSON.UserID // 숫자 ID
		userName = userID            // userName도 업데이트
	}

	if message.Sender != nil {
		userName = *message.Sender // 한글 이름 우선
	}

	defer func() {
		if r := recover(); r != nil {
			b.logger.Error("Panic in handleMessage",
				slog.Any("panic", r),
				slog.String("command", commandType),
			)
		}
	}()

	if b.isSelfSender(userName) {
		b.logger.Debug("Skipping self-issued message",
			slog.String("user", userName),
			slog.String("room", chatID),
			slog.String("payload", message.Msg),
		)
		return
	}

	// ACL: 허용된 방이 아니면 메시지 무시
	if b.acl != nil && !b.acl.IsRoomAllowed(roomName, chatID) {
		b.logger.Debug("Room not in ACL whitelist, ignoring message",
			slog.String("room", chatID),
			slog.String("room_name", roomName),
			slog.String("user_name", userName),
		)
		return
	}

	parsed := b.messageAdapter.ParseMessage(message)
	commandType = parsed.Type.String()

	if parsed.Type == domain.CommandUnknown {
		b.logger.Debug("Unknown command ignored",
			slog.String("msg", message.Msg),
			slog.String("room", chatID),
			slog.String("user_name", userName),
		)
		return // 알 수 없는 명령어는 무시함
	}

	b.logger.Info("Command received",
		slog.String("raw", parsed.RawMessage),
		slog.String("type", commandType),
		slog.String("user_id", userID),
		slog.String("user_name", userName),
		slog.String("room", chatID),
		slog.String("room_name", roomName),
	)

	cmdCtx := domain.NewCommandContext(chatID, roomName, userID, userName, message.Msg, false)

	if err := b.executeCommand(ctx, cmdCtx, parsed.Type, parsed.Params); err != nil {
		b.logger.Error("Failed to execute command", slog.Any("error", err))
		errorMsg := b.getErrorMessage(err, commandType)
		if chatID != "" {
			b.sendError(ctx, chatID, errorMsg)
		}
	}
}

func (b *Bot) executeCommand(ctx context.Context, cmdCtx *domain.CommandContext, cmdType domain.CommandType, params map[string]any) error {
	if b.commandRegistry == nil {
		return fmt.Errorf("command registry is not initialized")
	}

	key, normalizedParams := b.normalizeCommand(cmdType, params)

	if err := b.commandRegistry.Execute(ctx, cmdCtx, key, normalizedParams); err != nil {
		if errors.Is(err, command.ErrUnknownCommand) {
			b.logger.Warn("Unknown command", slog.String("type", cmdType.String()))
			if sendErr := b.sendMessage(ctx, cmdCtx.Room, adapter.ErrUnknownCommand); sendErr != nil {
				return fmt.Errorf("failed to send unknown command message: %w", sendErr)
			}
			return nil
		}
		return fmt.Errorf("execute command: %w", err)
	}

	return nil
}

func (b *Bot) normalizeCommand(cmdType domain.CommandType, params map[string]any) (string, map[string]any) {
	return normalizeCommandKey(cmdType, params)
}

func (b *Bot) isSelfSender(sender string) bool {
	canonical := stringutil.Normalize(sender)
	if canonical == "" || b.selfSender == "" {
		return false
	}
	return canonical == b.selfSender
}

func (b *Bot) sendMessage(ctx context.Context, room, message string) error {
	ctx, cancel := context.WithTimeout(ctx, constants.RequestTimeout.BotCommand)
	defer cancel()

	if err := b.irisClient.SendMessage(ctx, room, message); err != nil {
		serviceErr := appErrors.NewServiceError("failed to send message", "iris", "send_message", err)
		return fmt.Errorf("failed to send message to room %s: %w", room, serviceErr)
	}
	return nil
}

func (b *Bot) sendImage(ctx context.Context, room, imageBase64 string) error {
	ctx, cancel := context.WithTimeout(ctx, constants.RequestTimeout.BotCommand)
	defer cancel()

	if err := b.irisClient.SendImage(ctx, room, imageBase64); err != nil {
		serviceErr := appErrors.NewServiceError("failed to send image", "iris", "send_image", err)
		return fmt.Errorf("failed to send image to room %s: %w", room, serviceErr)
	}
	return nil
}

func (b *Bot) sendError(ctx context.Context, room, errorMsg string) error {
	message := b.formatter.FormatError(errorMsg)
	if err := b.sendMessage(ctx, room, message); err != nil {
		return fmt.Errorf("failed to send error message: %w", err)
	}
	return nil
}

func (b *Bot) getErrorMessage(err error, commandType string) string {
	if err == nil {
		return ""
	}

	msg := err.Error()

	if strings.Contains(msg, "외부 AI 서비스 장애") {
		return msg
	}

	// 서비스 에러 체크 (Iris 연결 실패)
	var serviceErr *appErrors.ServiceError
	if errors.As(err, &serviceErr) && strings.EqualFold(serviceErr.Service, "iris") {
		return adapter.ErrIrisConnectionFailed
	}

	// API 에러 체크 (외부 API 호출 실패)
	var apiErr *appErrors.APIError
	if errors.As(err, &apiErr) {
		return adapter.ErrExternalAPICallFailed
	}

	// 키 로테이션 에러 체크
	var keyRotationErr *appErrors.KeyRotationError
	if errors.As(err, &keyRotationErr) {
		return adapter.ErrExternalAPICallFailed
	}

	// 캐시 에러 체크
	var cacheErr *appErrors.CacheError
	if errors.As(err, &cacheErr) {
		return adapter.ErrCacheConnectionFailed
	}

	// 유효성 검사 에러 체크
	var validationErr *appErrors.ValidationError
	if errors.As(err, &validationErr) {
		return msg
	}

	if strings.Contains(msg, "Valkey") || strings.Contains(msg, "cache") {
		return adapter.ErrCacheConnectionFailed
	}

	return fmt.Sprintf(adapter.ErrCommandProcessingFailed, commandType)
}

// Shutdown: 봇의 리소스를 정리하고 안전하게 종료합니다.
func (b *Bot) Shutdown(ctx context.Context) error {
	b.logger.Info("Shutting down bot...")

	if b.workerPool != nil {
		if err := b.workerPool.ShutdownWait(ctx); err != nil {
			b.logger.Warn("Worker pool shutdown error", slog.Any("error", err))
		}
	}

	if b.holodex != nil {
		b.holodex.Stop()
	}

	if b.cache != nil {
		if err := b.cache.Close(); err != nil {
			b.logger.Warn("Error closing cache", slog.Any("error", err))
		}
	}

	if b.postgres != nil {
		if err := b.postgres.Close(); err != nil {
			b.logger.Warn("Error closing postgres", slog.Any("error", err))
		}
	}

	b.logger.Info("Bot shutdown complete")
	close(b.doneCh)
	return nil
}
