package bootstrap

import (
	"errors"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/kapu/hololive-api/internal/apifoundation"
	membermocks "github.com/kapu/hololive-api/internal/service/member/mocks"
	"github.com/kapu/hololive-shared/pkg/config/settings"
)

type botOwnerCloser func() error

func (f botOwnerCloser) Close() error { return f() }

func TestBotInfrastructureOwnerJoinsMemberCacheBeforeClosingClientsAndDatabase(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		entered := make(chan struct{})
		release := make(chan struct{})
		finish := sync.OnceFunc(func() { close(release) })

		defer finish()

		var resourceCloses atomic.Int64

		var order []string

		owner := &botInfrastructureOwner{
			stopHolodex: func() { order = append(order, "holodex") },
			stopMemberCache: func() {
				order = append(order, "member-cache")

				close(entered)
				<-release
			},
			internalClients: []io.Closer{botOwnerCloser(func() error {
				resourceCloses.Add(1)

				order = append(order, "internal-client")

				return nil
			})},
			irisClient: botOwnerCloser(func() error {
				order = append(order, "iris")

				return nil
			}),
			infraCleanup: func() { order = append(order, "database-cache") },
		}
		closed := make(chan error, 1)

		go func() { closed <- owner.Close() }()

		<-entered

		if got := resourceCloses.Load(); got != 0 {
			t.Fatalf("client closed while member cache is alive=%d want=0", got)
		}

		finish()

		if err := <-closed; err != nil {
			t.Fatal(err)
		}

		if want := []string{"holodex", "member-cache", "internal-client", "iris", "database-cache"}; !slices.Equal(order, want) {
			t.Fatalf("cleanup order=%v want=%v", order, want)
		}
	})
}

func TestBotInfrastructureOwnerPartialBuildClosesAcquiredResourcesAndPreservesErrors(t *testing.T) {
	clientErr := errors.New("client close failed")
	irisErr := errors.New("Iris close failed")

	var internalCloses, irisCloses, infraCloses int

	owner := &botInfrastructureOwner{
		internalClients: []io.Closer{botOwnerCloser(func() error { internalCloses++; return clientErr })},
		irisClient:      botOwnerCloser(func() error { irisCloses++; return irisErr }),
		infraCleanup:    func() { infraCloses++ },
	}

	for range 2 {
		err := owner.Close()
		if !errors.Is(err, clientErr) || !errors.Is(err, irisErr) {
			t.Fatalf("Close error=%v want both client and Iris causes", err)
		}
	}

	if internalCloses != 1 || irisCloses != 1 || infraCloses != 1 {
		t.Fatalf("cleanup counts client/Iris/infra=%d/%d/%d want=1/1/1", internalCloses, irisCloses, infraCloses)
	}
}

func TestInitAlarmYouTubeStackStillRejectsMalformedSettingsFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "settings.json")
	if err := os.WriteFile(path, []byte(`{"alarmAdvanceMinutes":`), 0o600); err != nil {
		t.Fatal(err)
	}

	stack, err := InitAlarmYouTubeStack(&settings.Config{
		AlarmServiceURL: "http://127.0.0.1:8081", SettingsFilePath: path,
		Notification: settings.NotificationConfig{AdvanceMinutes: []int{10, 5, 1}},
	}, &apifoundation.ScraperHolodexFoundation{MemberServiceAdapter: &membermocks.DataProvider{}}, slog.New(slog.DiscardHandler))
	if err == nil || stack != nil {
		t.Fatalf("malformed settings startup=(%+v, %v) want=(nil, error)", stack, err)
	}
}
