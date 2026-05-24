package notifier

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/service/alarm/queue"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

func TestNewNotifierValidation(t *testing.T) {
	t.Parallel()

	cache := newCheckerTestCacheClient(t)
	dedupService := dedup.NewService(cache, []int{5, 3, 1}, newCheckerTestLogger())
	queuePublisher := queue.NewPublisher(cache, newCheckerTestLogger())

	_, err := NewNotifier(nil, queuePublisher, nil, newCheckerTestLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "dedup service is nil")

	_, err = NewNotifier(dedupService, nil, nil, newCheckerTestLogger())
	require.Error(t, err)
	assert.Contains(t, err.Error(), "queue publisher is nil")
}

func TestNotifierReleaseClaimsBestEffort(t *testing.T) {
	t.Parallel()

	notifier := &Notifier{
		dedupService: dedup.NewService(&cachemocks.Client{
			DelManyFunc: func(context.Context, []string) (int64, error) {
				return 0, errors.New("delmany failed")
			},
		}, []int{5, 3, 1}, newCheckerTestLogger()),
		logger: newCheckerTestLogger(),
	}

	notifier.releaseClaimsBestEffort(t.Context(), []string{"claim-1"}, "release failed")
	notifier.releaseClaimsBestEffort(t.Context(), nil, "release failed")
}
