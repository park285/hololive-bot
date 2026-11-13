package scheduleposter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	posterstore "github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

// 자동 거절은 관리자가 되돌릴 수 없으므로 is_schedule 하나만 보고 판단한다.
const (
	reviewBatch = 20
	rejectBelow = 0.5
	confirmFrom = 0.8
	// 들어온 뒤 handOverAfter가 지나도록 판정하지 못한 후보는 실패 이유와 상관없이 관리자 검토로 넘긴다(collect).
	// 장애가 길어져도 후보가 알림 없이 만료되지 않게 하는 상한이다.
	handOverAfter     = 6 * time.Hour
	stageDecision     = "decision"
	stagePeriod       = "period"
	outcomeConfirmed  = "confirmed"
	outcomeRejected   = "rejected"
	outcomeDuplicate  = "duplicate"
	outcomeRequested  = "review_requested"
	outcomeSuperseded = "superseded"
)

type reviewStore interface {
	UnreviewedCandidates(ctx context.Context, limit int) ([]posterstore.ReviewCandidate, error)
	HandOverUnreviewed(ctx context.Context, createdBefore time.Time, reason posterstore.ReviewRequestReason) (int64, error)
	RequestReview(ctx context.Context, id int64, reason posterstore.ReviewRequestReason) (bool, error)
	AutoConfirm(ctx context.Context, id, expectedRevision int64, weekStart, weekEnd time.Time) (posterstore.WeekCoverage, int64, error)
	AutoReject(ctx context.Context, id, expectedRevision int64) (int64, error)
}

type posterJudge interface {
	judge(ctx context.Context, src source) (posterScores, usage, error)
	readPeriod(ctx context.Context, src source) (printedPeriod, usage, error)
}

// autoReview는 한 후보의 실패로 뒤 후보를 막지 않지만, 어느 후보도 판정할 수 없는 실패(errReviewUnavailable)면 배치를 멈춘다.
func (s *Service) autoReview(ctx context.Context) error {
	candidates, err := s.reviews.UnreviewedCandidates(ctx, reviewBatch)
	if err != nil {
		return fmt.Errorf("load unreviewed schedule poster candidates: %w", err)
	}

	var errs []error

	for _, item := range candidates {
		err := s.reviewOne(ctx, item)
		if err == nil {
			continue
		}

		errs = append(errs, fmt.Errorf("auto review schedule poster %d: %w", item.ID, err))

		if errors.Is(err, errReviewUnavailable) || ctx.Err() != nil {
			break
		}
	}

	return errors.Join(errs...)
}

func (s *Service) reviewOne(ctx context.Context, item posterstore.ReviewCandidate) error {
	original, err := s.sources.image(ctx, item.ImageURL)
	if err != nil {
		return s.unjudged(ctx, item, posterScores{}, posterstore.ReviewReasonUnjudgeable, fmt.Errorf("fetch poster image: %w", err))
	}

	if item.ImageSHA256 != "" && imageSHA256(original.Data) != item.ImageSHA256 {
		return s.requestReview(ctx, item, posterScores{}, posterstore.ReviewReasonImageChanged, errors.New("poster image changed after it was collected"))
	}

	scores, spent, err := s.judge.judge(ctx, original)
	s.logUsage(item.ID, stageDecision, spent)

	if err != nil {
		return s.unjudged(ctx, item, scores, posterstore.ReviewReasonUnjudgeable, err)
	}

	switch {
	case scores.schedule < rejectBelow:
		return s.reject(ctx, item, scores)
	case scores.lowest() < confirmFrom:
		return s.requestReview(ctx, item, scores, posterstore.ReviewReasonUncertain, nil)
	}

	period, spent, err := s.judge.readPeriod(ctx, original)
	s.logUsage(item.ID, stagePeriod, spent)

	if err != nil {
		return s.unjudged(ctx, item, scores, posterstore.ReviewReasonPeriodUnread, err)
	}

	start, end, err := period.week(postedDate(item.PostedAt))
	if err != nil {
		return s.requestReview(ctx, item, scores, posterstore.ReviewReasonPeriodUnread, fmt.Errorf("printed period %+v: %w", period, err))
	}

	return s.confirm(ctx, item, scores, start, end)
}

// unjudged는 후보 때문에 판정할 수 없는 경우(errUnjudgeable)만 관리자 검토로 넘기고, 그 밖의 실패는 오류로 돌려
// 다음 수집 주기에 다시 판정하게 둔다. 계속 실패하는 후보는 handOverAfter가 넘긴다.
func (s *Service) unjudged(
	ctx context.Context, item posterstore.ReviewCandidate, scores posterScores, reason posterstore.ReviewRequestReason, err error,
) error {
	if errors.Is(err, errUnjudgeable) {
		return s.requestReview(ctx, item, scores, reason, err)
	}

	return err
}

func (s *Service) confirm(ctx context.Context, item posterstore.ReviewCandidate, scores posterScores, start, end time.Time) error {
	coverage, _, err := s.reviews.AutoConfirm(ctx, item.ID, item.Revision, start, end)
	week := []slog.Attr{slog.String("week_start", start.Format(time.DateOnly)), slog.String("week_end", end.Format(time.DateOnly))}

	switch {
	case errors.Is(err, posterstore.ErrInvalidWeek):
		return s.requestReview(ctx, item, scores, posterstore.ReviewReasonWeekRejected, err)
	case err == nil && coverage == posterstore.WeekOverlapping:
		return s.requestReview(ctx, item, scores, posterstore.ReviewReasonWeekOverlapping,
			fmt.Errorf("a confirmed poster overlaps %s..%s", week[0].Value, week[1].Value))
	case coverage == posterstore.WeekSame:
		return s.settle(ctx, item, scores, outcomeDuplicate, "expire duplicate schedule poster", err, week...)
	default:
		return s.settle(ctx, item, scores, outcomeConfirmed, "confirm auto reviewed schedule poster", err, week...)
	}
}

func (s *Service) reject(ctx context.Context, item posterstore.ReviewCandidate, scores posterScores) error {
	_, err := s.reviews.AutoReject(ctx, item.ID, item.Revision)

	return s.settle(ctx, item, scores, outcomeRejected, "reject auto reviewed schedule poster", err)
}

// settle은 그 사이 관리자가 처리했거나 앞 후보의 확정이 닫은 후보(err가 충돌·상태·없음)를 superseded로 기록한다.
func (s *Service) settle(
	ctx context.Context, item posterstore.ReviewCandidate, scores posterScores, outcome, action string, err error, attrs ...slog.Attr,
) error {
	switch {
	case errors.Is(err, posterstore.ErrRevisionConflict), errors.Is(err, posterstore.ErrInvalidState), errors.Is(err, posterstore.ErrNotFound):
		s.logReview(ctx, item.ID, outcomeSuperseded, scores, err)

		return nil
	case err != nil:
		return fmt.Errorf("%s: %w", action, err)
	}

	s.logReview(ctx, item.ID, outcome, scores, nil, attrs...)

	return nil
}

func (s *Service) requestReview(
	ctx context.Context, item posterstore.ReviewCandidate, scores posterScores, reason posterstore.ReviewRequestReason, cause error,
) error {
	requested, err := s.reviews.RequestReview(ctx, item.ID, reason)
	if err != nil {
		return fmt.Errorf("request schedule poster review: %w", err)
	}

	outcome := outcomeRequested

	if !requested {
		outcome = outcomeSuperseded
	}

	s.logReview(ctx, item.ID, outcome, scores, cause, slog.String("request_reason", string(reason)))

	return nil
}

func (s *Service) logReview(ctx context.Context, posterID int64, outcome string, scores posterScores, reason error, attrs ...slog.Attr) {
	attrs = append(attrs,
		slog.Int64("poster_id", posterID),
		slog.String("outcome", outcome),
		slog.Float64(questionSchedule, scores.schedule),
		slog.Float64(questionOneWeek, scores.oneWeek),
		slog.Float64(questionOneStreamer, scores.oneStreamer))

	if reason != nil {
		attrs = append(attrs, slog.Any("reason", reason))
	}

	s.logger.LogAttrs(ctx, slog.LevelInfo, "Schedule poster auto reviewed", attrs...)
}
