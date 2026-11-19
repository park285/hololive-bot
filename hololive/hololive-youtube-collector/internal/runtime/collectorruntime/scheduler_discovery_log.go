package collectorruntime

import (
	"context"
	"log/slog"
	"time"

	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
)

const discoveryFailureLogInterval = time.Minute

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

func (s *leaseScheduler) reportDiscoveryFailure(ctx context.Context, key string, scope []slog.Attr, err error) {
	if supersededError(err) {
		return
	}

	class := string(collecterr.ClassOf(err))
	attrs := append(append(make([]slog.Attr, 0, len(scope)+6), scope...),
		slog.String("phase", "candidate_load"),
		slog.String("error_code", string(collecterr.OperationCandidateLoadFailed)),
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

func canceledByShutdown(ctx context.Context, class string) bool {
	return class == string(contract.ClassCanceled) && ctx.Err() != nil
}
