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
	"log/slog"
	"time"

	"github.com/park285/iris-client-go/v2/iris"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	configsettings "github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/activity"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/messagestrings"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
)

type Dependencies struct {
	BotSelfUser           string
	IrisBaseURL           string
	Notification          configsettings.NotificationConfig
	CalendarImageCacheDir string
	CalendarEntryCacheTTL time.Duration
	Logger                *slog.Logger
	Client                iris.BotClient
	MessageAdapter        *messaging.MessageAdapter
	Formatter             *formatter.ResponseFormatter
	MessageStrings        *messagestrings.Store
	MarkdownReplies       bool
	Cache                 cache.Client
	Postgres              database.Client
	MemberRepository      *member.Repository
	MemberCache           *member.Cache
	Holodex               domain.StreamProvider
	Chzzk                 *chzzk.Client
	Twitch                *twitch.Client
	Profiles              *member.ProfileService
	Alarm                 domain.AlarmCRUD
	Matcher               *matcher.Matcher
	MembersData           domain.MemberDataProvider
	Service               youtube.Service
	Activity              *activity.Logger
	Settings              settings.ReadWriter
	ACL                   *acl.Service
	MajorEventRepository  handlercore.MajorEventRepository
	MemberNews            handlercore.MemberNewsService
	CommandBuilders       []orchcmd.CommandBuilder
}

type coreDependencies struct {
	botSelfUser           string
	irisBaseURL           string
	notification          configsettings.NotificationConfig
	calendarImageCacheDir string
	calendarEntryCacheTTL time.Duration
	logger                *slog.Logger
}

type messagingDependencies struct {
	client          iris.BotClient
	messageAdapter  *messaging.MessageAdapter
	formatter       *formatter.ResponseFormatter
	messageStrings  *messagestrings.Store
	markdownReplies bool
}

type dataDependencies struct {
	cache            cache.Client
	postgres         database.Client
	memberRepository *member.Repository
	memberCache      *member.Cache
}

type streamDependencies struct {
	holodex     domain.StreamProvider
	chzzk       *chzzk.Client
	twitch      *twitch.Client
	profiles    *member.ProfileService
	alarm       domain.AlarmCRUD
	matcher     *matcher.Matcher
	membersData domain.MemberDataProvider
	service     youtube.Service
}

type supportDependencies struct {
	activity *activity.Logger
	settings settings.ReadWriter
	acl      *acl.Service
}

type featureDependencies struct {
	majorEventRepository handlercore.MajorEventRepository
	memberNews           handlercore.MemberNewsService
	commandBuilders      []orchcmd.CommandBuilder
}

func (d *Dependencies) coreDeps() coreDependencies {
	if d == nil {
		return coreDependencies{}
	}

	return coreDependencies{
		botSelfUser:           d.BotSelfUser,
		irisBaseURL:           d.IrisBaseURL,
		notification:          d.Notification,
		calendarImageCacheDir: d.CalendarImageCacheDir,
		calendarEntryCacheTTL: d.CalendarEntryCacheTTL,
		logger:                d.Logger,
	}
}

func (d *Dependencies) messagingDeps() messagingDependencies {
	if d == nil {
		return messagingDependencies{}
	}

	return messagingDependencies{
		client:          d.Client,
		messageAdapter:  d.MessageAdapter,
		formatter:       d.Formatter,
		messageStrings:  d.MessageStrings,
		markdownReplies: d.MarkdownReplies,
	}
}

func (d *Dependencies) dataDeps() dataDependencies {
	if d == nil {
		return dataDependencies{}
	}

	return dataDependencies{
		cache:            d.Cache,
		postgres:         d.Postgres,
		memberRepository: d.MemberRepository,
		memberCache:      d.MemberCache,
	}
}

func (d *Dependencies) streamDeps() streamDependencies {
	if d == nil {
		return streamDependencies{}
	}

	return streamDependencies{
		holodex:     d.Holodex,
		chzzk:       d.Chzzk,
		twitch:      d.Twitch,
		profiles:    d.Profiles,
		alarm:       d.Alarm,
		matcher:     d.Matcher,
		membersData: d.MembersData,
		service:     d.Service,
	}
}

func (d *Dependencies) supportDeps() supportDependencies {
	if d == nil {
		return supportDependencies{}
	}

	return supportDependencies{
		activity: d.Activity,
		settings: d.Settings,
		acl:      d.ACL,
	}
}

func (d *Dependencies) featureDeps() featureDependencies {
	if d == nil {
		return featureDependencies{}
	}

	return featureDependencies{
		majorEventRepository: d.MajorEventRepository,
		memberNews:           d.MemberNews,
		commandBuilders:      orchcmd.CloneCommandBuilders(d.CommandBuilders),
	}
}
