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

package handlers

import (
	"context"
	"log/slog"
	"slices"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/kapu/hololive-shared/pkg/domain"
	serviceTemplate "github.com/kapu/hololive-shared/pkg/service/template"
	"gorm.io/gorm"

	"github.com/kapu/hololive-kakao-bot-go/internal/adapter"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/notification"
)

type alarmListViewerStub struct {
	listCalled bool
	entries    []domain.AlarmListView
}

func (s *alarmListViewerStub) AddAlarm(context.Context, domain.AddAlarmRequest) (bool, error) {
	return false, nil
}

func (s *alarmListViewerStub) RemoveAlarm(context.Context, string, string, domain.AlarmTypes) (bool, error) {
	return false, nil
}

func (s *alarmListViewerStub) GetRoomAlarms(context.Context, string) ([]string, error) {
	return nil, nil
}

func (s *alarmListViewerStub) GetRoomAlarmsWithTypes(context.Context, string) ([]*domain.Alarm, error) {
	return nil, nil
}
func (s *alarmListViewerStub) ClearRoomAlarms(context.Context, string) (int, error) { return 0, nil }
func (s *alarmListViewerStub) GetNextStreamInfo(context.Context, string) (*domain.NextStreamInfo, error) {
	return nil, nil
}
func (s *alarmListViewerStub) UpdateAlarmAdvanceMinutes(context.Context, int) []int { return nil }
func (s *alarmListViewerStub) GetTargetMinutes() []int                              { return nil }
func (s *alarmListViewerStub) SetRoomName(context.Context, string, string) error    { return nil }
func (s *alarmListViewerStub) SetUserName(context.Context, string, string) error    { return nil }
func (s *alarmListViewerStub) GetAllAlarmKeys(context.Context) ([]*domain.AlarmEntry, error) {
	return nil, nil
}
func (s *alarmListViewerStub) WarmCacheFromDB(context.Context) error { return nil }
func (s *alarmListViewerStub) ListRoomAlarmsView(_ context.Context, _ string) ([]domain.AlarmListView, error) {
	s.listCalled = true
	return s.entries, nil
}

type memberProviderContextCapture struct {
	contexts []context.Context
}

type testContextKey string

func (c *memberProviderContextCapture) saw(expected context.Context) bool {
	return slices.Contains(c.contexts, expected)
}

type contextAwareMemberProvider struct {
	members    []*domain.Member
	byChannel  map[string]*domain.Member
	byName     map[string]*domain.Member
	ctxCapture *memberProviderContextCapture
}

func newContextAwareMemberProvider(members []*domain.Member) *contextAwareMemberProvider {
	byChannel := make(map[string]*domain.Member, len(members))

	byName := make(map[string]*domain.Member, len(members))
	for _, member := range members {
		if member == nil {
			continue
		}

		if member.ChannelID != "" {
			byChannel[member.ChannelID] = member
		}

		if member.Name != "" {
			byName[member.Name] = member
		}
	}

	return &contextAwareMemberProvider{
		members:    members,
		byChannel:  byChannel,
		byName:     byName,
		ctxCapture: &memberProviderContextCapture{},
	}
}

func (p *contextAwareMemberProvider) FindMemberByChannelID(channelID string) *domain.Member {
	return p.byChannel[channelID]
}

func (p *contextAwareMemberProvider) FindMemberByName(name string) *domain.Member {
	return p.byName[name]
}

func (p *contextAwareMemberProvider) FindMemberByAlias(string) *domain.Member {
	return nil
}

func (p *contextAwareMemberProvider) GetChannelIDs() []string {
	ids := make([]string, 0, len(p.byChannel))
	for id := range p.byChannel {
		ids = append(ids, id)
	}

	return ids
}

func (p *contextAwareMemberProvider) GetAllMembers() []*domain.Member {
	return p.members
}

func (p *contextAwareMemberProvider) WithContext(ctx context.Context) domain.MemberDataProvider {
	p.ctxCapture.contexts = append(p.ctxCapture.contexts, ctx)

	return &contextAwareMemberProvider{
		members:    p.members,
		byChannel:  p.byChannel,
		byName:     p.byName,
		ctxCapture: p.ctxCapture,
	}
}

func (p *contextAwareMemberProvider) FindMembersByName(string) []*domain.Member {
	return nil
}

func (p *contextAwareMemberProvider) FindMembersByAlias(string) []*domain.Member {
	return nil
}

type alarmAddRecorder struct {
	addCtx context.Context
}

func (s *alarmAddRecorder) AddAlarm(ctx context.Context, _ domain.AddAlarmRequest) (bool, error) {
	s.addCtx = ctx
	return true, nil
}

func (s *alarmAddRecorder) RemoveAlarm(context.Context, string, string, domain.AlarmTypes) (bool, error) {
	return false, nil
}

func (s *alarmAddRecorder) GetRoomAlarms(context.Context, string) ([]string, error) {
	return nil, nil
}

func (s *alarmAddRecorder) GetRoomAlarmsWithTypes(context.Context, string) ([]*domain.Alarm, error) {
	return nil, nil
}

func (s *alarmAddRecorder) ListRoomAlarmsView(context.Context, string) ([]domain.AlarmListView, error) {
	return nil, nil
}

func (s *alarmAddRecorder) ClearRoomAlarms(context.Context, string) (int, error) { return 0, nil }

func (s *alarmAddRecorder) GetNextStreamInfo(context.Context, string) (*domain.NextStreamInfo, error) {
	return nil, nil
}

func (s *alarmAddRecorder) UpdateAlarmAdvanceMinutes(context.Context, int) []int { return nil }

func (s *alarmAddRecorder) GetTargetMinutes() []int { return nil }

func (s *alarmAddRecorder) SetRoomName(context.Context, string, string) error { return nil }

func (s *alarmAddRecorder) SetUserName(context.Context, string, string) error { return nil }

func (s *alarmAddRecorder) GetAllAlarmKeys(context.Context) ([]*domain.AlarmEntry, error) {
	return nil, nil
}

func (s *alarmAddRecorder) WarmCacheFromDB(context.Context) error { return nil }

func TestAlarmCommand_InvalidAction(t *testing.T) {
	var sentError string

	deps := &Dependencies{
		Alarm:     &notification.AlarmService{},
		Matcher:   &matcher.Matcher{},
		Formatter: adapter.NewResponseFormatter("!", nil),
		SendMessage: func(ctx context.Context, room, message string) error {
			return nil
		},
		SendError: func(ctx context.Context, room, message string) error {
			sentError = message
			return nil
		},
		Logger: slog.Default(),
	}

	cmd := NewAlarmCommand(deps)
	params := map[string]any{
		"action":      "invalid",
		"sub_command": "설정123",
		"member":      "설정123",
	}

	ctx := &domain.CommandContext{
		Room:     "room-1",
		UserName: "user-1",
	}

	if err := cmd.Execute(t.Context(), ctx, params); err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	expectedMessage := deps.Formatter.InvalidAlarmUsage()
	if sentError != expectedMessage {
		t.Fatalf("expected error message %q, got %q", expectedMessage, sentError)
	}
}

func TestAlarmCommand_ListUsesBatchViewWhenAvailable(t *testing.T) {
	var sentMessage string

	alarm := &alarmListViewerStub{
		entries: []domain.AlarmListView{
			{
				ChannelID:  "ch-1",
				MemberName: "미코",
				AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive},
				NextStream: &domain.NextStreamInfo{
					Status:         domain.NextStreamStatusUpcoming,
					Title:          "테스트 방송",
					VideoID:        "vid1",
					StartScheduled: new(time.Date(2026, time.March, 6, 12, 0, 0, 0, time.UTC)),
				},
			},
		},
	}

	deps := &Dependencies{
		Alarm:     alarm,
		Matcher:   &matcher.Matcher{},
		Formatter: adapter.NewResponseFormatter("!", setupAlarmCommandTestRenderer(t)),
		SendMessage: func(ctx context.Context, room, message string) error {
			sentMessage = message
			return nil
		},
		SendError: func(ctx context.Context, room, message string) error { return nil },
		Logger:    slog.Default(),
	}

	cmd := NewAlarmCommand(deps)

	err := cmd.Execute(t.Context(), &domain.CommandContext{Room: "room-1"}, map[string]any{"action": "list"})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	if !alarm.listCalled {
		t.Fatal("expected ListRoomAlarmsView to be used")
	}

	if sentMessage == "" {
		t.Fatal("expected formatted alarm list message")
	}
}

func TestAlarmCommand_AddPropagatesRequestContextToMatcher(t *testing.T) {
	memberProvider := newContextAwareMemberProvider([]*domain.Member{{
		ChannelID: "ch-aqua",
		Name:      "Aqua",
	}})
	alarm := &alarmAddRecorder{}
	deps := &Dependencies{
		Alarm: alarm,
		//nolint:staticcheck // nil base context is the behavior under test; Execute must supply ctx.
		Matcher:   matcher.NewMatcher(nil, memberProvider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		Formatter: adapter.NewResponseFormatter("!", setupAlarmCommandTestRenderer(t)),
		SendMessage: func(context.Context, string, string) error {
			return nil
		},
		SendError: func(context.Context, string, string) error {
			t.Fatal("unexpected send error")
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	ctx := context.WithValue(t.Context(), testContextKey("request-id"), "alarm-propagation")

	err := NewAlarmCommand(deps).Execute(ctx, &domain.CommandContext{
		Room:     "room-1",
		RoomName: "room-name",
		UserID:   "user-1",
		UserName: "tester",
	}, map[string]any{
		"action": "add",
		"member": "Aqua",
	})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	if !memberProvider.ctxCapture.saw(ctx) {
		t.Fatal("expected matcher provider to receive request context")
	}

	if alarm.addCtx != ctx {
		t.Fatal("expected add alarm to receive original request context")
	}
}

func TestAlarmCommand_AddNoMatchStopsAfterErrorMessage(t *testing.T) {
	memberProvider := newContextAwareMemberProvider(nil)
	alarm := &alarmAddRecorder{}
	sendErrorCalled := false
	deps := &Dependencies{
		Alarm: alarm,
		//nolint:staticcheck // nil base context is the behavior under test; Execute must supply ctx.
		Matcher:   matcher.NewMatcher(nil, memberProvider, nil, nil, nil, slog.New(slog.DiscardHandler)),
		Formatter: adapter.NewResponseFormatter("!", setupAlarmCommandTestRenderer(t)),
		SendMessage: func(context.Context, string, string) error {
			return nil
		},
		SendError: func(context.Context, string, string) error {
			sendErrorCalled = true
			return nil
		},
		Logger: slog.New(slog.DiscardHandler),
	}

	err := NewAlarmCommand(deps).Execute(t.Context(), &domain.CommandContext{
		Room:     "room-1",
		RoomName: "room-name",
		UserID:   "user-1",
		UserName: "tester",
	}, map[string]any{
		"action": "add",
		"member": "NoSuchMember",
	})
	if err != nil {
		t.Fatalf("execute returned error: %v", err)
	}

	if !sendErrorCalled {
		t.Fatal("expected no-match error message")
	}
	if alarm.addCtx != nil {
		t.Fatal("expected AddAlarm not to be called after no-match member resolution")
	}
}

//go:fix inline

func setupAlarmCommandTestRenderer(t *testing.T) *serviceTemplate.Renderer {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := db.AutoMigrate(&domain.NotificationTemplate{}); err != nil {
		t.Fatalf("migrate template table: %v", err)
	}

	if err := db.Create([]domain.NotificationTemplate{
		{
			TemplateKey: domain.TemplateKeyCmdAlarmList,
			Body:        "알람 목록\n{{range .Alarms}}{{.MemberName}}\n{{end}}",
		},
		{
			TemplateKey: domain.TemplateKeyCmdAlarmAdded,
			Body:        "알람 추가\n{{.MemberName}}",
		},
	}).Error; err != nil {
		t.Fatalf("seed alarm list template: %v", err)
	}

	logger := slog.New(slog.DiscardHandler)

	return serviceTemplate.NewRenderer(db, logger)
}
