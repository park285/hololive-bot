package majorevent

import (
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestEventLinkStatusArraysCarryCheckedLinkForGuard(t *testing.T) {
	t.Parallel()

	checkedAt := time.Date(2026, time.August, 7, 12, 0, 0, 0, time.UTC)
	events := []*domain.MajorEvent{
		nil,
		{ID: 0, Link: "https://a", LinkStatus: domain.MajorEventLinkStatusOK, LinkCheckedAt: &checkedAt},
		{ID: 1, Link: "https://checked", LinkStatus: domain.MajorEventLinkStatusFailed, LinkCheckedAt: &checkedAt},
		{ID: 2, Link: "https://unchecked", LinkStatus: domain.MajorEventLinkStatusUnchecked},
	}

	ids, statuses, checkedAts, links := eventLinkStatusArrays(events)

	require.Equal(t, []int{1}, ids)
	assert.Equal(t, []string{"failed"}, statuses)
	assert.Equal(t, []time.Time{checkedAt}, checkedAts)
	assert.Equal(t, []string{"https://checked"}, links,
		"persist must target the link that was actually checked, not whatever the row holds now")
}
