package activity

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"testing"

	sharedlogging "github.com/park285/shared-go/v2/pkg/logging"

	"github.com/kapu/hololive-shared/pkg/privacylog"
)

func TestActivityLoggerWritesStructuredRecord(t *testing.T) {
	t.Parallel()

	var buf bytes.Buffer

	l := NewActivityLogger(slog.New(slog.NewJSONHandler(&buf, nil)))
	l.Log("settings_update", "Settings updated", []slog.Attr{slog.Int("alarm_advance_minutes", 5)})

	var record struct {
		Msg     string         `json:"msg"`
		Type    string         `json:"type"`
		Summary string         `json:"summary"`
		Details map[string]any `json:"details"`
	}

	if err := jsonv2.Unmarshal(buf.Bytes(), &record); err != nil {
		t.Fatalf("decode activity record: %v (%s)", err, buf.String())
	}

	if record.Msg != "activity" || record.Type != "settings_update" || record.Summary != "Settings updated" || record.Details["alarm_advance_minutes"] != float64(5) {
		t.Fatalf("activity record = %+v", record)
	}
}

func TestActivityLoggerPreservesMaskingAtFileSink(t *testing.T) {
	t.Parallel()

	dir := t.TempDir()

	logger, closer, err := sharedlogging.EnableFileLoggingWithOptions(sharedlogging.Config{
		Dir: dir, MaxSizeMB: 1, MaxBackups: 1, MaxAgeDays: 1,
	}, "activity.log", sharedlogging.Options{AsyncStdout: true, PrivacyKeys: privacylog.LoggerPrivacyKeys()})
	if err != nil {
		t.Fatal(err)
	}

	t.Cleanup(func() {
		if closeErr := closer.Close(); closeErr != nil {
			t.Error(closeErr)
		}
	})

	NewActivityLogger(logger.With(slog.String("request_id", "activity-projection-request"))).Log(
		"settings_update", "Settings updated", []slog.Attr{
			slog.Int("alarm_advance_minutes", 5),
			slog.String("room_name", "synthetic-room-sentinel"),
			slog.String("password", "synthetic-password-sentinel"),
			slog.Any("unapproved", struct{ Value string }{Value: "synthetic-aggregate-sentinel"}),
		})

	data, err := os.ReadFile(filepath.Join(dir, "activity.log")) //nolint:gosec // 테스트가 소유한 임시 디렉터리의 고정 파일만 읽는다.
	if err != nil {
		t.Fatal(err)
	}

	for _, sentinel := range []string{"synthetic-room-sentinel", "synthetic-password-sentinel", "synthetic-aggregate-sentinel"} {
		if strings.Contains(string(data), sentinel) {
			t.Fatal("활동 로그에 보호 대상 값이 기록됨")
		}
	}

	if !strings.Contains(string(data), `"request_id":"activity-projection-request"`) ||
		!strings.Contains(string(data), `"alarm_advance_minutes":5`) {
		t.Fatal("활동 로그의 상관관계 또는 최소 투영 필드가 누락됨")
	}
}
