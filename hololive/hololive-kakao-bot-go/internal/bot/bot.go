package bot

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/stringutil"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
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
	cache            cache.Client
	postgres         database.Client
	holodex          streamRuntime
	chzzk            *chzzk.Client
	twitch           *twitch.Client
	officialProfiles *member.ProfileService
	alarm            domain.AlarmCRUD
	matcher          *matcher.MemberMatcher
	commandRegistry  *command.Registry
	statsRepo        youtube.StatsCommandRepository
	acl              *acl.Service
	majorEventRepo   command.MajorEventRepository
	memberNews       command.MemberNewsService
	commandFactories []command.Factory
	membersData      member.DataProvider
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
	holodexRuntime, err := validateBotDependencies(deps)
	if err != nil {
		return nil, err
	}
	core := deps.coreDeps()
	messaging := deps.messagingDeps()
	data := deps.dataDeps()
	stream := deps.streamDeps()
	support := deps.supportDeps()
	feature := deps.featureDeps()

	bot := &Bot{
		botSelfUser:      core.botSelfUser,
		irisBaseURL:      core.irisBaseURL,
		notification:     core.notification,
		logger:           core.logger,
		irisClient:       messaging.client,
		messageAdapter:   messaging.messageAdapter,
		formatter:        messaging.formatter,
		cache:            data.cache,
		postgres:         data.postgres,
		holodex:          holodexRuntime,
		chzzk:            stream.chzzk,
		twitch:           stream.twitch,
		officialProfiles: stream.profiles,
		alarm:            stream.alarm,
		matcher:          stream.matcher,
		statsRepo:        stream.youTubeStatsRepo,
		acl:              support.acl,
		majorEventRepo:   feature.majorEventRepo,
		memberNews:       feature.memberNews,
		commandFactories: feature.commandFactories,
		membersData:      stream.membersData,
		workerPool:       support.workerPool,
		stopCh:           make(chan struct{}),
		doneCh:           make(chan struct{}),
		selfSender:       stringutil.Normalize(core.botSelfUser),
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

	factories := append([]command.Factory{}, command.DefaultFactories()...)

	if b.majorEventRepo != nil {
		b.logger.Info("MajorEvent command enabled")
		factories = append(factories, command.NewMajorEventFactory(b.majorEventRepo))
	}

	if deps.MemberNews != nil {
		b.logger.Info("MemberNews commands enabled")
		factories = append(factories, command.MemberNewsFactories()...)
	}

	if len(b.commandFactories) > 0 {
		b.logger.Info("External command factories enabled", slog.Int("count", len(b.commandFactories)))
		factories = append(factories, b.commandFactories...)
	}

	commandsList := command.BuildCommands(deps, factories...)
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

// Shutdown: 봇의 리소스를 정리하고 안전하게 종료합니다.
func (b *Bot) Shutdown(ctx context.Context) error {
	return b.ensureLifecycle().Shutdown(ctx)
}
