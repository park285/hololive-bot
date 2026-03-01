package bot

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/config"
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
	statsRepo        youtube.StatsCommandRepository
	acl              *acl.Service
	majorEventRepo   *majorevent.Repository
	memberNews       *membernews.Service
	membersData      domain.MemberDataProvider
	stopCh           chan struct{}
	doneCh           chan struct{}
	selfSender       string
	workerPool       *workerpool.Pool
	ingress          *MessageIngress
	commandExecutor  *CommandRouter
	transport        *CommandTransport
	lifecycle        *BotLifecycle
}

// NewBot: 필요한 의존성(Dependencies)을 주입받아 새로운 Bot 인스턴스를 생성하고 초기화합니다.
func NewBot(deps *Dependencies) (*Bot, error) {
	if deps == nil {
		return nil, fmt.Errorf("bot dependencies are required")
	}

	if deps.Logger == nil {
		return nil, fmt.Errorf("logger dependency is required")
	}
	deps.Logger.Info("Bot dependency snapshot", slog.Bool("stats_repo", deps.YouTubeStatsRepo != nil))
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

	bot.transport = NewCommandTransport(bot.irisClient, bot.formatter)
	bot.ingress = NewMessageIngress(bot.messageAdapter, bot.acl, bot.logger, bot.selfSender)
	bot.lifecycle = NewBotLifecycle(
		bot.logger,
		bot.cache,
		bot.irisClient,
		bot.irisBaseURL,
		bot.stopCh,
		bot.doneCh,
		bot.workerPool,
		bot.holodex,
		bot.postgres,
	)

	bot.initializeCommands()

	return bot, nil
}

func (b *Bot) ensureCommandExecutor() *CommandRouter {
	if b.commandExecutor == nil {
		b.commandExecutor = NewCommandRouter(b.commandRegistry, b.logger, b.sendMessage)
	}
	return b.commandExecutor
}

func (b *Bot) ensureIngress() *MessageIngress {
	if b.ingress == nil {
		b.ingress = NewMessageIngress(b.messageAdapter, b.acl, b.logger, b.selfSender)
	}
	return b.ingress
}

func (b *Bot) ensureTransport() *CommandTransport {
	if b.transport == nil {
		b.transport = NewCommandTransport(b.irisClient, b.formatter)
	}
	return b.transport
}

func (b *Bot) ensureLifecycle() *BotLifecycle {
	if b.lifecycle == nil {
		b.lifecycle = NewBotLifecycle(
			b.logger,
			b.cache,
			b.irisClient,
			b.irisBaseURL,
			b.stopCh,
			b.doneCh,
			b.workerPool,
			b.holodex,
			b.postgres,
		)
	}
	return b.lifecycle
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

	deps.Dispatcher = command.NewSequentialDispatcher(registry, normalizeCommandKey)

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

	b.commandExecutor = NewCommandRouter(registry, b.logger, b.sendMessage)
	b.logger.Info("Commands initialized", slog.Int("count", registry.Count()))
}

// Start: 봇 서비스를 시작한다. Valkey/Iris 연결 확인 후 Context가 종료될 때까지 대기합니다.
func (b *Bot) Start(ctx context.Context) error {
	return b.ensureLifecycle().Start(ctx)
}

func (b *Bot) waitUntilIrisReady(ctx context.Context, timeout, retryInterval, pingTimeout time.Duration) error {
	return b.ensureLifecycle().WaitUntilIrisReady(ctx, timeout, retryInterval, pingTimeout)
}

// HandleMessage: Iris webhook으로부터 수신한 메시지를 처리합니다.
// HTTP webhook 핸들러에서 호출하기 위해 public으로 노출됩니다.
func (b *Bot) HandleMessage(ctx context.Context, message *iris.Message) {
	commandType := "unknown"

	defer func() {
		if r := recover(); r != nil {
			b.logger.Error("Panic in handleMessage",
				slog.Any("panic", r),
				slog.String("command", commandType),
			)
		}
	}()

	envelope, ok := b.ensureIngress().Prepare(message)
	if !ok {
		return
	}

	commandType = envelope.CommandType
	cmdCtx := domain.NewCommandContext(
		envelope.ChatID,
		envelope.RoomName,
		envelope.UserID,
		envelope.UserName,
		message.Msg,
		false,
	)

	if err := b.executeCommand(ctx, cmdCtx, envelope.Parsed.Type, envelope.Parsed.Params); err != nil {
		b.logger.Error("Failed to execute command", slog.Any("error", err))
		errorMsg := b.getErrorMessage(err, commandType)
		if envelope.ChatID != "" {
			b.sendError(ctx, envelope.ChatID, errorMsg)
		}
	}
}

func (b *Bot) executeCommand(ctx context.Context, cmdCtx *domain.CommandContext, cmdType domain.CommandType, params map[string]any) error {
	return b.ensureCommandExecutor().Execute(ctx, cmdCtx, cmdType, params)
}

func (b *Bot) sendMessage(ctx context.Context, room, message string) error {
	return b.ensureTransport().SendMessage(ctx, room, message)
}

func (b *Bot) sendImage(ctx context.Context, room, imageBase64 string) error {
	return b.ensureTransport().SendImage(ctx, room, imageBase64)
}

func (b *Bot) sendError(ctx context.Context, room, errorMsg string) error {
	return b.ensureTransport().SendError(ctx, room, errorMsg)
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
	return b.ensureLifecycle().Shutdown(ctx)
}
