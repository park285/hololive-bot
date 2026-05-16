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

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/command"
)

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
	if got := deps.streamDeps(); got.holodex != nil || got.youTubeStatsRepo != nil {
		t.Fatal("streamDeps nil-safety failed")
	}
	if got := deps.supportDeps(); got.acl != nil || got.workerPool != nil {
		t.Fatal("supportDeps nil-safety failed")
	}
	if got := deps.featureDeps(); len(got.commandBuilders) != 0 || got.majorEventRepo != nil || got.memberNews != nil {
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
	workerPool := &workerpool.Pool{}
	externalBuilder := CommandBuilder(func(_ *command.Dependencies) command.Command {
		return command.NewHelpCommand(nil)
	})

	deps := &Dependencies{
		BotSelfUser:     "bot-self",
		IrisBaseURL:     "https://iris.internal",
		Notification:    config.NotificationConfig{},
		Logger:          logger,
		Client:          &fakeIrisClient{},
		MessageAdapter:  messageAdapter,
		Formatter:       formatter,
		Cache:           cacheSvc,
		Postgres:        postgresSvc,
		MemberRepo:      memberRepo,
		MemberCache:     memberCache,
		CommandBuilders: []CommandBuilder{externalBuilder},
		WorkerPool:      workerPool,
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
	if stream.service != nil {
		t.Fatal("streamDeps service mapping mismatch")
	}

	support := deps.supportDeps()
	if support.workerPool != workerPool {
		t.Fatal("supportDeps workerPool mapping mismatch")
	}

	feature := deps.featureDeps()
	if len(feature.commandBuilders) != 1 || feature.commandBuilders[0] == nil {
		t.Fatal("featureDeps commandBuilders mapping mismatch")
	}

	deps.CommandBuilders[0] = nil
	if feature.commandBuilders[0] == nil {
		t.Fatal("featureDeps commandBuilders must be copied defensively")
	}
}
