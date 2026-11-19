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
	passInterval           = 10 * time.Minute
	recentWindow           = 3 * 24 * time.Hour
	historyWindow          = handlers.MaxBroadcastHistoryDays * 24 * time.Hour
	fullScanInterval       = 24 * time.Hour
	endGrace               = time.Hour
	pageSize               = 200
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
	fullScanning bool
	failed       map[string]struct{}
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
