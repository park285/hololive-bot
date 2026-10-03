package sourceobservation

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-api/internal/youtube/reconcile/content"
	dbtest "github.com/kapu/hololive-dbtest"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	publishkit "github.com/kapu/hololive-youtube-collector/testkit/sourceobservation"
)

func populatedContentClock() content.EntityState {
	at := time.Date(2026, time.August, 14, 1, 0, 0, 0, time.UTC)
	negative := at.Add(time.Hour)
	negativeReceived := negative.Add(time.Second)
	firstAbsence := negative
	secondAbsence := negative.Add(time.Hour)
	missing := negative
	withdrawn := secondAbsence

	return content.EntityState{
		VideoID:                  testVideoID,
		ChannelID:                testChannelID,
		FirstPositiveEffectiveAt: at,
		Clock: content.ContentEvidenceClock{
			LastPositiveEffectiveAt: at,
			LastPositiveReceivedAt:  at.Add(time.Second),
			LastNegativeEffectiveAt: &negative,
			MissingSinceEffectiveAt: &missing,
		},
		LastPositiveValueSHA256: strings.Repeat("ab", 32),
		LastPositiveScopeSHA256: strings.Repeat("cd", 32),
		LastPositiveCoverage: content.VideoCoverage(&contract.ChannelListCoverageV1{
			ChannelID: testChannelID, MaxResults: 10, Exhausted: true,
		}),
		LastNegativeReceivedAt:    &negativeReceived,
		FirstAbsenceScheduledFor:  &firstAbsence,
		SecondAbsenceScheduledFor: &secondAbsence,
		LastAbsenceObservationID:  7,
		ConsecutiveAbsenceSlots:   2,
		WithdrawnAt:               &withdrawn,
	}
}

func contentClockUnchanged(t *testing.T, stored, next *content.EntityState) bool {
	t.Helper()

	coverage, err := content.MarshalCoverage(next.LastPositiveCoverage)
	if err != nil {
		t.Fatalf("marshal next coverage: %v", err)
	}

	unchanged, err := sameStoredContentClock(stored, next, coverage)
	if err != nil {
		t.Fatalf("compare content clock: %v", err)
	}

	return unchanged
}

// 0037이 저장하는 값 열 14개 중 하나라도 바뀌면 persist 대상이어야 한다. 비교에서 열이 빠지면 변경이 유실된다.
func TestSameStoredContentClockDetectsEveryPersistedColumn(t *testing.T) {
	t.Parallel()

	later := time.Date(2026, time.September, 1, 0, 0, 0, 0, time.UTC)
	mutations := map[string]func(*content.EntityState){
		"first_positive_effective_at":  func(c *content.EntityState) { c.FirstPositiveEffectiveAt = later },
		"last_positive_effective_at":   func(c *content.EntityState) { c.Clock.LastPositiveEffectiveAt = later },
		"last_positive_received_at":    func(c *content.EntityState) { c.Clock.LastPositiveReceivedAt = later },
		"last_positive_value_sha256":   func(c *content.EntityState) { c.LastPositiveValueSHA256 = strings.Repeat("ef", 32) },
		"last_positive_scope_sha256":   func(c *content.EntityState) { c.LastPositiveScopeSHA256 = strings.Repeat("ef", 32) },
		"last_positive_coverage":       func(c *content.EntityState) { c.LastPositiveCoverage.Videos.Exhausted = false },
		"last_negative_effective_at":   func(c *content.EntityState) { c.Clock.LastNegativeEffectiveAt = nil },
		"last_negative_received_at":    func(c *content.EntityState) { c.LastNegativeReceivedAt = &later },
		"first_absence_scheduled_for":  func(c *content.EntityState) { c.FirstAbsenceScheduledFor = nil },
		"second_absence_scheduled_for": func(c *content.EntityState) { c.SecondAbsenceScheduledFor = &later },
		"last_absence_observation_id":  func(c *content.EntityState) { c.LastAbsenceObservationID = 0 },
		"missing_since_effective_at":   func(c *content.EntityState) { c.Clock.MissingSinceEffectiveAt = &later },
		"consecutive_absence_slots":    func(c *content.EntityState) { c.ConsecutiveAbsenceSlots = 1 },
		"withdrawn_at":                 func(c *content.EntityState) { c.WithdrawnAt = nil },
	}

	stored := populatedContentClock()
	same := populatedContentClock()

	// 0037 인자는 video_id 키 하나와 값 열이다. 값 열을 추가하면 이 표에도 변형을 추가해야 비교 누락을 잡는다.
	if values := len(contentClockStatement(&stored, nil).Args) - 1; values != len(mutations) {
		t.Fatalf("0037 has %d value columns but %d mutations; add a mutation for each new column", values, len(mutations))
	}

	if !contentClockUnchanged(t, &stored, &same) {
		t.Fatal("identical clock must be treated as unchanged")
	}

	for column, mutate := range mutations {
		next := populatedContentClock()
		mutate(&next)

		if contentClockUnchanged(t, &stored, &next) {
			t.Fatalf("change of %s was treated as unchanged", column)
		}
	}
}

type contentClockRowVersion struct {
	ctid      string
	updatedAt string
}

func readContentClockRowVersion(ctx context.Context, t *testing.T, pool *pgxpool.Pool, videoID string) contentClockRowVersion {
	t.Helper()

	var version contentClockRowVersion

	if err := pool.QueryRow(ctx, `
		SELECT ctid::text, updated_at::text FROM youtube_content_evidence_clocks WHERE video_id = $1
	`, videoID).Scan(&version.ctid, &version.updatedAt); err != nil {
		t.Fatalf("load content clock %s version: %v", videoID, err)
	}

	return version
}

func TestContentClockUpsertGuardSkipsIdenticalValues(t *testing.T) {
	ctx := t.Context()
	pool := dbtest.NewPool(t)
	clock := populatedContentClock()

	clock.LastAbsenceObservationID = 0

	if _, err := pool.Exec(ctx, `
		INSERT INTO youtube_videos (video_id, channel_id, title, first_seen_at, last_seen_at)
		VALUES ($1, $2, $1, NOW(), NOW())
	`, clock.VideoID, clock.ChannelID); err != nil {
		t.Fatalf("seed catalog video: %v", err)
	}

	coverage, err := content.MarshalCoverage(clock.LastPositiveCoverage)
	if err != nil {
		t.Fatal(err)
	}

	execClock := func(label string) int64 {
		statement := contentClockStatement(&clock, coverage)

		tag, execErr := pool.Exec(ctx, statement.SQL, statement.Args...)
		if execErr != nil {
			t.Fatalf("%s: %v", label, execErr)
		}

		return tag.RowsAffected()
	}

	if affected := execClock("insert clock"); affected != 1 {
		t.Fatalf("insert affected %d rows, want 1", affected)
	}

	before := readContentClockRowVersion(ctx, t, pool, clock.VideoID)

	if affected := execClock("replay identical clock"); affected != 0 {
		t.Fatalf("identical upsert affected %d rows, want 0", affected)
	}

	if after := readContentClockRowVersion(ctx, t, pool, clock.VideoID); after != before {
		t.Fatalf("identical upsert rewrote the clock: before=%+v after=%+v", before, after)
	}

	clock.ConsecutiveAbsenceSlots = 1

	if affected := execClock("change clock"); affected != 1 {
		t.Fatalf("changed upsert affected %d rows, want 1", affected)
	}

	var slots int

	if err := pool.QueryRow(ctx, `
		SELECT consecutive_absence_slots FROM youtube_content_evidence_clocks WHERE video_id = $1
	`, clock.VideoID).Scan(&slots); err != nil {
		t.Fatal(err)
	}

	if slots != 1 {
		t.Fatalf("consecutive_absence_slots = %d, want 1", slots)
	}
}

// 이번 관측에 없고 부재 판정도 없는(PARTIAL) clock은 Decision.Clocks 스냅샷에 남아도 다시 쓰지 않는다.
func TestContentConsumerPersistsOnlyChangedClocks(t *testing.T) {
	pool, _, consumer, proof := startContentPersist(t)
	ctx := t.Context()

	proof = publishConsumeVideos(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, contract.CompletenessPartial, "vid-a", "vid-b")

	untouched := readContentClockRowVersion(ctx, t, pool, "vid-a")
	observed := readContentClockRowVersion(ctx, t, pool, "vid-b")

	publishConsumeVideos(ctx, t, pool, publishkit.NewPublisher(pool), consumer, &proof, contract.CompletenessPartial, "vid-b")

	if after := readContentClockRowVersion(ctx, t, pool, "vid-a"); after != untouched {
		t.Fatalf("unchanged clock was rewritten: before=%+v after=%+v", untouched, after)
	}

	if after := readContentClockRowVersion(ctx, t, pool, "vid-b"); after == observed {
		t.Fatalf("observed clock was not advanced: %+v", after)
	}

	var lastPositive, firstPositive time.Time

	if err := pool.QueryRow(ctx, `
		SELECT last_positive_effective_at, first_positive_effective_at
		FROM youtube_content_evidence_clocks
		WHERE video_id = 'vid-b'
	`).Scan(&lastPositive, &firstPositive); err != nil {
		t.Fatal(err)
	}

	if !lastPositive.After(firstPositive) {
		t.Fatalf("vid-b last positive %s did not advance past first positive %s", lastPositive, firstPositive)
	}
}
