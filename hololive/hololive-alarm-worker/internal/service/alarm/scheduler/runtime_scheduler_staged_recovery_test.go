package scheduler

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

func TestYouTubeIterationPublishesRecoveredCandidatesAndPreservesCheckError(t *testing.T) {
	checkErr := errors.New("provider lookup failed")

	var sent int

	s := &RuntimeScheduler{
		youtubeChecker: &runnerFunc{check: func(context.Context) ([]*domain.AlarmNotification, error) {
			return []*domain.AlarmNotification{{RoomID: "durable-room"}}, checkErr
		}},
		notifier: &senderFunc{send: func(_ context.Context, notifications []*domain.AlarmNotification) (delivery.SendResult, error) {
			sent += len(notifications)
			return delivery.SendResult{}, nil
		}},
		logger: testSchedulerLogger(),
	}
	require.ErrorIs(t, s.runYouTubeIteration(t.Context()), checkErr)
	require.Equal(t, 1, sent)
}
