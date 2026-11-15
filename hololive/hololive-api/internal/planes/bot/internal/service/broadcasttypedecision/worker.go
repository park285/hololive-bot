// Package broadcasttypedecision은 규칙과 영상별 검토가 미분류로 남긴 종료 방송의 유형을 Gemini Decisions API에 물어 저장한다.
package broadcasttypedecision

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/park285/shared-go/v2/pkg/retry"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"

	"github.com/kapu/hololive-api/internal/planes/bot/internal/broadcasttype"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/command/handlers/handlercore"
	"github.com/kapu/hololive-api/internal/service/geminidecision"
)

const (
	passInterval     = 10 * time.Minute
	recentWindow     = 3 * 24 * time.Hour
	historyWindow    = handlers.MaxBroadcastHistoryDays * 24 * time.Hour
	fullScanInterval = 24 * time.Hour
	// 종료 시각 확정은 종료 관측 3분 뒤 처음, 결과를 모르면 30분 뒤 다시 player를 확인해 멤버 전용을 기록한다.
	// 그 기록보다 먼저 물으면 멤버 한정 방송을 형식으로 저장하므로 기다린다.
	endGrace = time.Hour
	pageSize = 200
	// 무료 등급 한도(분당 5만 입력 토큰)를 넘지 않도록 요청 사이를 띄운다. 한 요청은 썸네일 포함 약 1천 토큰으로 어림했다.
	requestGap             = 3 * time.Second
	maxDecisionsPerPass    = 100
	maxConsecutiveFailures = 3
)

type store interface {
	candidates(ctx context.Context, since, endedBefore time.Time, cursorAt *time.Time, cursorVideoID string, limit int) ([]candidate, error)
	save(ctx context.Context, d decision) error
}

type judge interface {
	Decide(ctx context.Context, request geminidecision.Request) (geminidecision.Response, error)
}

type thumbnails interface {
	Download(ctx context.Context, entry *handlercore.BroadcastHistoryEntry) ([]byte, string, error)
}

type Worker struct {
	store      store
	judge      judge
	thumbnails thumbnails
	logger     *slog.Logger
	now        func() time.Time
	gap        time.Duration

	lastCall     time.Time
	lastFullScan time.Time
	// fullScanning은 끝나지 않은 전체 훑기 주기를 이어 가는 중이라는 뜻이다. 이어 가는 회차가 failed를 지우면
	// 계속 실패하는 방송에서 회차마다 다시 멈춰 주기가 끝나지 않는다.
	fullScanning bool
	// failed는 이번 전체 훑기 주기에 판정하지 못한 방송이다. 다음 전체 훑기 주기 전까지 다시 묻지 않는다.
	failed map[string]struct{}
}

func New(pool *pgxpool.Pool, apiKey string, logger *slog.Logger) *Worker {
	return &Worker{
		store:      pgStore{pool: pool},
		judge:      geminidecision.New(apiKey),
		thumbnails: handlers.NewYouTubeThumbnailDownloader(nil),
		logger:     logger.With(slog.String("service", "broadcast_type_decision")),
		now:        time.Now,
		gap:        requestGap,
		failed:     map[string]struct{}{},
	}
}

// Start는 ctx가 끝날 때까지 돌아오지 않는다.
func (w *Worker) Start(ctx context.Context) {
	w.logger.Info("Starting broadcast type decision worker", slog.String("model", geminidecision.Model))

	w.runPass(ctx)

	if err := lifecycle.RunTickerLoop(ctx, passInterval, func(ctx context.Context) error {
		w.runPass(ctx)

		return nil
	}); err != nil && ctx.Err() == nil {
		w.logger.Error("Broadcast type decision loop stopped", slog.Any("error", err))
	}
}

func (w *Worker) runPass(ctx context.Context) {
	if err := w.pass(ctx); err != nil && ctx.Err() == nil {
		w.logger.Error("Broadcast type decision pass failed", slog.Any("error", err))
	}
}

func (w *Worker) pass(ctx context.Context) error {
	now := w.now()
	full := w.lastFullScan.IsZero() || now.Sub(w.lastFullScan) >= fullScanInterval

	since := now.Add(-recentWindow)

	if full {
		since = now.Add(-historyWindow)

		if !w.fullScanning {
			clear(w.failed)

			w.fullScanning = true
		}
	}

	finished, err := w.decideSince(ctx, since)
	if full && finished {
		w.lastFullScan = now
		w.fullScanning = false
	}

	return err
}

func (w *Worker) decideSince(ctx context.Context, since time.Time) (bool, error) {
	var (
		cursorAt *time.Time
		cursorID string
		budget   = maxDecisionsPerPass
		failures int
	)

	for {
		page, err := w.store.candidates(ctx, since, w.now().Add(-endGrace), cursorAt, cursorID, pageSize)
		if err != nil {
			return false, err
		}

		for _, c := range page {
			if _, failed := w.failed[c.VideoID]; failed ||
				handlers.ClassifyBroadcastVideo(c.VideoID, c.ChannelID, c.TopicID, c.Title, handlers.BroadcastTypeSignals{}).Type != broadcasttype.Unknown {
				continue
			}

			if budget == 0 {
				return false, nil
			}

			budget--

			err := w.decide(ctx, c)

			switch {
			case err == nil:
				failures = 0
			case errors.Is(err, geminidecision.ErrUnavailable), ctx.Err() != nil:
				return false, err
			case errors.Is(err, handlercore.ErrBroadcastThumbnailNotFound):
				// 삭제·비공개 영상의 썸네일 404는 그 영상만의 사유라 연속 실패에 넣지도, 그 수를 되돌리지도 않는다.
				w.failed[c.VideoID] = struct{}{}

				w.logger.Info("Broadcast type decision skipped missing thumbnail", slog.String("video_id", c.VideoID), slog.Any("error", err))
			default:
				failures++

				w.failed[c.VideoID] = struct{}{}

				w.logger.Warn("Broadcast type decision skipped", slog.String("video_id", c.VideoID), slog.Any("error", err))

				if failures >= maxConsecutiveFailures {
					return false, fmt.Errorf("stop after %d consecutive failures: %w", failures, err)
				}
			}
		}

		if len(page) < pageSize {
			return true, nil
		}

		last := page[len(page)-1]

		cursorAt, cursorID = &last.SortAt, last.VideoID
	}
}

// decide는 썸네일 없이 제목만으로 묻지 않는다. 일시적인 내려받기 실패로 약한 답을 영구히 남기지 않도록, 내려받지 못한 방송은
// 저장하지 않고 다음 전체 훑기에서 다시 묻는다.
func (w *Worker) decide(ctx context.Context, c candidate) error {
	image, mime, err := w.thumbnails.Download(ctx, &handlercore.BroadcastHistoryEntry{VideoID: c.VideoID, ThumbnailURL: c.ThumbnailURL})
	if err != nil {
		return fmt.Errorf("download thumbnail: %w", err)
	}

	request, err := decisionRequest(c, mime, image)
	if err != nil {
		return err
	}

	if !w.pace(ctx) {
		return ctx.Err()
	}

	response, err := w.judge.Decide(ctx, request)
	if errors.Is(err, geminidecision.ErrBlocked) {
		w.logger.Warn("Broadcast type decision input blocked", slog.String("video_id", c.VideoID), slog.Any("error", err))

		return w.store.save(ctx, decision{VideoID: c.VideoID, Title: c.Title, Outcome: outcomeBlocked, Model: geminidecision.Model})
	}

	if err != nil {
		return fmt.Errorf("decide broadcast type: %w", err)
	}

	answer, err := parseAnswer(response)
	if err != nil {
		return err
	}

	w.logger.Info("Broadcast type decided",
		slog.String("video_id", c.VideoID),
		slog.String("broadcast_type", string(answer.Type)),
		slog.Float64("confidence", answer.Confidence),
		slog.Int("input_tokens", int(response.Usage.TotalInputTokens)),
	)

	typ := string(answer.Type)

	return w.store.save(ctx, decision{
		VideoID: c.VideoID, Title: c.Title, Outcome: outcomeDecided, Type: &typ, Confidence: &answer.Confidence, Model: geminidecision.Model,
	})
}

func (w *Worker) pace(ctx context.Context) bool {
	if !retry.Sleep(ctx, time.Until(w.lastCall.Add(w.gap))) {
		return false
	}

	w.lastCall = time.Now()

	return true
}
