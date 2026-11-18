package orchestration

import (
	"context"
	"log/slog"
	"time"

	"github.com/park285/iris-client-go/v5/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-api/internal/service/acl"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/kakaoroom"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

type BotIrisClient interface {
	iris.BotClient
	transport.ReplyClient
	kakaoroom.IrisRooms
}

type BotCache interface {
	cache.LowLevelCache
	WaitUntilReady(ctx context.Context, timeout time.Duration) error
}

type Dependencies struct {
	BotSelfUser           string
	IrisBaseURL           string
	CalendarEntryCacheTTL time.Duration
	Logger                *slog.Logger
	Client                BotIrisClient
	ReplyOutbox           transport.ReplyOutboxWriter
	MessageAdapter        *messaging.MessageAdapter
	Formatter             *formatter.ResponseFormatter
	MessageStrings        *messagestrings.Store
	MarkdownReplies       bool
	Cache                 BotCache
	Postgres              database.Client
	MemberRepository      *member.Repository
	Holodex               domain.StreamProvider
	Alarm                 handlercore.AlarmService
	Matcher               *matcher.Matcher
	MembersData           domain.MemberDataProvider
	ACL                   *acl.Service
}
