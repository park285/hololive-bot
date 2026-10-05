package membernews

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/filter"
	"github.com/kapu/hololive-api/internal/planes/llm/internal/service/membernews/model"
	"github.com/kapu/hololive-shared/pkg/timeutil"
)

type countingMemberNewsQuerier struct {
	memberNewsQuerier

	candidateQueries atomic.Int32
	memberQueries    atomic.Int32
	bounds           []any
}

func (q *countingMemberNewsQuerier) Query(ctx context.Context, sql string, args ...any) (rowsScanner, error) {
	if strings.Contains(sql, "FROM major_events") {
		q.candidateQueries.Add(1)

		q.bounds = slices.Clone(args)
	}

	if strings.Contains(sql, "FROM alarms") {
		q.memberQueries.Add(1)
	}

	rows, err := q.memberNewsQuerier.Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("counting query: %w", err)
	}

	return rows, nil
}

type snapshotDigestSummarizer struct {
	now     time.Time
	members [][]string
	titles  []string
}

func (s *snapshotDigestSummarizer) Summarize(_ context.Context, input *model.SummarizeInput) (*model.Digest, error) {
	s.now = input.Now
	s.members = append(s.members, slices.Clone(input.Candidates[0].MatchedMembers))
	s.titles = append(s.titles, input.Candidates[0].Candidate.Title)
	// 요약기 소비자가 입력을 바꾸더라도 다음 방의 공유 후보를 바꾸면 안 된다.
	input.Candidates[0].Candidate.Members[0] = "changed by room"
	*input.Candidates[0].Candidate.PubDate = time.Time{}

	return &model.Digest{TopItems: []model.SummaryItem{{Title: input.Candidates[0].Candidate.Title}}}, nil
}

func TestDigestRunPinsCandidateClockAndReadsRoomMembersPerRoom(t *testing.T) {
	repository, pool := newPeriodCandidatePool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `CREATE TABLE alarms(room_id text, channel_id text, member_name text);
  CREATE TABLE members(channel_id text, korean_name text, english_name text, japanese_name text);
  INSERT INTO alarms VALUES ('room-a','channel','미코'),('room-b','channel','미코');
  INSERT INTO major_events VALUES
   (1,'news','original September candidate','',ARRAY['미코','아쿠아'],'2026-09-30T14:59:59Z',NULL,'https://hololivepro.com/old','active','unchecked'),
   (2,'news','new October candidate','',ARRAY['미코','아쿠아'],'2026-09-30T15:00:01Z',NULL,'https://hololivepro.com/new','active','unchecked')`)
	if err != nil {
		t.Fatal(err)
	}

	service, summary, counter := newSnapshotDigestService(t, repository)

	service.SetClock(func() time.Time {
		t.Error("prepared run must use supplied clock")

		return time.Time{}
	})

	now := time.Date(2026, time.September, 30, 23, 59, 59, 0, timeutil.KSTZone)

	run, err := service.PrepareDigestRun(ctx, model.PeriodMonthly, now)
	if err != nil {
		t.Fatal(err)
	}

	if _, roomErr := run.GenerateRoomDigest(ctx, "room-a", model.PeriodMonthly); roomErr != nil {
		t.Fatal(roomErr)
	}

	_, err = pool.Exec(ctx, `UPDATE major_events SET title='changed after snapshot', pub_date='2026-10-01';
  UPDATE alarms SET member_name='아쿠아' WHERE room_id='room-b'`)
	if err != nil {
		t.Fatal(err)
	}

	if _, err := run.GenerateRoomDigest(ctx, "room-b", model.PeriodMonthly); err != nil {
		t.Fatal(err)
	}

	if counter.candidateQueries.Load() != 1 || counter.memberQueries.Load() != 2 {
		t.Fatalf("candidate queries=%d member queries=%d", counter.candidateQueries.Load(), counter.memberQueries.Load())
	}

	if !summary.now.Equal(now) || !slices.Equal(summary.titles, []string{"original September candidate", "original September candidate"}) {
		t.Fatalf("summary clock=%v titles=%v", summary.now, summary.titles)
	}

	if !slices.Equal(summary.members[0], []string{"미코"}) || !slices.Equal(summary.members[1], []string{"아쿠아"}) {
		t.Fatalf("per-room members=%v", summary.members)
	}

	assertCandidateSQLBounds(t, counter.bounds, model.PeriodMonthly, now)

	if _, err := run.GenerateRoomDigest(ctx, "room-a", model.PeriodWeekly); err == nil {
		t.Fatal("prepared run accepted another period")
	}
}

func TestManualDigestUsesOneClockAndPreservesNoMemberPriority(t *testing.T) {
	repository, pool := newPeriodCandidatePool(t)
	ctx := t.Context()

	_, err := pool.Exec(ctx, `CREATE TABLE alarms(room_id text, channel_id text, member_name text);
  CREATE TABLE members(channel_id text, korean_name text, english_name text, japanese_name text);
  INSERT INTO alarms VALUES ('room-a','channel','미코');
  INSERT INTO major_events VALUES (1,'news','September candidate','',ARRAY['미코'],'2026-09-30T14:59:59Z',NULL,'https://hololivepro.com/old','active','unchecked')`)
	if err != nil {
		t.Fatal(err)
	}

	service, summary, counter := newSnapshotDigestService(t, repository)
	now := time.Date(2026, time.September, 30, 23, 59, 59, 0, timeutil.KSTZone)
	clockCalls := 0

	service.SetClock(func() time.Time { clockCalls++; return now.Add(time.Duration(clockCalls-1) * 2 * time.Second) })

	if _, err := service.GenerateRoomDigest(ctx, "room-a", model.PeriodMonthly); err != nil {
		t.Fatal(err)
	}

	if clockCalls != 1 || !summary.now.Equal(now) {
		t.Fatalf("clock calls=%d summary=%v", clockCalls, summary.now)
	}

	if _, err := service.GenerateRoomDigest(ctx, "no-members", model.PeriodWeekly); !errors.Is(err, model.ErrNoSubscribedMembers) {
		t.Fatalf("no-member error=%v", err)
	}

	if counter.candidateQueries.Load() != 1 || clockCalls != 1 {
		t.Fatalf("no-member request loaded candidates or clock: queries=%d calls=%d", counter.candidateQueries.Load(), clockCalls)
	}
}

func newSnapshotDigestService(t *testing.T, repository *Repository) (*Service, *snapshotDigestSummarizer, *countingMemberNewsQuerier) {
	t.Helper()

	validator, err := NewSourceValidator(t.Context(), "", nil, nil)
	if err != nil {
		t.Fatal(err)
	}

	summary := &snapshotDigestSummarizer{}
	counter := &countingMemberNewsQuerier{memberNewsQuerier: repository.pool}

	repository.pool = counter

	service := NewService(repository, summary, validator, nil, nil, WithPromptGuard(newMemberNewsPromptGuard(t)))

	return service, summary, counter
}

func assertCandidateSQLBounds(t *testing.T, bounds []any, period model.Period, now time.Time) {
	t.Helper()

	start, end := filter.PeriodBounds(period, now)
	gotStart, startOK := bounds[0].(time.Time)
	gotEnd, endOK := bounds[1].(time.Time)

	if !startOK || !endOK || !gotStart.Equal(start) || !gotEnd.Equal(end) {
		t.Fatalf("SQL bounds=%v want=%v/%v", bounds, start, end)
	}
}
