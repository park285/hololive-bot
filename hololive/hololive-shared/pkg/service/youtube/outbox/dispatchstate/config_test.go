package dispatchstate

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestNormalizeDispatcherConfigKeepsClaimFreshnessAboveReviveWindow(t *testing.T) {
	t.Parallel()

	t.Run("zero claim window defaults above revive", func(t *testing.T) {
		t.Parallel()
		got := NormalizeDispatcherConfig(&Config{})
		require.Equal(t, 2*time.Hour, got.ClaimFreshnessWindow)
		require.Greater(t, got.ClaimFreshnessWindow, got.ReviveFreshnessWindow)
	})

	t.Run("claim window at or below revive is raised above it", func(t *testing.T) {
		t.Parallel()
		got := NormalizeDispatcherConfig(&Config{ClaimFreshnessWindow: 30 * time.Minute})
		require.Greater(t, got.ClaimFreshnessWindow, got.ReviveFreshnessWindow)
	})

	t.Run("claim window above revive is preserved", func(t *testing.T) {
		t.Parallel()
		got := NormalizeDispatcherConfig(&Config{ClaimFreshnessWindow: 90 * time.Minute})
		require.Equal(t, 90*time.Minute, got.ClaimFreshnessWindow)
	})

	t.Run("claim window below revive plus interval is raised", func(t *testing.T) {
		t.Parallel()
		got := NormalizeDispatcherConfig(&Config{
			ReviveFreshnessWindow: 60 * time.Minute,
			ReviveInterval:        5 * time.Minute,
			ClaimFreshnessWindow:  62 * time.Minute,
		})
		require.Equal(t, 65*time.Minute, got.ClaimFreshnessWindow)
	})
}
