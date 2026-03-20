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
	"context"
	"log/slog"
	"testing"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/youtube"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
)

type stubYouTubeScheduler struct{}

func (s *stubYouTubeScheduler) Start(context.Context) {}
func (s *stubYouTubeScheduler) Stop()                 {}

func TestDependenciesViews_NilSafety(t *testing.T) {
	var deps *Dependencies

	if got := deps.coreDeps(); got.logger != nil || got.botSelfUser != "" || got.irisBaseURL != "" {
		t.Fatal("coreDeps nil-safety failed")
	}

	if got := deps.messagingDeps(); got.client != nil || got.messageAdapter != nil || got.formatter != nil {
		t.Fatal("messagingDeps nil-safety failed")
	}

	if got := deps.dataDeps(); got.cache != nil || got.postgres != nil || got.memberRepo != nil || got.memberCache != nil {
		t.Fatal("dataDeps nil-safety failed")
	}

	if got := deps.streamDeps(); got.holodex != nil || got.scheduler != nil || got.youTubeStatsRepo != nil {
		t.Fatal("streamDeps nil-safety failed")
	}

	if got := deps.supportDeps(); got.acl != nil || got.workerPool != nil {
		t.Fatal("supportDeps nil-safety failed")
	}

	if got := deps.featureDeps(); len(got.commandFactories) != 0 || got.majorEventRepo != nil || got.memberNews != nil {
		t.Fatal("featureDeps nil-safety failed")
	}
}

func TestDependenciesViews_FieldMapping(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	messageAdapter := &adapter.MessageAdapter{}
	formatter := &adapter.ResponseFormatter{}
	cacheSvc := &cache.Service{}
	postgresSvc := &database.PostgresService{}
	memberRepo := &member.Repository{}
	memberCache := &member.Cache{}
	scheduler := &stubYouTubeScheduler{}
	workerPool := &workerpool.Pool{}
	factory := command.Factory(func(deps *command.Dependencies) command.Command { return nil })

	deps := &Dependencies{
		BotSelfUser:      "bot-self",
		IrisBaseURL:      "https://iris.internal",
		Notification:     config.NotificationConfig{},
		Logger:           logger,
		Client:           &fakeIrisClient{},
		MessageAdapter:   messageAdapter,
		Formatter:        formatter,
		Cache:            cacheSvc,
		Postgres:         postgresSvc,
		MemberRepo:       memberRepo,
		MemberCache:      memberCache,
		Scheduler:        scheduler,
		CommandFactories: []command.Factory{factory},
		WorkerPool:       workerPool,
	}

	core := deps.coreDeps()
	if core.botSelfUser != "bot-self" || core.irisBaseURL != "https://iris.internal" || core.logger != logger {
		t.Fatal("coreDeps mapping mismatch")
	}

	messaging := deps.messagingDeps()
	if messaging.client != deps.Client || messaging.messageAdapter != messageAdapter || messaging.formatter != formatter {
		t.Fatal("messagingDeps mapping mismatch")
	}

	data := deps.dataDeps()
	if data.cache != cacheSvc || data.postgres != postgresSvc || data.memberRepo != memberRepo || data.memberCache != memberCache {
		t.Fatal("dataDeps mapping mismatch")
	}

	stream := deps.streamDeps()
	if stream.scheduler != scheduler {
		t.Fatal("streamDeps scheduler mapping mismatch")
	}

	support := deps.supportDeps()
	if support.workerPool != workerPool {
		t.Fatal("supportDeps workerPool mapping mismatch")
	}

	feature := deps.featureDeps()
	if len(feature.commandFactories) != 1 || feature.commandFactories[0] == nil {
		t.Fatal("featureDeps commandFactories mapping mismatch")
	}

	// defensive copy 보장
	deps.CommandFactories[0] = nil

	if feature.commandFactories[0] == nil {
		t.Fatal("featureDeps commandFactories must be copied defensively")
	}
}

var _ youtube.Scheduler = (*stubYouTubeScheduler)(nil)
