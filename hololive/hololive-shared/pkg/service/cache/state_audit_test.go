package cache

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

func TestAuditPersistedStateCountsShapesWithoutPayloads(t *testing.T) {
	service, mini := newTestCacheService(t)

	require.NoError(t, service.Set(t.Context(), "notified:canonical-stream:1785118500:5", "1", time.Minute))
	mini.HSet("notified:canonical-hash", "start_scheduled", "2026-07-27T10:15:00Z")
	mini.HSet("notified:canonical-hash", "5", "1")
	require.NoError(t, mini.Set("notified:old-stream", `{"start_scheduled":"2026-07-27T10:15:00Z","sent_at":{"5":true}}`))
	mini.HSet("notified:incomplete-hash", "start_scheduled", "2026-07-27T10:15:00Z")
	mini.HSet("notified:unknown:shape", "start_scheduled", "2026-07-27T10:15:00Z")
	mini.HSet("notified:unknown:shape", "5", "1")
	require.NoError(t, mini.Set("notified:upcoming:event:room:channel:1785118500:fingerprint", `{"notified_at":"2026-07-27T10:15:00Z"}`))
	mini.HSet(memberHashKey, "Miko:Hololive", "canonical-channel")
	mini.HSet(memberHashKey, "Miko", "old-channel")

	report, err := service.AuditPersistedState(context.Background())
	require.NoError(t, err)
	require.Equal(t, 2, report.CanonicalNotifiedKeys)
	require.Equal(t, 3, report.AggregateNotifiedLegacyKeys)
	require.Equal(t, 1, report.CanonicalMemberFields)
	require.Equal(t, 1, report.OldMemberCacheKeys)
}

func TestAuditPersistedStateReportsMissingMemberHash(t *testing.T) {
	service, _ := newTestCacheService(t)

	report, err := service.AuditPersistedState(context.Background())
	require.NoError(t, err)
	require.Equal(t, 1, report.NotifiedKeysMissing)
	require.Equal(t, 1, report.MemberHashMissing)
}

func TestAuditPersistedStateReturnsReadFailure(t *testing.T) {
	service, _ := newTestCacheService(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	_, err := service.AuditPersistedState(ctx)
	require.Error(t, err)
}

func TestStateAuditShapeClassifiers(t *testing.T) {
	t.Parallel()
	require.True(t, isCanonicalNotifiedMinuteKey("notified:stream:1785118500:5"))
	require.False(t, isCanonicalNotifiedMinuteKey("notified:stream"))
	require.False(t, isCanonicalNotifiedMinuteKey("notified:schedule:transition:stream:1785118500:1785118560"))
	require.True(t, isAggregateNotifiedKey("notified:stream"))
	require.False(t, isAggregateNotifiedKey("notified:unknown:shape"))
	require.True(t, isKnownUnrelatedNotifiedKey("notified:upcoming:event:room:channel:1785118500:fingerprint"))
	require.False(t, isKnownUnrelatedNotifiedKey("notified:unknown:shape"))
	require.True(t, isCanonicalMemberField("Miko:Hololive"))
	require.False(t, isCanonicalMemberField("Miko"))
	require.False(t, isCanonicalMemberField("Miko:Hololive:extra"))
}

func TestStateAuditClassifiesMissingNotifiedKey(t *testing.T) {
	service, _ := newTestCacheService(t)

	shape, err := service.classifyNotifiedKey(t.Context(), "notified:missing-stream")
	require.NoError(t, err)
	require.Equal(t, stateShapeMissing, shape)
}
