package preparation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/lifecycle"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
)

var resolverTestNow = time.Date(2026, time.August, 31, 3, 0, 0, 0, time.UTC)

func TestLogicalKeyUsesCanonicalPostAndRoom(t *testing.T) {
	t.Parallel()

	row := testDelivery(t, 1, lifecycle.StatusPending, resolverTestNow, true)

	row.Kind = domain.OutboxKindCommunityPost
	row.ContentID = "community:UgkxPost123"
	row.Payload = `{"post_id":"UgkxPost123","canonical_post_id":"community:UgkxPost123"}`
	row.RoomID = " room-1 "

	result := testResolver(t, 10).ResolveGroups([]DeliverySnapshot{row}, nil, nil, resolverTestNow)
	require.Len(t, result, 1)
	require.Equal(t, LogicalActive, result[0].Kind())
	require.Equal(t, "community:UgkxPost123", result[0].Key().LogicalID)
	require.Equal(t, "room-1", result[0].Key().RoomID)
}

func TestSameBatchSelectsDeterministicOwner(t *testing.T) {
	t.Parallel()

	later := testDelivery(t, 9, lifecycle.StatusPending, resolverTestNow.Add(time.Minute), true)
	earlierHigherID := testDelivery(t, 8, lifecycle.StatusPending, resolverTestNow, true)
	earlierLowerID := testDelivery(t, 2, lifecycle.StatusPending, resolverTestNow, true)

	result := testResolver(t, 10).ResolveGroups([]DeliverySnapshot{later, earlierHigherID, earlierLowerID}, nil, nil, resolverTestNow)
	require.Len(t, result, 1)
	require.Equal(t, int64(2), result[0].Owner().DeliveryID)
	require.Len(t, result[0].Followers(), 2)
}

func TestFollowerCannotUseIndependentAttemptBudget(t *testing.T) {
	t.Parallel()

	owner := testDelivery(t, 1, lifecycle.StatusPending, resolverTestNow, false)

	owner.AttemptCount = 4

	follower := testDelivery(t, 2, lifecycle.StatusPending, resolverTestNow.Add(time.Minute), true)

	follower.AttemptCount = 0

	result := testResolver(t, 10).ResolveGroups([]DeliverySnapshot{follower, owner}, nil, nil, resolverTestNow)
	require.Equal(t, LogicalOwnerPendingElsewhere, result[0].Kind())
	require.Equal(t, int64(1), result[0].Owner().DeliveryID)
	require.False(t, result[0].ProviderAllowed())
}

func TestPendingOwnerDefersFollowerToOwnerDue(t *testing.T) {
	t.Parallel()

	owner := testDelivery(t, 1, lifecycle.StatusPending, resolverTestNow, false)

	owner.NextAttemptAt = resolverTestNow.Add(7 * time.Minute)

	follower := testDelivery(t, 2, lifecycle.StatusPending, resolverTestNow.Add(time.Minute), true)

	result := testResolver(t, 10).ResolveGroups([]DeliverySnapshot{owner, follower}, nil, nil, resolverTestNow)
	require.Equal(t, LogicalOwnerPendingElsewhere, result[0].Kind())
	require.Equal(t, owner.NextAttemptAt, result[0].Due())
}

func TestFailedOwnerMirrorsFollowerFailed(t *testing.T) {
	t.Parallel()

	owner := testDelivery(t, 1, lifecycle.StatusFailed, resolverTestNow, false)
	follower := testDelivery(t, 2, lifecycle.StatusPending, resolverTestNow.Add(time.Minute), true)

	result := testResolver(t, 10).ResolveGroups([]DeliverySnapshot{owner, follower}, nil, nil, resolverTestNow)
	require.Equal(t, LogicalFailed, result[0].Kind())
	require.Equal(t, int64(1), result[0].Owner().DeliveryID)
	require.False(t, result[0].ProviderAllowed())
}

func TestSentEvidenceReconcilesFailedAndQuarantinedFollowers(t *testing.T) {
	t.Parallel()

	failed := testDelivery(t, 1, lifecycle.StatusFailed, resolverTestNow, false)
	quarantined := testDelivery(t, 2, lifecycle.StatusQuarantined, resolverTestNow.Add(time.Minute), false)
	key := mustKey(t, failed)

	result := testResolver(t, 10).ResolveGroups(
		[]DeliverySnapshot{failed, quarantined},
		[]LedgerEvidence{{Key: key, Status: lifecycle.LedgerSent, RecordedAt: resolverTestNow}},
		nil,
		resolverTestNow,
	)
	require.Equal(t, LogicalFulfilled, result[0].Kind())
	require.Len(t, result[0].Members(), 2)
}

func TestQuarantinedEvidenceBlocksProviderCall(t *testing.T) {
	t.Parallel()

	row := testDelivery(t, 1, lifecycle.StatusPending, resolverTestNow, true)
	key := mustKey(t, row)
	result := testResolver(t, 10).ResolveGroups(
		[]DeliverySnapshot{row},
		[]LedgerEvidence{{Key: key, Status: lifecycle.LedgerQuarantined}},
		nil,
		resolverTestNow,
	)
	require.Equal(t, LogicalUnresolved, result[0].Kind())
	require.False(t, result[0].ProviderAllowed())
}

func TestSendingEvidenceDefersProviderCall(t *testing.T) {
	t.Parallel()

	sending := testDelivery(t, 1, lifecycle.StatusSending, resolverTestNow, false)

	sending.LockedAt = resolverTestNow.Add(-30 * time.Second)

	pending := testDelivery(t, 2, lifecycle.StatusPending, resolverTestNow.Add(time.Minute), true)

	result := testResolver(t, 10).ResolveGroups([]DeliverySnapshot{sending, pending}, nil, nil, resolverTestNow)
	require.Equal(t, LogicalInFlight, result[0].Kind())
	require.Equal(t, resolverTestNow.Add(90*time.Second), result[0].Due())
	require.False(t, result[0].ProviderAllowed())
}

func TestQuarantineAndSendingMixedStateIsBreach(t *testing.T) {
	t.Parallel()

	sending := testDelivery(t, 1, lifecycle.StatusSending, resolverTestNow, false)
	quarantined := testDelivery(t, 2, lifecycle.StatusQuarantined, resolverTestNow.Add(time.Minute), false)

	result := testResolver(t, 10).ResolveGroups([]DeliverySnapshot{sending, quarantined}, nil, nil, resolverTestNow)
	require.Equal(t, LogicalInvariantBreach, result[0].Kind())
	require.Equal(t, InvariantQuarantinedAndSending, result[0].InvariantReason())
}

func TestMultipleSendingRowsIsBreach(t *testing.T) {
	t.Parallel()

	first := testDelivery(t, 1, lifecycle.StatusSending, resolverTestNow, false)
	second := testDelivery(t, 2, lifecycle.StatusSending, resolverTestNow.Add(time.Minute), false)

	result := testResolver(t, 10).ResolveGroups([]DeliverySnapshot{first, second}, nil, nil, resolverTestNow)
	require.Equal(t, LogicalInvariantBreach, result[0].Kind())
	require.Equal(t, InvariantMultipleSending, result[0].InvariantReason())
}

func TestProceedCacheHitStillResolvesLogicalGroup(t *testing.T) {
	t.Parallel()

	row := testDelivery(t, 1, lifecycle.StatusPending, resolverTestNow, true)
	key := mustKey(t, row)
	coordinator := NewCoordinator(testResolver(t, 10))
	result := coordinator.ResolveForPreparation(PrepareBatchInput{
		Rows: []DeliverySnapshot{row}, Ledger: []LedgerEvidence{{Key: key, Status: lifecycle.LedgerQuarantined}},
		At: resolverTestNow, ProceedCacheHit: true,
	})
	require.Equal(t, LogicalUnresolved, result[0].Kind())
	require.False(t, result[0].ProviderAllowed())
}

func TestLogicalGroupOverflowFailsClosed(t *testing.T) {
	t.Parallel()

	first := testDelivery(t, 1, lifecycle.StatusPending, resolverTestNow, true)
	second := testDelivery(t, 2, lifecycle.StatusPending, resolverTestNow.Add(time.Minute), false)

	result := testResolver(t, 1).ResolveGroups([]DeliverySnapshot{first, second}, nil, nil, resolverTestNow)
	require.Equal(t, LogicalInvariantBreach, result[0].Kind())
	require.Equal(t, InvariantGroupScanOverflow, result[0].InvariantReason())
}

func TestInvalidLogicalIdentityFailsBeforeProviderCall(t *testing.T) {
	t.Parallel()

	row := testDelivery(t, 1, lifecycle.StatusPending, resolverTestNow, true)

	row.Kind = domain.OutboxKindNewShort
	row.ContentID = "short:video-a"
	row.Payload = `{"canonical_post_id":"short:video-b"}`

	result := testResolver(t, 10).ResolveGroups([]DeliverySnapshot{row}, nil, nil, resolverTestNow)
	require.Equal(t, LogicalInvariantBreach, result[0].Kind())
	require.Equal(t, InvariantInvalidLogicalIdentity, result[0].InvariantReason())
	require.False(t, result[0].ProviderAllowed())
}

func TestLedgerSentEvidenceReconcilesCleanedPhysicalRows(t *testing.T) {
	t.Parallel()

	key, err := ytcontentid.ResolveLogicalKey(domain.OutboxKindNewVideo, "video-1", "room-1")
	require.NoError(t, err)

	result := testResolver(t, 10).ResolveGroups(
		nil,
		[]LedgerEvidence{{Key: key, Status: lifecycle.LedgerSent}},
		[]ytcontentid.LogicalKey{key},
		resolverTestNow,
	)
	require.Len(t, result, 1)
	require.Equal(t, LogicalFulfilled, result[0].Kind())
}

func TestLedgerQuarantineEvidenceBlocksProviderCall(t *testing.T) {
	t.Parallel()

	key, err := ytcontentid.ResolveLogicalKey(domain.OutboxKindNewVideo, "video-1", "room-1")
	require.NoError(t, err)

	result := testResolver(t, 10).ResolveGroups(
		nil,
		[]LedgerEvidence{{Key: key, Status: lifecycle.LedgerQuarantined}},
		[]ytcontentid.LogicalKey{key},
		resolverTestNow,
	)
	require.Equal(t, LogicalUnresolved, result[0].Kind())
	require.False(t, result[0].ProviderAllowed())
}

func testResolver(t *testing.T, limit int) Resolver {
	t.Helper()

	resolver, err := NewResolver(ResolverConfig{
		LogicalGroupScanLimit: limit,
		RetryBackoff:          time.Minute,
		LockTimeout:           2 * time.Minute,
		RequireTerminalLedger: true,
	})
	require.NoError(t, err)

	return resolver
}

func testDelivery(t *testing.T, id int64, status lifecycle.DeliveryStatus, createdAt time.Time, current bool) DeliverySnapshot {
	t.Helper()

	lockedAt := resolverTestNow
	row := DeliverySnapshot{
		DeliveryID: id, OutboxID: 10, Kind: domain.OutboxKindNewVideo,
		ContentID: "video-1", RoomID: "room-1", Status: status,
		AttemptCount: int(id), NextAttemptAt: resolverTestNow.Add(time.Minute),
		CreatedAt: createdAt, LockedAt: lockedAt, RowVersion: id + 10,
		InCurrentBatch: current,
	}

	if current {
		lease, err := lifecycle.NewPreparationLease(row.DeliveryID, row.RowVersion, lockedAt)
		require.NoError(t, err)

		row.Lease = lease
	}

	return row
}

func mustKey(t *testing.T, row DeliverySnapshot) ytcontentid.LogicalKey {
	t.Helper()

	key, err := row.logicalKey()
	require.NoError(t, err)

	return key
}
