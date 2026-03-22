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

package dispatch

import (
	"context"
	"log/slog"
	"net"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/alicebob/miniredis/v2"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/iris"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type pipelineTestSender struct {
	rooms    []string
	messages []string
}

func (s *pipelineTestSender) SendMessage(_ context.Context, room, message string, _ ...iris.SendOption) error {
	s.rooms = append(s.rooms, room)
	s.messages = append(s.messages, message)
	return nil
}

func newPipelineTestCache(t *testing.T) cache.Client {
	t.Helper()

	mini := miniredis.RunT(t)
	host, rawPort, err := net.SplitHostPort(mini.Addr())
	if err != nil {
		t.Fatalf("split miniredis addr: %v", err)
	}

	port, err := strconv.Atoi(rawPort)
	if err != nil {
		t.Fatalf("parse miniredis port: %v", err)
	}

	svc, err := cache.NewCacheService(
		context.Background(),
		cache.Config{
			Host:         host,
			Port:         port,
			DisableCache: true,
		},
		slog.New(slog.NewTextHandler(testWriter{t: t}, nil)),
	)
	if err != nil {
		t.Fatalf("new cache service: %v", err)
	}

	t.Cleanup(func() {
		if closeErr := svc.Close(); closeErr != nil {
			t.Fatalf("close cache service: %v", closeErr)
		}
		mini.Close()
	})

	return svc
}

type testWriter struct {
	t *testing.T
}

func (w testWriter) Write(p []byte) (int, error) {
	w.t.Log(strings.TrimSpace(string(p)))
	return len(p), nil
}

func TestAlarmQueueDispatchPipeline_EndToEnd(t *testing.T) {
	t.Parallel()

	cacheSvc := newPipelineTestCache(t)
	logger := slog.New(slog.NewTextHandler(testWriter{t: t}, nil))

	publisher := queue.NewPublisher(cacheSvc, logger)
	consumer := queue.NewConsumer(cacheSvc, logger, queue.WithMaxBatch(10))
	sender := &pipelineTestSender{}

	dispatcher, err := NewDispatcher(
		consumer,
		sender,
		NewSimpleRenderer(),
		10,
		1,
		logger,
	)
	if err != nil {
		t.Fatalf("NewDispatcher() error = %v", err)
	}

	startAt := time.Now().UTC().Add(5 * time.Minute).Truncate(time.Minute)
	link := "https://youtube.com/watch?v=pipeline-e2e-stream"
	notification := domain.NewAlarmNotification(
		"room-e2e",
		&domain.Channel{ID: "channel-e2e", Name: "테스트 멤버"},
		&domain.Stream{
			ID:             "pipeline-e2e-stream",
			Title:          "파이프라인 통합 테스트 방송",
			ChannelID:      "channel-e2e",
			ChannelName:    "테스트 멤버",
			Status:         domain.StreamStatusUpcoming,
			StartScheduled: &startAt,
			Link:           &link,
			Channel:        &domain.Channel{ID: "channel-e2e", Name: "테스트 멤버"},
		},
		5,
		[]string{"user-e2e"},
		"테스트 일정 변경 메시지",
	)

	if err := publisher.Publish(context.Background(), notification, []string{"notified:claim:test"}); err != nil {
		t.Fatalf("Publish() error = %v", err)
	}

	if runErr := dispatcher.RunOnce(context.Background()); runErr != nil {
		t.Fatalf("RunOnce() error = %v", runErr)
	}

	if len(sender.rooms) != 1 {
		t.Fatalf("sent rooms = %d, want 1", len(sender.rooms))
	}
	if sender.rooms[0] != "room-e2e" {
		t.Fatalf("sent room = %q, want room-e2e", sender.rooms[0])
	}
	if len(sender.messages) != 1 {
		t.Fatalf("sent messages = %d, want 1", len(sender.messages))
	}

	message := sender.messages[0]
	for _, want := range []string{
		"테스트 멤버",
		"파이프라인 통합 테스트 방송",
		"5분 전",
		"https://youtube.com/watch?v=pipeline-e2e-stream",
	} {
		if !strings.Contains(message, want) {
			t.Fatalf("message %q does not contain %q", message, want)
		}
	}

	sizeResp := cacheSvc.GetClient().Do(
		context.Background(),
		cacheSvc.B().Llen().Key(queue.AlarmDispatchQueue).Build(),
	)
	size, err := sizeResp.AsInt64()
	if err != nil {
		t.Fatalf("LLEN alarm queue: %v", err)
	}
	if size != 0 {
		t.Fatalf("queue size = %d, want 0", size)
	}
}
