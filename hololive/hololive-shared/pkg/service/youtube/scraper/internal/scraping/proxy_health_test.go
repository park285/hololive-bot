package scraping

import (
	"testing"

	"github.com/stretchr/testify/assert"
)

func TestProxyHealthTrackerDisabledNeverTriggers(t *testing.T) {
	tracker := newProxyHealthTracker(ProxyFallbackPolicy{})
	for range 10 {
		assert.False(t, tracker.RecordTransportFailure())
	}
}

func TestProxyHealthTrackerTriggersAtThreshold(t *testing.T) {
	tracker := newProxyHealthTracker(ProxyFallbackPolicy{
		Enabled:                true,
		MaxConsecutiveFailures: 3,
	})
	assert.False(t, tracker.RecordTransportFailure())
	assert.False(t, tracker.RecordTransportFailure())
	assert.True(t, tracker.RecordTransportFailure(), "3rd failure should signal fallback")
	// 이미 fallback 신호 후에는 중복 트리거하지 않음.
	assert.False(t, tracker.RecordTransportFailure())
}

func TestProxyHealthTrackerSuccessResetsCounter(t *testing.T) {
	tracker := newProxyHealthTracker(ProxyFallbackPolicy{
		Enabled:                true,
		MaxConsecutiveFailures: 3,
	})
	tracker.RecordTransportFailure()
	tracker.RecordTransportFailure()
	tracker.RecordSuccess()
	assert.False(t, tracker.RecordTransportFailure())
	assert.False(t, tracker.RecordTransportFailure())
	assert.True(t, tracker.RecordTransportFailure())
}

func TestProxyHealthTrackerArmAllowsNewWindow(t *testing.T) {
	tracker := newProxyHealthTracker(ProxyFallbackPolicy{
		Enabled:                true,
		MaxConsecutiveFailures: 2,
	})
	tracker.RecordTransportFailure()
	assert.True(t, tracker.RecordTransportFailure())

	// Arm은 새로운 관찰 window를 시작하여 다시 임계치까지 카운트 가능.
	tracker.Arm()
	assert.False(t, tracker.RecordTransportFailure())
	assert.True(t, tracker.RecordTransportFailure())
}
