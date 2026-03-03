package bot

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

// Dependencies: 봇 실행에 필요한 모든 의존성을 담는 구조체 (Service Locator 패턴)
type Dependencies struct {
	BotSelfUser      string
	IrisBaseURL      string
	Notification     config.NotificationConfig
	Logger           *slog.Logger
	Client           iris.Client
	MessageAdapter   *adapter.MessageAdapter
	Formatter        *adapter.ResponseFormatter
	Cache            cache.Client
	Postgres         database.Client
	MemberRepo       *member.Repository
	MemberCache      *member.Cache
	Holodex          domain.StreamProvider
	Chzzk            *chzzk.Client
	Twitch           *twitch.Client
	Profiles         *member.ProfileService
	Alarm            domain.AlarmCRUD
	Matcher          *matcher.MemberMatcher
	MembersData      member.DataProvider
	Service          youtube.Service
	Scheduler        youtube.Scheduler
	YouTubeStatsRepo youtube.StatsCommandRepository
	Activity         *activity.Logger
	Settings         settings.ReadWriter
	ACL              *acl.Service
	MajorEventRepo   command.MajorEventRepository
	MemberNews       command.MemberNewsService
	WorkerPool       *workerpool.Pool
}
