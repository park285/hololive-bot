package domain_test

import (
	"testing"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestNewAlarmNotification_UsesExplicitLiveDispatchRoute(t *testing.T) {
	t.Parallel()

	notification := domain.NewAlarmNotification("room-live", nil, nil, 5, nil, "")
	if notification.AlarmType != domain.AlarmTypeLive {
		t.Fatalf("AlarmType = %q, want %q", notification.AlarmType, domain.AlarmTypeLive)
	}

	if err := notification.ValidateLiveDispatchRoute(); err != nil {
		t.Fatalf("ValidateLiveDispatchRoute() error = %v", err)
	}
}

func TestAlarmNotificationStartingPhase(t *testing.T) {
	t.Parallel()

	startedAt := time.Date(2026, time.August, 17, 0, 31, 45, 0, time.UTC)
	tests := map[string]struct {
		notification *domain.AlarmNotification
		wantStarting bool
		wantCatchup  bool
	}{
		"nil": {},
		"zero minute starts without stream state": {
			notification: &domain.AlarmNotification{MinutesUntil: 0},
			wantStarting: true,
		},
		"positive minute upcoming stays prelive": {
			notification: &domain.AlarmNotification{
				MinutesUntil: 5,
				Stream:       &domain.Stream{Status: domain.StreamStatusUpcoming},
			},
		},
		"positive minute live is catchup": {
			notification: &domain.AlarmNotification{
				MinutesUntil: 5,
				Stream:       &domain.Stream{Status: domain.StreamStatusLive},
			},
			wantStarting: true,
			wantCatchup:  true,
		},
		"positive minute actual start is catchup": {
			notification: &domain.AlarmNotification{
				MinutesUntil: 5,
				Stream:       &domain.Stream{Status: domain.StreamStatusUpcoming, StartActual: &startedAt},
			},
			wantStarting: true,
			wantCatchup:  true,
		},
	}

	for name, tc := range tests {
		t.Run(name, func(t *testing.T) {
			t.Parallel()

			if got := tc.notification.IsStarting(); got != tc.wantStarting {
				t.Fatalf("IsStarting() = %t, want %t", got, tc.wantStarting)
			}

			if got := tc.notification.IsLiveCatchup(); got != tc.wantCatchup {
				t.Fatalf("IsLiveCatchup() = %t, want %t", got, tc.wantCatchup)
			}
		})
	}
}

func TestAlarmTypesValueStaysString(t *testing.T) {
	t.Parallel()

	for name, types := range map[string]domain.AlarmTypes{
		"empty": nil,
		"multi": {domain.AlarmTypeLive, domain.AlarmTypeCommunity},
	} {
		value, err := types.Value()
		if err != nil {
			t.Fatalf("%s: Value() error = %v", name, err)
		}

		if _, ok := value.(string); !ok {
			t.Fatalf("%s: Value() = %T, want string — exec 모드에서 []byte는 bytea(\\x hex)로 인코딩되어 alarm_type[] 파싱이 깨진다", name, value)
		}
	}
}
