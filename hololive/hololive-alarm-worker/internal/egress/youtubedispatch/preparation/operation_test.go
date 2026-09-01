package preparation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
)

func TestPreparedOperationFreezesFencesMembershipAndRequest(t *testing.T) {
	t.Parallel()

	ownerRow := testDelivery(t, 1, lifecycle.StatusPending, resolverTestNow, true)
	followerRow := testDelivery(t, 2, lifecycle.StatusPending, resolverTestNow.Add(time.Minute), false)
	resolution := testResolver(t, 10).ResolveGroups(
		[]DeliverySnapshot{ownerRow, followerRow}, nil, nil, resolverTestNow,
	)[0]
	owner, err := NewPreparedOwner(resolution, resolverTestNow.Add(999*time.Nanosecond))
	require.NoError(t, err)

	dedupeKeys := []string{"delivery:1"}
	request, err := NewImmutableSendRequest("room-1", "message", dedupeKeys)
	require.NoError(t, err)

	dedupeKeys[0] = "mutated"

	operation, err := NewPreparedSendOperation(
		"operation-1",
		"client-request-1",
		[]PreparedOwner{owner},
		[]lifecycle.TrackingRequirement{lifecycle.NoTracking{}},
		request,
		resolverTestNow.Add(999*time.Nanosecond),
	)
	require.NoError(t, err)
	require.Equal(t, int64(1), operation.Owners()[0].DeliveryID())
	require.Equal(t, []int64{2}, operation.Owners()[0].FollowerIDs())
	require.Equal(t, ownerRow.RowVersion, operation.Owners()[0].ExpectedVersion())
	require.Equal(t, ownerRow.RowVersion+1, operation.Owners()[0].NextVersion())
	require.Equal(t, lifecycle.StatusPending, operation.Owners()[0].ExpectedStatus())
	require.Equal(t, lifecycle.StatusSending, operation.Owners()[0].NextStatus())
	require.Equal(t, []string{"delivery:1"}, operation.Request().DedupeKeys())
	require.Equal(t, resolverTestNow, operation.PreparedAt())
	require.Equal(t, operation.Owners()[0].Key(), operation.LedgerKeys()[0])
}

func TestPreparedOperationRejectsDuplicateLogicalOwner(t *testing.T) {
	t.Parallel()

	row := testDelivery(t, 1, lifecycle.StatusPending, resolverTestNow, true)
	resolution := testResolver(t, 10).ResolveGroups([]DeliverySnapshot{row}, nil, nil, resolverTestNow)[0]
	owner, err := NewPreparedOwner(resolution, resolverTestNow)
	require.NoError(t, err)

	request, err := NewImmutableSendRequest("room-1", "message", []string{"delivery:1"})
	require.NoError(t, err)

	_, err = NewPreparedSendOperation(
		"operation-1", "client-request-1", []PreparedOwner{owner, owner},
		[]lifecycle.TrackingRequirement{lifecycle.NoTracking{}}, request, resolverTestNow,
	)
	require.ErrorContains(t, err, "duplicate owner")
}
