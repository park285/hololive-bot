package scheduleposter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"

	"github.com/park285/shared-go/v2/pkg/retry"

	posterstore "github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type translationLine struct {
	change int
	row    extractedRow
}

type translationReport struct {
	translated int
	failed     bool
}

func translationLines(rows []posterstore.Row, prior, changes []posterstore.Change) []translationLine {
	slots := posterstore.ApplyChanges(rows, slices.Concat(prior, changes))

	var lines []translationLine

	for i := range changes {
		change := &changes[i]
		if !change.Kind.Applied() || change.Text == "" {
			continue
		}

		for k := range slots {
			if slices.ContainsFunc(slots[k].Steps, func(step posterstore.Step) bool {
				return step.PostID == change.PostID && step.Item == change.Item
			}) {
				lines = append(lines, translationLine{change: i, row: slotLine(&slots[k])})

				break
			}
		}
	}

	return lines
}

// slotLine이 다루는 칸이 added로 만들었거나 되살린 칸이면, 마지막 added 앞의 원문과 배지는 지금 방송의 것이 아니다.
func slotLine(slot *posterstore.Slot) extractedRow {
	row := extractedRow{Status: slot.Status, Text: slot.SourceText, Badges: slot.Badges}
	steps := slot.Steps

	for k := range slices.Backward(steps) {
		if steps[k].Kind == posterstore.ChangeAdded {
			steps = steps[k:]
			row.Text, row.Badges = "", nil

			break
		}
	}

	for i := range steps {
		switch text := steps[i].Text; {
		case text == "":
		case row.Text == "":
			row.Text = text
		default:
			row.Updates = append(row.Updates, text)
		}
	}

	return row
}

// translateChanges가 돌려주는 오류는 저장소 오류와 서비스 실패(vertexServiceFailure)뿐이다. 그 밖의 번역 실패는 한국어 칸을 비운 변경을 그대로 돌려준다.
func (s *Service) translateChanges(
	ctx context.Context, target Target, schedule *posterstore.PublishedSchedule, post *posterstore.Post,
	prior, changes []posterstore.Change, members []*domain.Member,
) ([]posterstore.Change, translationReport, error) {
	lines := translationLines(schedule.Rows, prior, changes)
	if len(lines) == 0 {
		return changes, translationReport{}, nil
	}

	terms, err := s.rows.WeekTerms(ctx, posterstore.PendingRows{ChannelID: target.ChannelID, WeekStart: schedule.WeekStart, WeekEnd: schedule.WeekEnd})
	if err != nil {
		return nil, translationReport{}, fmt.Errorf("load schedule poster week terms: %w", err)
	}

	translated, err := s.translateLines(ctx, target, schedule, lines, changes, terms, members)
	if errors.Is(err, errChangeServiceFailed) {
		return nil, translationReport{}, err
	}

	if err != nil {
		s.logger.Warn("Schedule poster change translation failed",
			slog.Int64("post_id", post.ID), slog.Int64("poster_id", schedule.PosterID), slog.Any("error", err))

		return changes, translationReport{failed: true}, nil
	}

	return translated, translationReport{translated: len(lines)}, nil
}

func (s *Service) translateLines(
	ctx context.Context, target Target, schedule *posterstore.PublishedSchedule, lines []translationLine,
	changes []posterstore.Change, terms []posterstore.Term, members []*domain.Member,
) ([]posterstore.Change, error) {
	owner, ok := posterOwner(target.ChannelID, members)
	if !ok {
		return nil, errors.New("poster owner is not in members")
	}

	zones := make([]string, len(schedule.Rows))
	for i := range schedule.Rows {
		zones[i] = schedule.Rows[i].TZ
	}

	rows := make([]extractedRow, len(lines))
	for i := range lines {
		rows[i] = lines[i].row
	}

	named := nameTableMembers(members)
	prompt := buildTranslatePrompt(rows, posterZone(zones), owner, named, s.config.Translation.Glossary, earlierRenderings(terms))
	checker := newRowChecker(owner, named)

	var filled []posterstore.Change

	err := retry.WithRetry(ctx, s.changeTranslateRetry(schedule.PosterID), func(ctx context.Context) error {
		translated, spent, err := s.rowModel.translateRows(ctx, prompt)
		s.logUsage(schedule.PosterID, stageChangeTranslate, spent)

		if err != nil {
			return err
		}

		if err := validateTranslated(len(lines), translated); err != nil {
			return err
		}

		attempt := fillTranslated(changes, lines, rows, translated, checker)
		if err := posterstore.ValidateChanges(attempt); err != nil {
			return fmt.Errorf("check translated changes: %w", err)
		}

		filled = attempt

		return nil
	})
	if vertexServiceFailure(err) {
		return nil, fmt.Errorf("%w: translate checked schedule poster changes: %w", errChangeServiceFailed, err)
	}

	if err != nil {
		return nil, fmt.Errorf("translate checked schedule poster changes: %w", err)
	}

	return filled, nil
}

func (s *Service) changeTranslateRetry(posterID int64) retry.RetryOptions {
	options := s.retryOptions(posterID, stageChangeTranslate)

	options.ShouldRetry = func(err error) bool { return !vertexUnreachable(err) && retryableTranslationError(err) }

	return options
}

func fillTranslated(
	changes []posterstore.Change, lines []translationLine, rows []extractedRow, translated []translatedRow, checker rowChecker,
) []posterstore.Change {
	filled := make([]posterstore.Change, len(changes))
	copy(filled, changes)

	for i := range lines {
		out, change := &translated[i], &filled[lines[i].change]

		change.TitleKo, change.WithKo, change.PlaceKo, change.TagsKo, change.NoteKo = out.TitleKo, out.WithKo, out.PlaceKo, out.TagsKo, out.NoteKo
		change.Terms, change.DoubtKo = verifiedTerms(&rows[i], out), out.DoubtKo
		change.Warnings = capWarnings(checker.warnings(&rows[i], out))
	}

	return filled
}
