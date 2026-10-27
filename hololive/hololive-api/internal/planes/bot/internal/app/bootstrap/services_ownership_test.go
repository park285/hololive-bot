package bootstrap

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"testing"

	"github.com/kapu/hololive-api/internal/apifoundation"
	apiconfig "github.com/kapu/hololive-api/internal/config"
	membermocks "github.com/kapu/hololive-api/internal/service/member/mocks"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

type botOwnerCloser func() error

func (f botOwnerCloser) Close() error { return f() }

func TestBotInfrastructureOwnerPartialBuildClosesAcquiredResourcesAndPreservesErrors(t *testing.T) {
	clientErr := errors.New("client close failed")
	irisErr := errors.New("Iris close failed")

	var internalCloses, irisCloses int

	owner := &botInfrastructureOwner{
		internalClients: []io.Closer{botOwnerCloser(func() error { internalCloses++; return clientErr })},
		irisClient:      botOwnerCloser(func() error { irisCloses++; return irisErr }),
	}

	for range 2 {
		err := owner.Close()
		if !errors.Is(err, clientErr) || !errors.Is(err, irisErr) {
			t.Fatalf("Close error=%v want both client and Iris causes", err)
		}
	}

	if internalCloses != 1 || irisCloses != 1 {
		t.Fatalf("cleanup counts client/Iris=%d/%d want=1/1", internalCloses, irisCloses)
	}
}

func TestInitAlarmYouTubeStackStillRejectsMalformedSettingsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"alarmAdvanceMinutes":`), 0o600); err != nil {
		t.Fatal(err)
	}

	stack, err := InitAlarmYouTubeStack(&apiconfig.BotPlaneConfig{
		AlarmServiceURL: "http://127.0.0.1:8081", SettingsFilePath: path,
		Notification: settings.NotificationConfig{AdvanceMinutes: []int{10, 5, 1}},
	}, &apifoundation.ScraperHolodexFoundation{MemberData: &membermocks.DataProvider{}}, slog.New(slog.DiscardHandler))
	if err == nil || stack != nil {
		t.Fatalf("malformed settings startup=(%+v, %v) want=(nil, error)", stack, err)
	}
}
