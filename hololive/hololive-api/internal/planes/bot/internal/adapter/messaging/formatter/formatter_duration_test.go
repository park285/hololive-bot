package formatter

import (
	"testing"
	"time"
)

func TestFormatBroadcastDuration(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		in   time.Duration
		want string
	}{
		{-time.Minute, "1분 미만"},
		{59 * time.Second, "1분 미만"},
		{time.Minute + 59*time.Second, "1분"},
		{45 * time.Minute, "45분"},
		{2 * time.Hour, "2시간"},
		{90 * time.Minute, "1시간 30분"},
		{24 * time.Hour, "1일"},
		{77*time.Hour + 5*time.Minute, "3일 5시간 5분"},
		{48*time.Hour + 7*time.Minute, "2일 7분"},
	} {
		if got := formatBroadcastDuration(tc.in); got != tc.want {
			t.Errorf("formatBroadcastDuration(%v) = %q, want %q", tc.in, got, tc.want)
		}
	}
}
