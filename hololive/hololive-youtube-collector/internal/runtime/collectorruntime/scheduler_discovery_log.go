package collectorruntime

import (
	"context"
	"log/slog"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

// discoveryFailureLogInterval은 같은 후보 탐색 실패가 이어질 때 WARN을 다시 남기는 최소 간격이다. 탐색은 acquisition
// cadence(기본 1초)마다 돌므로 DB 장애 동안 매 cycle WARN을 남기면 같은 내용이 분당 60건씩 쌓인다. 첫 실패는 바로
// 기록하고 그 사이 실패는 suppressed_cycles로 다음 WARN에 담는다. 매 cycle 결과는 youtube_collection_discovery_cycles_total과
// /diagnostics/workers가 계속 드러낸다.
const discoveryFailureLogInterval = time.Minute

// globalDiscoveryKey는 runner와 무관한 projection 조회 실패의 연속 실패 키다.
const globalDiscoveryKey = ""

type discoveryFailureStreak struct {
	attrs        []slog.Attr
	since        time.Time
	lastLoggedAt time.Time
	failures     int
	suppressed   int
}

type discoveryFailureDecision struct {
	emit       bool
	since      time.Time
	suppressed int
}

// observe는 실패 한 건을 연속 실패에 더하고 이번 실패를 WARN으로 남길지 정한다.
func (f *discoveryFailureStreak) observe(now time.Time) discoveryFailureDecision {
	f.failures++

	if f.since.IsZero() {
		f.since = now
		f.lastLoggedAt = now

		return discoveryFailureDecision{emit: true, since: now}
	}

	if now.Sub(f.lastLoggedAt) < discoveryFailureLogInterval {
		f.suppressed++

		return discoveryFailureDecision{since: f.since}
	}

	decision := discoveryFailureDecision{emit: true, since: f.since, suppressed: f.suppressed}

	f.lastLoggedAt = now
	f.suppressed = 0

	return decision
}

// reportDiscoveryFailure는 후보 탐색 실패를 job 필드 없이 기록한다. 종료로 scheduler context가 취소된 뒤의 취소는
// 실패가 아니므로 INFO로 남기고 연속 실패에 넣지 않는다. 투영 세대가 바뀐 superseded는 정상 종료라 기록하지 않는다.
func (s *leaseScheduler) reportDiscoveryFailure(ctx context.Context, key string, scope []slog.Attr, err error) {
	if supersededError(err) {
		return
	}

	class := string(collecterr.ClassOf(err))
	attrs := append(append(make([]slog.Attr, 0, len(scope)+6), scope...),
		slog.String("phase", "candidate_load"),
		slog.String("error_code", string(collecterr.CandidateFailed)),
		slog.String("error_class", class),
		slog.String("error_detail", collecterr.SanitizeDetail(collecterr.DiagnosticOf(err).Detail())),
	)

	if canceledByShutdown(ctx, class) {
		s.log(ctx, slog.LevelInfo, "YouTube collection candidate discovery canceled by shutdown", attrs)

		return
	}

	decision := s.noteDiscoveryFailure(key, scope, time.Now())
	if !decision.emit {
		return
	}

	attrs = append(attrs,
		slog.Time("since", decision.since),
		slog.Int("suppressed_cycles", decision.suppressed),
	)
	s.log(ctx, slog.LevelWarn, "YouTube collection candidate discovery failed", attrs)
}

func (s *leaseScheduler) noteDiscoveryFailure(key string, scope []slog.Attr, now time.Time) discoveryFailureDecision {
	s.cycleMu.Lock()
	defer s.cycleMu.Unlock()

	if s.discoveryFailures == nil {
		s.discoveryFailures = make(map[string]*discoveryFailureStreak)
	}

	streak := s.discoveryFailures[key]
	if streak == nil {
		streak = &discoveryFailureStreak{attrs: scope}
		s.discoveryFailures[key] = streak
	}

	return streak.observe(now)
}

// recoverDiscovery는 이번 cycle에서 성공한 키의 연속 실패를 닫고 복구를 INFO 한 건으로 남긴다.
func (s *leaseScheduler) recoverDiscovery(ctx context.Context, keys []string) {
	now := time.Now()
	recovered := s.takeDiscoveryFailures(keys)

	for _, streak := range recovered {
		attrs := append(append(make([]slog.Attr, 0, len(streak.attrs)+3), streak.attrs...),
			slog.Duration("failed_for", now.Sub(streak.since)),
			slog.Int("failed_cycles", streak.failures),
			slog.Int("suppressed_cycles", streak.suppressed),
		)
		s.log(ctx, slog.LevelInfo, "YouTube collection candidate discovery recovered", attrs)
	}
}

func (s *leaseScheduler) takeDiscoveryFailures(keys []string) []*discoveryFailureStreak {
	s.cycleMu.Lock()
	defer s.cycleMu.Unlock()

	if len(s.discoveryFailures) == 0 {
		return nil
	}

	var recovered []*discoveryFailureStreak

	for _, key := range keys {
		if streak, ok := s.discoveryFailures[key]; ok {
			recovered = append(recovered, streak)

			delete(s.discoveryFailures, key)
		}
	}

	return recovered
}

func (s *leaseScheduler) log(ctx context.Context, level slog.Level, message string, attrs []slog.Attr) {
	if s.logger == nil {
		return
	}

	s.logger.LogAttrs(ctx, level, message, attrs...)
}

// canceledByShutdown은 종료나 fatal로 scheduler context가 취소된 뒤 생긴 자체 취소인지 알린다. 안쪽 context만
// 취소된 경우(renew 실패 등)는 바깥 context가 살아 있으므로 실패로 남는다.
func canceledByShutdown(ctx context.Context, class string) bool {
	return class == string(contract.ClassCanceled) && ctx.Err() != nil
}
