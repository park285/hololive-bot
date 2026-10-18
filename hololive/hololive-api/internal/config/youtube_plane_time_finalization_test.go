package config

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestTimeFinalizationBacklogConfiguration(t *testing.T) {
	require.Zero(t, DefaultYouTubePlaneConfig().TimeFinalizationBacklogBatchSize)

	for _, size := range []int{-1, 0, 200, 1000, 1001} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			t.Setenv("YOUTUBE_PLANE_TIME_FINALIZATION_BACKLOG_BATCH_SIZE", strconv.Itoa(size))

			loaded, err := loadYouTubePlaneConfig()
			require.NoError(t, err)
			require.Equal(t, size, loaded.TimeFinalizationBacklogBatchSize)

			cfg := DefaultYouTubePlaneConfig()

			cfg.TimeFinalizationBacklogBatchSize = loaded.TimeFinalizationBacklogBatchSize

			if size < 0 || size > 1000 {
				require.ErrorContains(t, cfg.Validate(), "time finalization backlog batch size")
			} else {
				require.NoError(t, cfg.Validate())
			}
		})
	}

	t.Setenv("YOUTUBE_PLANE_TIME_FINALIZATION_BACKLOG_BATCH_SIZE", "invalid")

	_, err := loadYouTubePlaneConfig()
	require.Error(t, err)
}
