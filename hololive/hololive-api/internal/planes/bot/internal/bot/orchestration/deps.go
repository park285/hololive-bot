// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package orchestration

import (
	"context"
	"log/slog"
	"time"

	"github.com/park285/iris-client-go/v4/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/transport"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-api/internal/service/acl"
	configsettings "github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/kakaoroom"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
)

// BotIrisClient는 bot orchestration이 발송과 room catalog 구성에 사용하는 Iris 계약입니다.
type BotIrisClient interface {
	iris.BotClient
	transport.ReplyClient
	kakaoroom.IrisRooms
}

// BotCache는 Bot이 명령 admission(B·DoMulti)과 기동 readiness 대기(WaitUntilReady)에 쓰는 cache 작업이다.
type BotCache interface {
	cache.LowLevelCache
	WaitUntilReady(ctx context.Context, timeout time.Duration) error
}

type Dependencies struct {
	BotSelfUser           string
	IrisBaseURL           string
	Notification          configsettings.NotificationConfig
	CalendarImageCacheDir string
	CalendarEntryCacheTTL time.Duration
	Logger                *slog.Logger
	Client                BotIrisClient
	// ReplyOutbox는 모든 명령 응답을 기록하는 durable reply 원장이다. Iris 발송은 outbox dispatcher만 한다.
	ReplyOutbox          transport.ReplyOutboxWriter
	MessageAdapter       *messaging.MessageAdapter
	Formatter            *formatter.ResponseFormatter
	MessageStrings       *messagestrings.Store
	MarkdownReplies      bool
	Cache                BotCache
	Postgres             database.Client
	MemberRepository     *member.Repository
	Holodex              domain.StreamProvider
	Alarm                handlercore.AlarmService
	Matcher              *matcher.Matcher
	MembersData          domain.MemberDataProvider
	ACL                  *acl.Service
	MajorEventRepository handlercore.MajorEventRepository
	MemberNews           handlercore.MemberNewsService
	CommandBuilders      []orchcmd.CommandBuilder
}
