package notificationdelivery

import (
	"context"
	"fmt"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type deliveryLeaseTestRepository struct {
	mockDeliveryRepository

	mu      sync.Mutex
	pending []domain.NotificationDeliveryOutbox
	expires map[int64]time.Time
}

func (r *deliveryLeaseTestRepository) fetchReadyAndLock(_ context.Context, _ string, limit int, lease time.Duration, activeRooms []string, processedIDs []int64) ([]domain.NotificationDeliveryOutbox, error) {
	r.mu.Lock()
	defer r.mu.Unlock()

	claimed := make([]domain.NotificationDeliveryOutbox, 0, limit)
	remaining := make([]domain.NotificationDeliveryOutbox, 0, len(r.pending))

	for i := range r.pending {
		item := &r.pending[i]

		if len(claimed) == limit || slices.Contains(activeRooms, item.RoomID) || slices.Contains(processedIDs, item.ID) {
			remaining = append(remaining, *item)
			continue
		}

		activeRooms = append(activeRooms, item.RoomID)
		claimed = append(claimed, *item)
		r.expires[item.ID] = time.Now().Add(lease)
	}

	r.pending = remaining

	return claimed, nil
}

func TestDispatcherClaimsOnlyWhenDeliveryCanStart(t *testing.T) {
	for _, tc := range []struct {
		name         string
		count, rooms int
		delay        time.Duration
	}{
		{"same_room", 10, 1, 9 * time.Second},
		{"different_rooms", 50, 50, 9 * time.Second},
		{"near_attempt_budget", 8, 1, 53 * time.Second},
	} {
		t.Run(tc.name, func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				repo := &deliveryLeaseTestRepository{expires: make(map[int64]time.Time)}

				for i := range tc.count {
					repo.pending = append(repo.pending, domain.NotificationDeliveryOutbox{ID: int64(i + 1), RoomID: fmt.Sprintf("room-%d", i%tc.rooms), Payload: makePayload(t, "hello")})
				}

				var expired, sent atomic.Int32

				repo.saveRequestFn = func(_ context.Context, id int64, _ string, _, _ *preparedMessage) (bool, error) {
					repo.mu.Lock()

					valid := repo.expires[id].After(time.Now())
					repo.mu.Unlock()

					if !valid {
						expired.Add(1)
					}

					return valid, nil
				}

				sender := &mockSender{sendFn: func(context.Context, string, string) error {
					time.Sleep(tc.delay)
					sent.Add(1)

					return nil
				}}
				cfg := testDispatcherConfig()

				cfg.AttemptTimeout = 54 * time.Second

				d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), &cfg)
				d.processReady(t.Context())
				require.EqualValues(t, tc.count, sent.Load())
				require.Zero(t, expired.Load())
				t.Logf("sent=%d expired_before_send=%d", sent.Load(), expired.Load())
			})
		})
	}
}

func TestDispatcherStopsClaimingAtBatchLimit(t *testing.T) {
	repo := &deliveryLeaseTestRepository{expires: make(map[int64]time.Time)}

	for i := range 10 {
		repo.pending = append(repo.pending, domain.NotificationDeliveryOutbox{ID: int64(i + 1), RoomID: "room", Payload: makePayload(t, "hello")})
	}

	cfg := testDispatcherConfig()

	cfg.BatchSize = 3

	var sent atomic.Int32

	sender := &mockSender{sendFn: func(context.Context, string, string) error {
		sent.Add(1)

		return nil
	}}
	d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), &cfg)
	d.processReady(t.Context())
	require.EqualValues(t, 3, sent.Load())
	require.Len(t, repo.pending, 7)
}
