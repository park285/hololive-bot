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

package bot

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/kapu/hololive-shared/pkg/service/youtube/stats"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

// Dependencies: 봇 실행에 필요한 모든 의존성을 담는 구조체 (Service Locator 패턴).
type Dependencies struct {
	BotSelfUser      string
	IrisBaseURL      string
	Notification     config.NotificationConfig
	Logger           *slog.Logger
	Client           irisClient
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
	YouTubeStatsRepo stats.StatsCommandRepository
	Activity         *activity.Logger
	Settings         settings.ReadWriter
	ACL              *acl.Service
	MajorEventRepo   command.MajorEventRepository
	MemberNews       command.MemberNewsService
	CommandBuilders  []CommandBuilder
	WorkerPool       *workerpool.Pool
}

type coreDependencies struct {
	botSelfUser  string
	irisBaseURL  string
	notification config.NotificationConfig
	logger       *slog.Logger
}

type messagingDependencies struct {
	client         irisClient
	messageAdapter *adapter.MessageAdapter
	formatter      *adapter.ResponseFormatter
}

type dataDependencies struct {
	cache       cache.Client
	postgres    database.Client
	memberRepo  *member.Repository
	memberCache *member.Cache
}

type streamDependencies struct {
	holodex          domain.StreamProvider
	chzzk            *chzzk.Client
	twitch           *twitch.Client
	profiles         *member.ProfileService
	alarm            domain.AlarmCRUD
	matcher          *matcher.MemberMatcher
	membersData      member.DataProvider
	service          youtube.Service
	scheduler        youtube.Scheduler
	youTubeStatsRepo stats.StatsCommandRepository
}

type supportDependencies struct {
	activity   *activity.Logger
	settings   settings.ReadWriter
	acl        *acl.Service
	workerPool *workerpool.Pool
}

type featureDependencies struct {
	majorEventRepo  command.MajorEventRepository
	memberNews      command.MemberNewsService
	commandBuilders []CommandBuilder
}

func (d *Dependencies) coreDeps() coreDependencies {
	if d == nil {
		return coreDependencies{}
	}

	return coreDependencies{
		botSelfUser:  d.BotSelfUser,
		irisBaseURL:  d.IrisBaseURL,
		notification: d.Notification,
		logger:       d.Logger,
	}
}

func (d *Dependencies) messagingDeps() messagingDependencies {
	if d == nil {
		return messagingDependencies{}
	}

	return messagingDependencies{
		client:         d.Client,
		messageAdapter: d.MessageAdapter,
		formatter:      d.Formatter,
	}
}

func (d *Dependencies) dataDeps() dataDependencies {
	if d == nil {
		return dataDependencies{}
	}

	return dataDependencies{
		cache:       d.Cache,
		postgres:    d.Postgres,
		memberRepo:  d.MemberRepo,
		memberCache: d.MemberCache,
	}
}

func (d *Dependencies) streamDeps() streamDependencies {
	if d == nil {
		return streamDependencies{}
	}

	return streamDependencies{
		holodex:          d.Holodex,
		chzzk:            d.Chzzk,
		twitch:           d.Twitch,
		profiles:         d.Profiles,
		alarm:            d.Alarm,
		matcher:          d.Matcher,
		membersData:      d.MembersData,
		service:          d.Service,
		scheduler:        d.Scheduler,
		youTubeStatsRepo: d.YouTubeStatsRepo,
	}
}

func (d *Dependencies) supportDeps() supportDependencies {
	if d == nil {
		return supportDependencies{}
	}

	return supportDependencies{
		activity:   d.Activity,
		settings:   d.Settings,
		acl:        d.ACL,
		workerPool: d.WorkerPool,
	}
}

func (d *Dependencies) featureDeps() featureDependencies {
	if d == nil {
		return featureDependencies{}
	}

	return featureDependencies{
		majorEventRepo:  d.MajorEventRepo,
		memberNews:      d.MemberNews,
		commandBuilders: cloneCommandBuilders(d.CommandBuilders),
	}
}
