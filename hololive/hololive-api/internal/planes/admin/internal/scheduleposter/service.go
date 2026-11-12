package scheduleposter

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"strconv"
	"sync"
	"time"

	"github.com/park285/shared-go/v2/pkg/retry"
	"github.com/park285/shared-go/v2/pkg/runtime/lifecycle"
	"google.golang.org/genai"

	posterstore "github.com/kapu/hololive-shared/pkg/service/scheduleposter"
)

const (
	maxPages = 5
	// FxTwitter 응답은 시간순이 아니고 오래된 리포스트가 섞이므로, 본인 원글로만 수집 범위와 멈춤을 판단한다.
	lookback           = 8 * 24 * time.Hour
	retention          = 60 * 24 * time.Hour
	purgeBatch         = 100
	excerptRunes       = 500
	translateInterval  = time.Minute
	translateBatch     = 5
	initialCollectWait = 30 * time.Second
	initialTranslate   = 15 * time.Second
)

// 실패 사유는 관리자 화면에 그대로 보이는 분류다.
const (
	reasonSourceUnavailable = "source_unavailable"
	reasonTranslationFailed = "translation_failed"
)

var posterTextPattern = regexp.MustCompile(`(?i)週間スケジュール|スケジュール|schedule|予定|weekly`)

type candidateStore interface {
	InsertCandidates(ctx context.Context, candidates []posterstore.Candidate) (int64, error)
	ExpirePending(ctx context.Context, before time.Time) (int64, error)
	Purge(ctx context.Context, before time.Time, limit int) (int64, error)
}

type translationStore interface {
	PendingTranslations(ctx context.Context, limit int) ([]posterstore.PendingTranslation, error)
	RecordTranslation(ctx context.Context, item posterstore.PendingTranslation, image posterstore.Image) (bool, error)
	RecordTranslationFailure(ctx context.Context, item posterstore.PendingTranslation, reason string) (bool, error)
}

type sourceClient interface {
	statuses(ctx context.Context, userID, cursor string) (statusPage, error)
	image(ctx context.Context, rawURL string) (source, error)
}

type posterTranslator interface {
	translateText(ctx context.Context, src source, prompt string) (textTranslation, error)
	renderImage(ctx context.Context, src source, prompt string) (source, error)
}

type Service struct {
	config       Config
	candidates   candidateStore
	translations translationStore
	sources      sourceClient
	translator   posterTranslator
	members      memberSource
	logger       *slog.Logger
	now          func() time.Time
	retry        retry.RetryOptions
}

func New(ctx context.Context, config *Config, store *posterstore.Store, members memberSource, logger *slog.Logger) (*Service, error) {
	if config == nil || store == nil || members == nil || logger == nil {
		return nil, errors.New("schedule poster service requires config, store, members and logger")
	}

	translator, err := newVertexTranslator(ctx, config.Translation)
	if err != nil {
		return nil, err
	}

	return newService(*config, store, store, newFXClient(), translator, members, logger), nil
}

func newService(
	config Config,
	candidates candidateStore,
	translations translationStore,
	sources sourceClient,
	translator posterTranslator,
	members memberSource,
	logger *slog.Logger,
) *Service {
	return &Service{
		config:       config,
		candidates:   candidates,
		translations: translations,
		sources:      sources,
		translator:   translator,
		members:      members,
		logger:       logger.With(slog.String("service", "schedule_poster")),
		now:          time.Now,
		retry:        translationRetryOptions(),
	}
}

// translationRetryOptions는 번역 재시도 예외 계약(docs/current/services/hololive-api.md)의 한도다.
func translationRetryOptions() retry.RetryOptions {
	return retry.RetryOptions{MaxAttempts: 3, BaseDelay: 5 * time.Second, Jitter: 2 * time.Second}
}

// Start는 ctx가 끝날 때까지 돌아오지 않는다.
func (s *Service) Start(ctx context.Context) {
	s.logger.Info("Starting schedule poster service",
		slog.Int("targets", len(s.config.Targets)),
		slog.Duration("poll_interval", s.config.PollInterval()),
		slog.String("model", s.config.Translation.Model),
	)

	var wg sync.WaitGroup

	wg.Go(func() { s.runLoop(ctx, "collect", initialCollectWait, s.config.PollInterval(), s.collect) })
	wg.Go(func() { s.runLoop(ctx, "translate", initialTranslate, translateInterval, s.translatePending) })
	wg.Wait()
	s.logger.Info("Schedule poster service stopped")
}

func (s *Service) runLoop(ctx context.Context, task string, wait, interval time.Duration, fn func(context.Context) error) {
	if !retry.Sleep(ctx, wait) {
		return
	}

	run := func(ctx context.Context) {
		if err := fn(ctx); err != nil && ctx.Err() == nil {
			s.logger.Error("Schedule poster task failed", slog.String("task", task), slog.Any("error", err))
		}
	}

	run(ctx)

	tick := func(ctx context.Context) error {
		run(ctx)

		return nil
	}

	if err := lifecycle.RunTickerLoop(ctx, interval, tick); err != nil && ctx.Err() == nil {
		s.logger.Error("Schedule poster loop stopped", slog.String("task", task), slog.Any("error", err))
	}
}

func (s *Service) collect(ctx context.Context) error {
	now := s.now()

	var errs []error

	for _, target := range s.config.Targets {
		candidates, err := s.collectTarget(ctx, target, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("collect target %s: %w", target.XUserID, err))
		}

		inserted, err := s.candidates.InsertCandidates(ctx, candidates)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		if inserted > 0 {
			s.logger.Info("Schedule poster candidates found",
				slog.String("channel_id", target.ChannelID), slog.Int64("inserted", inserted))
		}
	}

	if _, err := s.candidates.ExpirePending(ctx, now.Add(-lookback)); err != nil {
		errs = append(errs, err)
	}

	if _, err := s.candidates.Purge(ctx, now.Add(-retention), purgeBatch); err != nil {
		errs = append(errs, err)
	}

	return errors.Join(errs...)
}

// collectTarget은 페이지 도중 실패해도 그때까지 찾은 후보를 함께 돌려준다.
func (s *Service) collectTarget(ctx context.Context, target Target, now time.Time) ([]posterstore.Candidate, error) {
	cutoff := now.Add(-lookback)

	var (
		candidates []posterstore.Candidate
		cursor     string
	)

	for range maxPages {
		page, err := s.sources.statuses(ctx, target.XUserID, cursor)
		if err != nil {
			return candidates, err
		}

		originals, recent := 0, 0

		for i := range page.Results {
			item := &page.Results[i]
			if item.Author.ID != target.XUserID || !item.original() {
				continue
			}

			originals++

			postedAt := time.Unix(item.CreatedTimestamp, 0)
			if postedAt.Before(cutoff) {
				continue
			}

			recent++

			if candidate, ok := s.candidateFrom(item, target, postedAt); ok {
				candidates = append(candidates, candidate)
			}
		}

		if page.Cursor.Bottom == "" || len(page.Results) == 0 || (originals > 0 && recent == 0) {
			break
		}

		cursor = page.Cursor.Bottom
	}

	return candidates, nil
}

func (s *Service) candidateFrom(item *status, target Target, postedAt time.Time) (posterstore.Candidate, bool) {
	if !posterTextPattern.MatchString(item.Text) {
		return posterstore.Candidate{}, false
	}

	photo, ok := item.firstPhoto()
	if !ok {
		return posterstore.Candidate{}, false
	}

	if !posterImageURL(photo) {
		s.logger.Warn("Schedule poster photo skipped: unexpected image URL",
			slog.String("post_id", item.ID), slog.String("image_url", photo))

		return posterstore.Candidate{}, false
	}

	id, err := strconv.ParseInt(item.ID, 10, 64)
	if err != nil || id <= 0 {
		return posterstore.Candidate{}, false
	}

	excerpt := []rune(item.Text)
	if len(excerpt) > excerptRunes {
		excerpt = excerpt[:excerptRunes]
	}

	return posterstore.Candidate{
		ID:          id,
		ChannelID:   target.ChannelID,
		XUserID:     target.XUserID,
		PostURL:     "https://x.com/i/status/" + item.ID,
		ImageURL:    photo,
		TextExcerpt: string(excerpt),
		PostedAt:    postedAt,
	}, true
}

func (s *Service) translatePending(ctx context.Context) error {
	pending, err := s.translations.PendingTranslations(ctx, translateBatch)
	if err != nil {
		return fmt.Errorf("load pending translations: %w", err)
	}

	if len(pending) == 0 {
		return nil
	}

	// 대응표 없이 번역하면 이름이 뜻풀이로 바뀌므로, 멤버를 못 읽으면 행을 대기 상태로 두고 다음 tick에 다시 본다.
	members, err := s.members.GetAllMembers(ctx)
	if err != nil {
		return fmt.Errorf("load members for translation glossary: %w", err)
	}

	var errs []error

	for _, item := range pending {
		prompt := buildTextPrompt(item.ChannelID, members, s.config.Translation.Glossary)

		image, reason, err := s.translateOne(ctx, item, prompt)
		if ctx.Err() != nil {
			return errors.Join(append(errs, ctx.Err())...)
		}

		if err != nil {
			s.logger.Warn("Schedule poster translation failed",
				slog.Int64("poster_id", item.ID), slog.String("reason", reason), slog.Any("error", err))

			if _, recordErr := s.translations.RecordTranslationFailure(ctx, item, reason); recordErr != nil {
				errs = append(errs, recordErr)
			}

			continue
		}

		recorded, err := s.translations.RecordTranslation(ctx, item, image)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		s.logger.Info("Schedule poster translated",
			slog.Int64("poster_id", item.ID), slog.Bool("recorded", recorded), slog.Int("bytes", len(image.Data)))
	}

	return errors.Join(errs...)
}

func (s *Service) translateOne(ctx context.Context, item posterstore.PendingTranslation, textPrompt string) (posterstore.Image, string, error) {
	original, sourceErr := s.sources.image(ctx, item.ImageURL)
	if sourceErr != nil {
		return posterstore.Image{}, reasonSourceUnavailable, sourceErr
	}

	var translation textTranslation

	textErr := retry.WithRetry(ctx, s.retryOptions(item.ID, "text"), func(ctx context.Context) error {
		var err error

		translation, err = s.translator.translateText(ctx, original, textPrompt)

		return err
	})
	if textErr != nil {
		return posterstore.Image{}, reasonTranslationFailed, fmt.Errorf("translate poster text: %w", textErr)
	}

	s.logger.Info("Schedule poster text translated",
		slog.Int64("poster_id", item.ID),
		slog.Int("web_search_queries", translation.webSearchQueries),
		slog.String("table", translation.table))

	imagePrompt := buildImagePrompt(translation.table)

	var image posterstore.Image

	imageErr := retry.WithRetry(ctx, s.retryOptions(item.ID, "image"), func(ctx context.Context) error {
		translated, err := s.translator.renderImage(ctx, original, imagePrompt)
		if err != nil {
			return err
		}

		data, err := toJPEG(translated)
		if err != nil {
			return err
		}

		image = posterstore.Image{MIME: "image/jpeg", Data: data}

		return nil
	})
	if imageErr != nil {
		return posterstore.Image{}, reasonTranslationFailed, fmt.Errorf("render translated poster: %w", imageErr)
	}

	return image, "", nil
}

func (s *Service) retryOptions(posterID int64, stage string) retry.RetryOptions {
	options := s.retry

	options.ShouldRetry = retryableTranslationError
	options.OnRetry = func(attempt int, err error, _ time.Duration) {
		s.logger.Warn("Schedule poster translation attempt failed, will retry",
			slog.Int64("poster_id", posterID), slog.String("stage", stage), slog.Int("attempt", attempt), slog.Any("error", err))
	}

	return options
}

// retryableTranslationError는 Vertex가 요청 자체를 거절한 오류(400·401·403·404)를 다시 보내도 같은 결과라 재시도에서 뺀다.
func retryableTranslationError(err error) bool {
	apiErr, ok := errors.AsType[genai.APIError](err)
	if !ok {
		return true
	}

	switch apiErr.Code {
	case http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound:
		return false
	default:
		return true
	}
}
