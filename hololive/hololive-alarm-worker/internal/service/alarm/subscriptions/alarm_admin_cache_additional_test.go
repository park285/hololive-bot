// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package subscriptions

import (
	"log/slog"
	"testing"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

func newDiscardAlarmLogger() *slog.Logger {
	return slog.New(slog.DiscardHandler)
}

func TestAlarmKeyHelpers(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	assert.Equal(t, testRoomID, as.getRegistryKey(testRoomID))
	assert.Equal(t, sharedalarmkeys.ChannelSubscribersKeyPrefix+testChannelID, as.channelSubscribersKey(testChannelID))
}

func TestAlarmCacheNameAndSubscriberHelpers(t *testing.T) {
	t.Parallel()

	as := newTestAlarmService(t)
	ctx := t.Context()

	require.NoError(t, as.CacheMemberName(ctx, testChannelID, testMemberName))

	name, err := as.GetMemberName(ctx, testChannelID)
	require.NoError(t, err)
	assert.Equal(t, testMemberName, name)

	_, err = as.cache.SAdd(ctx, as.channelSubscribersKeyByType(testChannelID, domain.AlarmTypeLive), []string{testRoomID})
	require.NoError(t, err)

	_, err = as.cache.SAdd(ctx, as.channelSubscribersKeyByType(testChannelID, domain.AlarmTypeCommunity), []string{testRoomID})
	require.NoError(t, err)

	_, err = as.cache.SAdd(ctx, as.channelSubscribersKeyByType(testChannelID, domain.AlarmTypeShorts), []string{testRoomID})
	require.NoError(t, err)

	liveSubs, err := as.GetChannelSubscribersByType(ctx, testChannelID, domain.AlarmTypeLive)
	require.NoError(t, err)
	assert.Equal(t, []string{testRoomID}, liveSubs)

	communitySubs, err := as.GetChannelSubscribersByType(ctx, testChannelID, domain.AlarmTypeCommunity)
	require.NoError(t, err)
	assert.Equal(t, []string{testRoomID}, communitySubs)

	shortsSubs, err := as.GetChannelSubscribersByType(ctx, testChannelID, domain.AlarmTypeShorts)
	require.NoError(t, err)
	assert.Equal(t, []string{testRoomID}, shortsSubs)
}

func TestTargetMinutesUpdatesReturnConfirmedResult(t *testing.T) {
	t.Parallel()

	as := &AlarmService{logger: newDiscardAlarmLogger()}
	assert.Equal(t, []int{5, 3, 1}, as.GetTargetMinutes())

	updated, err := as.UpdateAlarmAdvanceMinutes(t.Context(), 10)
	require.NoError(t, err)
	assert.Equal(t, domain.ApplyConfirmed, updated.Outcome)
	assert.Equal(t, 10, updated.RequestedMinutes)
	assert.Equal(t, []int{10, 3, 1}, updated.TargetMinutes)
	assert.Equal(t, []int{10, 3, 1}, as.GetTargetMinutes())

	updated, err = as.UpdateAlarmAdvanceMinutes(t.Context(), 1)
	require.NoError(t, err)
	assert.Equal(t, domain.ApplyConfirmed, updated.Outcome)
	assert.Equal(t, 1, updated.RequestedMinutes)
	assert.Equal(t, []int{1}, updated.TargetMinutes)
	assert.Equal(t, []int{1}, as.GetTargetMinutes())
}
