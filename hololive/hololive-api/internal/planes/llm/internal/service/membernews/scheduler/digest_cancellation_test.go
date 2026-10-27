package scheduler

import (
	"context"
	"testing"
	"testing/synctest"

	"github.com/kapu/hololive-api/internal/planes/llm/membernews/model"
)

func TestCanceledDigestDispatchJoinsAndCountsUnstartedRooms(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		ctx, cancel := context.WithCancel(t.Context())
		defer cancel()

		rooms := []model.SubscribedRoom{
			{RoomID: "accepted"},
			{RoomID: "room-2"},
			{RoomID: "room-3"},
			{RoomID: "room-4"},
			{RoomID: "room-5"},
			{RoomID: "unstarted-1"},
			{RoomID: "unstarted-2"},
		}
		entered := make(chan struct{}, maxConcurrentDigests)
		release := make(chan struct{})
		completed := make(chan digestEnqueueResult, 1)

		go func() {
			completed <- dispatchDigestRooms(ctx, rooms, &digestDispatchConfig{
				processRoom: func(ctx context.Context, _, roomID string) digestEnqueueResult {
					entered <- struct{}{}

					<-ctx.Done()
					<-release

					if roomID == "accepted" {
						return digestEnqueueResult{Attempted: 1, Queued: 1}
					}

					return failedRoomResult(roomID)
				},
			})
		}()

		for range maxConcurrentDigests {
			<-entered
		}

		cancel()
		synctest.Wait()

		select {
		case <-completed:
			t.Fatal("dispatch returned before canceled room tasks joined")
		default:
		}

		close(release)

		result := <-completed
		if result.Attempted != len(rooms) || result.Queued != 1 || result.Failed != 6 || len(result.FailedRooms) != 6 {
			t.Fatalf("canceled dispatch lost queued or unstarted rooms: %+v", result)
		}
	})
}
