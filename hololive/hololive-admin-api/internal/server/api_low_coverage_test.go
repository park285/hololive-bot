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

package server

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"

	"github.com/gin-gonic/gin"
	triggercontracts "github.com/kapu/hololive-shared/pkg/contracts/trigger"
	"github.com/kapu/hololive-shared/pkg/domain"

	"github.com/kapu/hololive-shared/pkg/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/activity"
)

type stubAlarmCRUDForServer struct {
	getAllAlarmKeys func(context.Context) ([]*domain.AlarmEntry, error)
	removeAlarm     func(context.Context, string, string, domain.AlarmTypes) (bool, error)
}

func (s *stubAlarmCRUDForServer) AddAlarm(context.Context, domain.AddAlarmRequest) (bool, error) {
	return false, nil
}

func (s *stubAlarmCRUDForServer) RemoveAlarm(
	ctx context.Context,
	roomID, channelID string,
	alarmTypes domain.AlarmTypes,
) (bool, error) {
	if s.removeAlarm == nil {
		return false, nil
	}

	return s.removeAlarm(ctx, roomID, channelID, alarmTypes)
}

func (s *stubAlarmCRUDForServer) GetRoomAlarms(context.Context, string) ([]string, error) {
	return nil, nil
}

func (s *stubAlarmCRUDForServer) GetRoomAlarmsWithTypes(context.Context, string) ([]*domain.Alarm, error) {
	return nil, nil
}

func (s *stubAlarmCRUDForServer) ListRoomAlarmsView(context.Context, string) ([]domain.AlarmListView, error) {
	return nil, nil
}

func (s *stubAlarmCRUDForServer) ClearRoomAlarms(context.Context, string) (int, error) {
	return 0, nil
}

func (s *stubAlarmCRUDForServer) GetNextStreamInfo(context.Context, string) (*domain.NextStreamInfo, error) {
	return nil, nil
}

func (s *stubAlarmCRUDForServer) UpdateAlarmAdvanceMinutes(context.Context, int) []int {
	return nil
}

func (s *stubAlarmCRUDForServer) GetTargetMinutes() []int {
	return nil
}

func (s *stubAlarmCRUDForServer) SetRoomName(context.Context, string, string) error {
	return nil
}

func (s *stubAlarmCRUDForServer) SetUserName(context.Context, string, string) error {
	return nil
}

func (s *stubAlarmCRUDForServer) GetAllAlarmKeys(ctx context.Context) ([]*domain.AlarmEntry, error) {
	if s.getAllAlarmKeys == nil {
		return nil, nil
	}

	return s.getAllAlarmKeys(ctx)
}

func (s *stubAlarmCRUDForServer) WarmCacheFromDB(context.Context) error {
	return nil
}

func newAPITestContext(method, urlPath string, body []byte) (*gin.Context, *httptest.ResponseRecorder) {
	rec := httptest.NewRecorder()
	ctx, _ := gin.CreateTestContext(rec)

	req := httptest.NewRequestWithContext(context.Background(), method, urlPath, bytes.NewReader(body))
	if len(body) > 0 {
		req.Header.Set("Content-Type", "application/json")
	}

	ctx.Request = req

	return ctx, rec
}

func newDiscardLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func newActivityLoggerForTest(t *testing.T) *activity.Logger {
	t.Helper()
	return activity.NewActivityLogger(filepath.Join(t.TempDir(), "activity.log"), newDiscardLogger())
}

func TestAlarmAPIHandler_GetAlarmsAndDeleteAlarm(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("get alarms success", func(t *testing.T) {
		handler := &AlarmAPIHandler{APIHandler: &APIHandler{
			alarm: &stubAlarmCRUDForServer{
				getAllAlarmKeys: func(context.Context) ([]*domain.AlarmEntry, error) {
					return []*domain.AlarmEntry{{RoomID: "r1", ChannelID: "c1"}}, nil
				},
			},
			logger: newDiscardLogger(),
		}}

		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/alarms", nil)
		handler.GetAlarms(ctx)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}

		body := rec.Body.String()
		if !strings.Contains(body, `"status":"ok"`) || !strings.Contains(body, `"roomId":"r1"`) {
			t.Fatalf("unexpected body: %s", body)
		}
	})

	t.Run("get alarms error", func(t *testing.T) {
		handler := &AlarmAPIHandler{APIHandler: &APIHandler{
			alarm: &stubAlarmCRUDForServer{
				getAllAlarmKeys: func(context.Context) ([]*domain.AlarmEntry, error) {
					return nil, errors.New("boom")
				},
			},
			logger: newDiscardLogger(),
		}}

		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/alarms", nil)
		handler.GetAlarms(ctx)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
		}
	})

	t.Run("delete alarm bad json", func(t *testing.T) {
		handler := &AlarmAPIHandler{APIHandler: &APIHandler{
			alarm:    &stubAlarmCRUDForServer{},
			logger:   newDiscardLogger(),
			activity: newActivityLoggerForTest(t),
		}}

		ctx, rec := newAPITestContext(http.MethodDelete, "/api/holo/alarm", []byte("{"))
		handler.DeleteAlarm(ctx)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})

	t.Run("delete alarm internal error", func(t *testing.T) {
		handler := &AlarmAPIHandler{APIHandler: &APIHandler{
			alarm: &stubAlarmCRUDForServer{
				removeAlarm: func(context.Context, string, string, domain.AlarmTypes) (bool, error) {
					return false, errors.New("remove failed")
				},
			},
			logger:   newDiscardLogger(),
			activity: newActivityLoggerForTest(t),
		}}

		ctx, rec := newAPITestContext(http.MethodDelete, "/api/holo/alarm", []byte(`{"roomId":"room-1","channelId":"ch-1"}`))
		handler.DeleteAlarm(ctx)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
		}
	})

	t.Run("delete alarm success", func(t *testing.T) {
		var gotRoomID, gotChannelID string

		handler := &AlarmAPIHandler{APIHandler: &APIHandler{
			alarm: &stubAlarmCRUDForServer{
				removeAlarm: func(_ context.Context, roomID, channelID string, _ domain.AlarmTypes) (bool, error) {
					gotRoomID, gotChannelID = roomID, channelID
					return true, nil
				},
			},
			logger:   newDiscardLogger(),
			activity: newActivityLoggerForTest(t),
		}}

		ctx, rec := newAPITestContext(http.MethodDelete, "/api/holo/alarm", []byte(`{"roomId":"room-1","channelId":"ch-1"}`))
		handler.DeleteAlarm(ctx)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}

		if gotRoomID != "room-1" || gotChannelID != "ch-1" {
			t.Fatalf("remove args mismatch got room=%q channel=%q", gotRoomID, gotChannelID)
		}

		if !strings.Contains(rec.Body.String(), `"removed":true`) {
			t.Fatalf("unexpected body: %s", rec.Body.String())
		}
	})
}

func TestMajorEventAPIHandler_TriggerEndpoints(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("weekly scheduler not initialized", func(t *testing.T) {
		handler := &MajorEventAPIHandler{APIHandler: &APIHandler{logger: newDiscardLogger()}}
		ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/trigger/major-event", nil)
		handler.TriggerMajorEventNotification(ctx)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
		}
	})

	t.Run("weekly conflict in progress", func(t *testing.T) {
		handler := &MajorEventAPIHandler{APIHandler: &APIHandler{
			logger:              newDiscardLogger(),
			majorEventScheduler: &stubMajorEventScheduler{err: triggercontracts.ErrNotificationInProgress},
		}}
		ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/trigger/major-event", nil)
		handler.TriggerMajorEventNotification(ctx)

		if rec.Code != http.StatusConflict {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusConflict, rec.Body.String())
		}
	})

	t.Run("weekly internal error", func(t *testing.T) {
		handler := &MajorEventAPIHandler{APIHandler: &APIHandler{
			logger:              newDiscardLogger(),
			majorEventScheduler: &stubMajorEventScheduler{err: errors.New("boom")},
		}}
		ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/trigger/major-event", nil)
		handler.TriggerMajorEventNotification(ctx)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
		}
	})

	t.Run("weekly success", func(t *testing.T) {
		handler := &MajorEventAPIHandler{APIHandler: &APIHandler{
			logger:              newDiscardLogger(),
			majorEventScheduler: &stubMajorEventScheduler{},
		}}
		ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/trigger/major-event", nil)
		handler.TriggerMajorEventNotification(ctx)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
	})

	t.Run("monthly scheduler not initialized", func(t *testing.T) {
		handler := &MajorEventAPIHandler{APIHandler: &APIHandler{logger: newDiscardLogger()}}
		ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/trigger/major-event-monthly", nil)
		handler.TriggerMajorEventMonthlyNotification(ctx)

		if rec.Code != http.StatusServiceUnavailable {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusServiceUnavailable, rec.Body.String())
		}
	})

	t.Run("monthly conflict in progress", func(t *testing.T) {
		handler := &MajorEventAPIHandler{APIHandler: &APIHandler{
			logger:                     newDiscardLogger(),
			majorEventMonthlyScheduler: &stubMajorEventMonthlyScheduler{err: triggercontracts.ErrNotificationInProgress},
		}}
		ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/trigger/major-event-monthly", nil)
		handler.TriggerMajorEventMonthlyNotification(ctx)

		if rec.Code != http.StatusConflict {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusConflict, rec.Body.String())
		}
	})

	t.Run("monthly success", func(t *testing.T) {
		handler := &MajorEventAPIHandler{APIHandler: &APIHandler{
			logger:                     newDiscardLogger(),
			majorEventMonthlyScheduler: &stubMajorEventMonthlyScheduler{},
		}}
		ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/trigger/major-event-monthly", nil)
		handler.TriggerMajorEventMonthlyNotification(ctx)

		if rec.Code != http.StatusOK {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusOK, rec.Body.String())
		}
	})
}

func TestProfileAPIHandler_ValidationAndConverters(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("get profile requires channelId", func(t *testing.T) {
		handler := &ProfileAPIHandler{APIHandler: &APIHandler{logger: newDiscardLogger()}}
		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/profile", nil)
		handler.GetProfile(ctx)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})

	t.Run("get profile service unavailable", func(t *testing.T) {
		handler := &ProfileAPIHandler{APIHandler: &APIHandler{logger: newDiscardLogger()}}
		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/profile?channelId=UC123", nil)
		handler.GetProfile(ctx)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
		}
	})

	t.Run("get profile by name requires name", func(t *testing.T) {
		handler := &ProfileAPIHandler{APIHandler: &APIHandler{logger: newDiscardLogger()}}
		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/profile/by-name", nil)
		handler.GetProfileByName(ctx)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})

	t.Run("get profile by name service unavailable", func(t *testing.T) {
		handler := &ProfileAPIHandler{APIHandler: &APIHandler{logger: newDiscardLogger()}}
		ctx, rec := newAPITestContext(http.MethodGet, "/api/holo/profile/by-name?name=Sora", nil)
		handler.GetProfileByName(ctx)

		if rec.Code != http.StatusInternalServerError {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusInternalServerError, rec.Body.String())
		}
	})

	t.Run("convert profile data", func(t *testing.T) {
		profile := &domain.TalentProfile{
			Slug:         "sora",
			EnglishName:  "Tokino Sora",
			JapaneseName: "ときのそら",
			Catchphrase:  "hello",
			Description:  "idol",
			DataEntries: []domain.TalentProfileEntry{
				{Label: "Unit", Value: "JP"},
			},
			SocialLinks: []domain.TalentSocialLink{
				{Label: "X", URL: "https://x.com/sora"},
			},
			OfficialURL: "https://example.com/sora",
		}

		got := convertToProfileData(profile)
		if got == nil {
			t.Fatal("convertToProfileData returned nil")
		}

		if got.EnglishName != "Tokino Sora" || got.JapaneseName != "ときのそら" {
			t.Fatalf("unexpected profile names: %+v", got)
		}

		if len(got.DataEntries) != 1 || got.DataEntries[0].Label != "Unit" {
			t.Fatalf("unexpected data entries: %+v", got.DataEntries)
		}

		if len(got.SocialLinks) != 1 || got.SocialLinks[0].Label != "X" {
			t.Fatalf("unexpected social links: %+v", got.SocialLinks)
		}

		if convertToProfileData(nil) != nil {
			t.Fatal("convertToProfileData(nil) must return nil")
		}
	})

	t.Run("convert translated rows", func(t *testing.T) {
		rows := []domain.TranslatedProfileDataRow{
			{Label: "생일", Value: "5월 15일"},
			{Label: "소속", Value: "JP"},
		}

		got := convertTranslatedRows(rows)
		if len(got) != 2 {
			t.Fatalf("len(got)=%d want=2", len(got))
		}

		if got[0].Label != "생일" || got[1].Value != "JP" {
			t.Fatalf("unexpected converted rows: %+v", got)
		}
	})
}

func TestRoomAPIHandler_NilAndBadRequestBranches(t *testing.T) {
	gin.SetMode(gin.TestMode)

	t.Run("add room bad json", func(t *testing.T) {
		handler := &RoomAPIHandler{APIHandler: &APIHandler{acl: &acl.Service{}, logger: newDiscardLogger()}}
		ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/rooms", []byte("{"))
		handler.AddRoom(ctx)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})

	t.Run("remove room bad json", func(t *testing.T) {
		handler := &RoomAPIHandler{APIHandler: &APIHandler{acl: &acl.Service{}, logger: newDiscardLogger()}}
		ctx, rec := newAPITestContext(http.MethodDelete, "/api/holo/rooms", []byte("{"))
		handler.RemoveRoom(ctx)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})

	t.Run("set acl bad json", func(t *testing.T) {
		handler := &RoomAPIHandler{APIHandler: &APIHandler{acl: &acl.Service{}, logger: newDiscardLogger()}}
		ctx, rec := newAPITestContext(http.MethodPost, "/api/holo/acl", []byte("{"))
		handler.SetACL(ctx)

		if rec.Code != http.StatusBadRequest {
			t.Fatalf("status=%d want=%d body=%s", rec.Code, http.StatusBadRequest, rec.Body.String())
		}
	})
}
