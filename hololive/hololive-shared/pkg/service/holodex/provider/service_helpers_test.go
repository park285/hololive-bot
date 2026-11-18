package holodexprovider

import (
	"testing"
)

const (
	testOpChannelsLiveStatus = "channels_live_status"
	testVideoID              = "video-1"
	testChannelID            = "channel-1"
)

func TestNormalizeStreamOrg_TrimsSpaceAndBang(t *testing.T) {
	t.Parallel()

	if got := normalizeStreamOrg("  Hololive! "); got != "hololive" {
		t.Fatalf("normalizeStreamOrg() = %q, want %q", got, "hololive")
	}
}
