package config

import (
	"strconv"
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/config/configtest"
	"github.com/kapu/hololive-shared/pkg/config/settingstest"
)

func TestTimeFinalizationBacklogConfiguration(t *testing.T) {
	settingstest.UseProfileFixture(t, configtest.APIProfileFixture(t))

	profile, profileErr := LoadAPIWorkerProfile()
	if profileErr != nil {
		t.Fatal(profileErr)
	}

	for _, size := range []int{-1, 0, 200, 1000, 1001} {
		t.Run(strconv.Itoa(size), func(t *testing.T) {
			t.Setenv("YOUTUBE_PLANE_TIME_FINALIZATION_BACKLOG_BATCH_SIZE", strconv.Itoa(size))

			cfg, err := loadYouTubePlaneConfig()
			require.NoError(t, err)

			applySourceObservationWorkerProfile(&cfg, profile)

			if size < 0 || size > 1000 {
				require.Error(t, cfg.Validate())
			} else {
				require.NoError(t, cfg.Validate())
			}
		})
	}

	t.Setenv("YOUTUBE_PLANE_TIME_FINALIZATION_BACKLOG_BATCH_SIZE", "invalid")

	_, err := loadYouTubePlaneConfig()
	require.Error(t, err)
}
