package scheduler

import (
	"errors"
	"testing"
	"time"

	"github.com/park285/shared-go/v2/pkg/outputguard"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
	"github.com/kapu/hololive-shared/pkg/timeutil"
)

func TestSchedulersPrepareOneRunAndUseCapturedClock(t *testing.T) {
	for _, period := range []model.Period{model.PeriodWeekly, model.PeriodMonthly} {
		t.Run(string(period), func(t *testing.T) {
			prepared := &mockDigestService{}
			service := &mockDigestService{
				rooms: []model.SubscribedRoom{{RoomID: "room-a"}, {RoomID: "room-b"}}, generator: prepared,
				digestErrs: map[string]error{"room-a": errors.New("unprepared service must not generate"), "room-b": errors.New("unprepared service must not generate")},
			}
			locker := &mockNotificationLocker{acquireToken: testLockHandle, acquireAcquired: true}
			outbox := newMockOutboxRepository()
			now := time.Date(2026, time.September, 30, 23, 59, 59, 0, timeutil.KSTZone)
			calls := 0
			clock := func() time.Time { calls++; return now.Add(time.Duration(calls-1) * 2 * time.Second) }

			var err error

			if period == model.PeriodWeekly {
				scheduler := NewScheduler(service, mockFormatter{}, locker, outbox, nil, WithOutputGuard(outputguard.NewGuard()))
				scheduler.SetClock(clock)

				err = scheduler.SendWeeklyDigest(t.Context())
			} else {
				scheduler := NewMonthlyScheduler(service, mockFormatter{}, locker, outbox, nil, WithMonthlyOutputGuard(outputguard.NewGuard()))
				scheduler.SetClock(clock)

				err = scheduler.SendMonthlyDigest(t.Context())
			}

			if err != nil {
				t.Fatal(err)
			}

			if service.prepareCalls != 1 || calls != 1 || !service.runNow.Equal(now) || service.runPeriod != period {
				t.Fatalf("prepares=%d clock calls=%d run now=%v period=%v", service.prepareCalls, calls, service.runNow, service.runPeriod)
			}

			if len(outbox.enqueuedItems) != 2 || len(locker.releaseCalls) != 1 {
				t.Fatalf("enqueued=%d releases=%d", len(outbox.enqueuedItems), len(locker.releaseCalls))
			}
		})
	}
}

func TestSchedulerCommonPreparationFailureStopsRoomsAndReleasesLock(t *testing.T) {
	cause := errors.New("common candidate DB query failed")
	service := &mockDigestService{rooms: []model.SubscribedRoom{{RoomID: testRoomID}}, prepareErr: cause}
	locker := &mockNotificationLocker{acquireToken: testLockHandle, acquireAcquired: true}
	outbox := newMockOutboxRepository()
	scheduler := NewScheduler(service, mockFormatter{}, locker, outbox, nil, WithOutputGuard(outputguard.NewGuard()))
	err := scheduler.SendWeeklyDigest(t.Context())

	if !errors.Is(err, cause) || service.prepareCalls != 1 || len(outbox.enqueuedItems) != 0 || len(locker.releaseCalls) != 1 {
		t.Fatalf("error=%v prepares=%d enqueues=%d releases=%d", err, service.prepareCalls, len(outbox.enqueuedItems), len(locker.releaseCalls))
	}
}

func TestSchedulerNoRoomsOrLockSkipDoesNotPrepareRun(t *testing.T) {
	for _, acquired := range []bool{false, true} {
		service := &mockDigestService{}
		locker := &mockNotificationLocker{acquireToken: testLockHandle, acquireAcquired: acquired}
		scheduler := NewScheduler(service, mockFormatter{}, locker, newMockOutboxRepository(), nil)

		if err := scheduler.SendWeeklyDigest(t.Context()); err != nil {
			t.Fatal(err)
		}

		if service.prepareCalls != 0 {
			t.Fatalf("acquired=%t prepares=%d", acquired, service.prepareCalls)
		}
	}
}
