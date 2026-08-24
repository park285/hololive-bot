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
	"testing"
	"time"

	messagingadapter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging"
	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/bot/orchestration/orchcmd"
	command "github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

func TestDependenciesViews_NilSafety(t *testing.T) {
	var deps *Dependencies

	if got := deps.coreDeps(); got.logger != nil || got.botSelfUser != "" || got.irisBaseURL != "" {
		t.Fatal("coreDeps nil-safety failed")
	}

	if got := deps.messagingDeps(); got.client != nil || got.messageAdapter != nil || got.formatter != nil || got.markdownReplies {
		t.Fatal("messagingDeps nil-safety failed")
	}

	if got := deps.dataDeps(); got.cache != nil || got.postgres != nil || got.memberRepository != nil || got.memberCache != nil {
		t.Fatal("dataDeps nil-safety failed")
	}

	if got := deps.streamDeps(); got.holodex != nil {
		t.Fatal("streamDeps nil-safety failed")
	}

	if got := deps.supportDeps(); got.acl != nil {
		t.Fatal("supportDeps nil-safety failed")
	}

	if got := deps.featureDeps(); len(got.commandBuilders) != 0 || got.majorEventRepository != nil || got.memberNews != nil {
		t.Fatal("featureDeps nil-safety failed")
	}
}

type fieldMappingFixture struct {
	logger           *slog.Logger
	messageAdapter   *messagingadapter.MessageAdapter
	formatter        *messageformatter.ResponseFormatter
	cacheService     *cache.Service
	postgresService  *database.PostgresService
	memberRepository *member.Repository
	memberCache      *member.Cache
	deps             *Dependencies
}

func newFieldMappingFixture() fieldMappingFixture {
	f := fieldMappingFixture{
		logger:           slog.New(slog.DiscardHandler),
		messageAdapter:   &messagingadapter.MessageAdapter{},
		formatter:        &messageformatter.ResponseFormatter{},
		cacheService:     &cache.Service{},
		postgresService:  &database.PostgresService{},
		memberRepository: &member.Repository{},
		memberCache:      &member.Cache{},
	}
	externalBuilder := orchcmd.CommandBuilder(func(_ *handlercore.Dependencies) handlercore.Command {
		return command.NewHelpCommand(nil)
	})

	f.deps = &Dependencies{
		BotSelfUser:           "bot-self",
		IrisBaseURL:           "https://iris.internal",
		Notification:          settings.NotificationConfig{},
		CalendarImageCacheDir: "data/test-calendar-cache",
		CalendarEntryCacheTTL: time.Hour,
		Logger:                f.logger,
		Client:                &fakeIrisClient{},
		MessageAdapter:        f.messageAdapter,
		Formatter:             f.formatter,
		MarkdownReplies:       true,
		Cache:                 f.cacheService,
		Postgres:              f.postgresService,
		MemberRepository:      f.memberRepository,
		MemberCache:           f.memberCache,
		CommandBuilders:       []orchcmd.CommandBuilder{externalBuilder},
	}

	return f
}

func assertCoreDepsMapping(t *testing.T, f fieldMappingFixture) {
	t.Helper()

	core := f.deps.coreDeps()
	if core.botSelfUser != "bot-self" ||
		core.irisBaseURL != "https://iris.internal" ||
		core.calendarImageCacheDir != "data/test-calendar-cache" ||
		core.calendarEntryCacheTTL != time.Hour ||
		core.logger != f.logger {
		t.Fatal("coreDeps mapping mismatch")
	}
}

func assertMessagingDepsMapping(t *testing.T, f fieldMappingFixture) {
	t.Helper()

	messaging := f.deps.messagingDeps()
	if messaging.client != f.deps.Client || messaging.messageAdapter != f.messageAdapter || messaging.formatter != f.formatter {
		t.Fatal("messagingDeps mapping mismatch")
	}

	if !messaging.markdownReplies {
		t.Fatal("messagingDeps markdownReplies mapping mismatch")
	}
}

func assertDataDepsMapping(t *testing.T, f fieldMappingFixture) {
	t.Helper()

	data := f.deps.dataDeps()
	if data.cache != f.cacheService || data.postgres != f.postgresService ||
		data.memberRepository != f.memberRepository || data.memberCache != f.memberCache {
		t.Fatal("dataDeps mapping mismatch")
	}
}

func assertFeatureDepsMapping(t *testing.T, f fieldMappingFixture) {
	t.Helper()

	feature := f.deps.featureDeps()
	if len(feature.commandBuilders) != 1 || feature.commandBuilders[0] == nil {
		t.Fatal("featureDeps commandBuilders mapping mismatch")
	}

	f.deps.CommandBuilders[0] = nil

	if feature.commandBuilders[0] == nil {
		t.Fatal("featureDeps commandBuilders must be copied defensively")
	}
}

func TestDependenciesViews_FieldMapping(t *testing.T) {
	f := newFieldMappingFixture()

	assertCoreDepsMapping(t, f)
	assertMessagingDepsMapping(t, f)
	assertDataDepsMapping(t, f)

	if stream := f.deps.streamDeps(); stream.service != nil {
		t.Fatal("streamDeps service mapping mismatch")
	}

	assertFeatureDepsMapping(t, f)
}
