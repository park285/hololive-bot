package scheduleposter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"slices"
	"sync"
	"time"

	"github.com/park285/shared-go/v2/pkg/retry"

	"github.com/kapu/hololive-shared/pkg/domain"
	posterstore "github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

const (
	stageRowsExtract   = "rows_extract"
	stageRowsTranslate = "rows_translate"
	rowsStageBackoff   = 150 * time.Second
)

// extractBudget을 넘긴 추출 실행은 끊기고, 그때까지 성공한 실행들로 정한다.
func (c RowsConfig) extractBudget() time.Duration {
	return 2*c.Extract.timeout() + rowsStageBackoff
}

func (c RowsConfig) translateBudget() time.Duration {
	return 2*c.Translate.timeout() + rowsStageBackoff
}

const (
	reasonExtractionFailed = "extraction_failed"
	reasonNoRows           = "no_rows"
	reasonUnsupportedZone  = "unsupported_zone"
)

type rowStore interface {
	PendingRows(ctx context.Context, today time.Time, limit int) ([]posterstore.PendingRows, error)
	WeekTerms(ctx context.Context, item posterstore.PendingRows) ([]posterstore.Term, error)
	RecordRows(ctx context.Context, item posterstore.PendingRows, rows []posterstore.Row, dayShift int) (bool, error)
	RecordRowsFailure(ctx context.Context, item posterstore.PendingRows, reason string) (bool, error)
}

type rowsKey struct {
	posterID int64
	revision int64
}

// rowsLoopState는 행 추출 goroutine만 만지므로 잠그지 않는다.
type rowsLoopState struct {
	budget          time.Duration
	translateBudget time.Duration
	done            map[rowsKey]rowsResult
}

func newRowsLoopState(config RowsConfig) rowsLoopState {
	return rowsLoopState{
		budget:          config.extractBudget() + config.translateBudget(),
		translateBudget: config.translateBudget(),
		done:            map[rowsKey]rowsResult{},
	}
}

type rowsResult struct {
	rows     []posterstore.Row
	dayShift int
	reason   string
}

func pendingRowsKey(item posterstore.PendingRows) rowsKey {
	return rowsKey{posterID: item.ID, revision: item.RowsRevision}
}

func (s *Service) extractPendingRows(ctx context.Context) error {
	pending, err := s.rows.PendingRows(ctx, postedDate(s.now()), translateBatch)
	if err != nil {
		return fmt.Errorf("load pending schedule poster rows: %w", err)
	}

	maps.DeleteFunc(s.rowsLoop.done, func(key rowsKey, _ rowsResult) bool {
		return !slices.ContainsFunc(pending, func(item posterstore.PendingRows) bool { return pendingRowsKey(item) == key })
	})

	var members, named []*domain.Member

	if slices.ContainsFunc(pending, func(item posterstore.PendingRows) bool {
		_, done := s.rowsLoop.done[pendingRowsKey(item)]

		return !done
	}) {
		// 이름표 없이 번역하면 이름이 뜻풀이로 바뀌므로, 멤버를 못 읽으면 대기 상태로 두고 다음 tick에 다시 본다.
		members, err = s.members.GetAllMembers(ctx)
		if err != nil {
			return fmt.Errorf("load members for schedule poster rows: %w", err)
		}

		named = nameTableMembers(members)
	}

	var errs []error

	for _, item := range pending {
		key := pendingRowsKey(item)

		result, done := s.rowsLoop.done[key]
		if !done {
			posterCtx, cancel := context.WithTimeout(ctx, s.rowsLoop.budget)
			rows, dayShift, reason, err := s.extractOne(posterCtx, item, members, named)

			cancel()

			if ctx.Err() != nil {
				return errors.Join(append(errs, ctx.Err())...)
			}

			if err != nil && reason == "" {
				rowsTotal.WithLabelValues(rowsResultStoreError).Inc()

				errs = append(errs, err)

				continue
			}

			if err != nil {
				s.logger.Warn("Schedule poster rows failed",
					slog.Int64("poster_id", item.ID), slog.String("reason", reason), slog.Any("error", err))
			}

			result = rowsResult{rows: rows, dayShift: dayShift, reason: reason}
			s.rowsLoop.done[key] = result
		}

		if err := s.recordRows(ctx, item, result); err != nil {
			errs = append(errs, err)

			continue
		}

		delete(s.rowsLoop.done, key)
	}

	return errors.Join(errs...)
}

func (s *Service) recordRows(ctx context.Context, item posterstore.PendingRows, result rowsResult) error {
	var (
		recorded bool
		err      error
	)

	if result.reason != "" {
		recorded, err = s.rows.RecordRowsFailure(ctx, item, result.reason)
	} else {
		recorded, err = s.rows.RecordRows(ctx, item, result.rows, result.dayShift)
	}

	if err != nil {
		rowsTotal.WithLabelValues(rowsResultStoreError).Inc()

		return fmt.Errorf("record schedule poster rows: poster %d: %w", item.ID, err)
	}

	switch {
	case !recorded:
		rowsTotal.WithLabelValues(rowsResultStale).Inc()
	case result.reason != "":
		rowsTotal.WithLabelValues(rowsResultFailed).Inc()
	default:
		rowsTotal.WithLabelValues(rowsResultSucceeded).Inc()
	}

	if result.reason == "" {
		s.logRows(item.ID, recorded, result.rows)
	}

	return nil
}

// extractOne의 reason이 비어 있는 오류는 저장소 오류라 기록하지 않고 대기 상태로 둔다.
// 추출 경고와 번역은 포스터의 현지 날짜·시각으로 하고, 저장할 행만 JST로 바꾼다. 검증 하니스와 같은 입력을 쓰기 위해서다.
func (s *Service) extractOne(
	ctx context.Context, item posterstore.PendingRows, members, named []*domain.Member,
) ([]posterstore.Row, int, string, error) {
	original, err := s.sources.image(ctx, item.ImageURL)
	if err != nil {
		return nil, 0, reasonSourceUnavailable, err
	}

	if item.ImageSHA256 != "" && imageSHA256(original.Data) != item.ImageSHA256 {
		return nil, 0, reasonSourceUnavailable, errors.New("poster image changed after it was collected")
	}

	owner, ok := posterOwner(item.ChannelID, members)
	if !ok {
		return nil, 0, reasonTranslationFailed, errors.New("poster owner is not in members")
	}

	terms, err := s.rows.WeekTerms(ctx, item)
	if err != nil {
		return nil, 0, "", fmt.Errorf("load schedule poster week terms: %w", err)
	}

	extracted, base, err := s.extractRows(ctx, item, original, buildExtractPrompt(item.WeekStart, item.WeekEnd, named))
	if errors.Is(err, errUnsupportedZone) {
		return nil, 0, reasonUnsupportedZone, err
	}

	if err != nil {
		return nil, 0, reasonExtractionFailed, err
	}

	if len(extracted) == 0 {
		return nil, 0, reasonNoRows, errors.New("poster has no schedule rows")
	}

	zones := make([]string, len(extracted))
	for i := range extracted {
		zones[i] = optional(extracted[i].TZ)
	}

	prompt := buildTranslatePrompt(extracted, posterZone(zones), owner, named, s.config.Translation.Glossary, earlierRenderings(terms))

	rows, err := s.translateRows(ctx, item, extracted, base, prompt, newRowChecker(owner, named))
	if err != nil {
		return nil, 0, reasonTranslationFailed, err
	}

	rows, posterShift, err := toJSTRows(rows)
	if err != nil {
		return nil, 0, reasonExtractionFailed, err
	}

	return rows, rowsDayShift(rows, item.WeekStart, item.WeekEnd, posterShift), "", nil
}

func (s *Service) extractRows(
	ctx context.Context, item posterstore.PendingRows, original source, prompt string,
) ([]extractedRow, []posterstore.Row, error) {
	if deadline, ok := ctx.Deadline(); ok {
		var cancel context.CancelFunc

		ctx, cancel = context.WithDeadline(ctx, deadline.Add(-s.rowsLoop.translateBudget))
		defer cancel()
	}

	runs := make([][]extractedRow, extractRuns)
	errs := make([]error, extractRuns)

	var wg sync.WaitGroup

	for run := range extractRuns {
		wg.Go(func() { runs[run], errs[run] = s.extractOnce(ctx, item, original, prompt) })
	}

	wg.Wait()

	var succeeded [][]extractedRow

	for run := range extractRuns {
		if errs[run] == nil {
			succeeded = append(succeeded, runs[run])
		}
	}

	if len(succeeded) < extractQuorum {
		return nil, nil, fmt.Errorf("extract checked poster rows: %d of %d runs succeeded: %w", len(succeeded), extractRuns, errors.Join(errs...))
	}

	rows, err := voteExtracted(succeeded)
	if err != nil {
		return nil, nil, fmt.Errorf("vote extracted poster rows: %w", err)
	}

	base, err := validateExtracted(rows)
	if err != nil {
		return nil, nil, fmt.Errorf("check voted poster rows: %w", err)
	}

	return rows, base, nil
}

func (s *Service) extractOnce(ctx context.Context, item posterstore.PendingRows, original source, prompt string) ([]extractedRow, error) {
	var rows []extractedRow

	options := s.retryOptions(item.ID, stageRowsExtract)
	// 표에 없는 시간대는 포스터에 인쇄된 값이라 다시 뽑아도 같게 나온다.
	options.ShouldRetry = func(err error) bool {
		return !errors.Is(err, errUnsupportedZone) && retryableTranslationError(err)
	}

	err := retry.WithRetry(ctx, options, func(ctx context.Context) error {
		extracted, spent, err := s.rowModel.extractRows(ctx, original, prompt)
		s.logUsage(item.ID, stageRowsExtract, spent)

		if err != nil {
			return err
		}

		if _, err := validateExtracted(extracted); err != nil {
			return err
		}

		rows = extracted

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("extract poster rows run: %w", err)
	}

	return rows, nil
}

func (s *Service) translateRows(
	ctx context.Context, item posterstore.PendingRows, extracted []extractedRow, base []posterstore.Row, prompt string, checker rowChecker,
) ([]posterstore.Row, error) {
	weekStart, weekEnd := item.WeekStart.Format(time.DateOnly), item.WeekEnd.Format(time.DateOnly)

	var rows []posterstore.Row

	err := retry.WithRetry(ctx, s.retryOptions(item.ID, stageRowsTranslate), func(ctx context.Context) error {
		translated, spent, err := s.rowModel.translateRows(ctx, prompt)
		s.logUsage(item.ID, stageRowsTranslate, spent)

		if err != nil {
			return err
		}

		if err := validateTranslated(len(extracted), translated); err != nil {
			return err
		}

		combined := slices.Clone(base)

		for i := range combined {
			out := &translated[i]

			combined[i].TitleKo, combined[i].WithKo, combined[i].PlaceKo = out.TitleKo, out.WithKo, out.PlaceKo
			combined[i].TagsKo, combined[i].NoteKo, combined[i].DoubtKo = out.TagsKo, out.NoteKo, out.DoubtKo
			combined[i].Terms = verifiedTerms(&extracted[i], out)
		}

		// 경고는 모델 출력 길이에 비례해 커지므로, 저장 상한을 넘는 출력은 경고를 만들기 전에 거부한다.
		if err := posterstore.ValidateRows(combined); err != nil {
			return fmt.Errorf("check translated rows: %w", err)
		}

		for i := range combined {
			src := &extracted[i]

			combined[i].Warnings = capWarnings(append(extractWarnings(src, weekStart, weekEnd), checker.warnings(src, &translated[i])...))
		}

		if err := posterstore.ValidateRows(combined); err != nil {
			return fmt.Errorf("check translated row warnings: %w", err)
		}

		rows = combined

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("translate checked poster rows: %w", err)
	}

	return rows, nil
}

func (s *Service) logRows(posterID int64, recorded bool, rows []posterstore.Row) {
	warnings := make([]int, len(rows))
	doubts, terms := 0, 0

	for i := range rows {
		warnings[i] = len(rows[i].Warnings)

		terms += len(rows[i].Terms)

		if rows[i].DoubtKo != "" {
			doubts++
		}
	}

	s.logger.Info("Schedule poster rows recorded",
		slog.Int64("poster_id", posterID),
		slog.Bool("recorded", recorded),
		slog.Int("rows", len(rows)),
		slog.Any("row_warnings", warnings),
		slog.Int("doubt_rows", doubts),
		slog.Int("terms", terms))
}
