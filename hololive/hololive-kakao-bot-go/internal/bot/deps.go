package bot

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/adapter"
	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/majorevent"
	"github.com/kapu/hololive-shared/pkg/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/membernews"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
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
	Cache            *cache.Service
	Postgres         *database.PostgresService
	MemberRepo       *member.Repository
	MemberCache      *member.Cache
	Holodex          domain.StreamProvider
	Chzzk            *chzzk.Client
	Twitch           *twitch.Client
	Profiles         *member.ProfileService
	Alarm            domain.AlarmCRUD
	Matcher          *matcher.MemberMatcher
	MembersData      domain.MemberDataProvider
	Service          *youtube.Service
	Scheduler        *youtube.Scheduler
	YouTubeStatsRepo youtube.StatsCommandRepository
	Activity         *activity.Logger
	Settings         *settings.Service
	ACL              *acl.Service
	MajorEventRepo   *majorevent.Repository
	MemberNews       *membernews.Service
	WorkerPool       *workerpool.Pool
}
