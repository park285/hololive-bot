package scheduleposter

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
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

	posterstore "github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	maxPages           = 5
	lookback           = 8 * 24 * time.Hour
	retention          = 60 * 24 * time.Hour
	purgeBatch         = 100
	excerptRunes       = 500
	translateInterval  = time.Minute
	translateBatch     = 5
	initialCollectWait = 30 * time.Second
	initialTranslate   = 15 * time.Second
)

const (
	reasonSourceUnavailable = "source_unavailable"
	reasonTranslationFailed = "translation_failed"
)

const posterTextExpr = `週間|今週|来週|スケジュール|schedule|weekly`

var posterTextPattern = regexp.MustCompile(`(?i)` + posterTextExpr)

type candidateStore interface {
	InsertCandidates(ctx context.Context, candidates []posterstore.Candidate, autoReview bool) (int64, error)
	InsertCommunityCandidates(ctx context.Context, channelIDs []string, since time.Time, textPattern string, autoReview bool) (int64, error)
	InsertThumbnailCandidates(ctx context.Context, candidates []posterstore.ThumbnailCandidate, autoReview bool) (int64, error)
	ExpirePending(ctx context.Context, before time.Time) (int64, error)
	Purge(ctx context.Context, before time.Time, limit int) (int64, error)
}

type sourceClient interface {
	statuses(ctx context.Context, userID, cursor string) (statusPage, error)
	image(ctx context.Context, rawURL string) (source, error)
}

type imageStore interface {
	Image(ctx context.Context, id int64) (posterstore.Image, bool, error)
	SaveImage(ctx context.Context, id int64, image posterstore.Image) error
}

type memberSource interface {
	GetAllMembers(ctx context.Context) ([]*domain.Member, error)
}

type Service struct {
	config     Config
	candidates candidateStore
	sources    sourceClient
	images     imageStore
	rows       rowStore
	rowModel   rowModel
	reviews    reviewStore
	judge      posterJudge
	changes    changeStore
	ledger     postLedger
	classifier postClassifier
	matcher    postMatcher
	members    memberSource
	logger     *slog.Logger
	now        func() time.Time
	retry      retry.RetryOptions
	rowsLoop   rowsLoopState
	changeLoop changeLoopState
	collectNow chan struct{}
}

func New(ctx context.Context, config *Config, apiKey string, store *posterstore.Store, members memberSource, logger *slog.Logger) (*Service, error) {
	if config == nil || store == nil || members == nil || logger == nil {
		return nil, errors.New("schedule poster service requires config, store, members and logger")
	}

	translator, err := newVertexTranslator(ctx, config.Translation)
	if err != nil {
		return nil, err
	}

	service := newService(*config, store, store, newFXClient(), members, logger)

	service.rows, service.images = store, store
	service.rowModel = translator
	service.changes, service.ledger, service.matcher = store, store, translator

	if apiKey != "" {
		judge, err := newGeminiJudge(ctx, apiKey)
		if err != nil {
			return nil, err
		}

		service.judge = judge
		service.classifier = judge
	}

	return service, nil
}

func newService(
	config Config,
	candidates candidateStore,
	reviews reviewStore,
	sources sourceClient,
	members memberSource,
	logger *slog.Logger,
) *Service {
	return &Service{
		config:     config,
		candidates: candidates,
		reviews:    reviews,
		sources:    sources,
		members:    members,
		logger:     logger.With(slog.String("service", "schedule_poster")),
		now:        time.Now,
		retry:      translationRetryOptions(),
		rowsLoop:   newRowsLoopState(config.Translation.Rows),
		collectNow: make(chan struct{}, 1),
	}
}

func translationRetryOptions() retry.RetryOptions {
	return retry.RetryOptions{MaxAttempts: 10, BaseDelay: 5 * time.Second, MaxDelay: time.Minute, Jitter: 2 * time.Second}
}

func (s *Service) Start(ctx context.Context) {
	s.logger.Info("Starting schedule poster service",
		slog.Int("targets", len(s.config.Targets)),
		slog.Duration("poll_interval", s.config.PollInterval()),
		slog.Bool("auto_review", s.judge != nil),
		slog.String("rows_extract_model", s.config.Translation.Rows.Extract.Model),
		slog.String("rows_extract_thinking_level", string(s.config.Translation.Rows.Extract.ThinkingLevel)),
		slog.String("rows_translate_model", s.config.Translation.Rows.Translate.Model),
		slog.String("rows_translate_thinking_level", string(s.config.Translation.Rows.Translate.ThinkingLevel)),
		slog.Bool("change_detection", s.detectsChanges()),
		slog.Duration("change_poll_interval", s.config.ChangePollInterval()),
		slog.String("rows_match_model", s.config.Translation.Rows.Match.Model),
		slog.String("rows_match_thinking_level", string(s.config.Translation.Rows.Match.ThinkingLevel)),
		slog.Float64("schedule_info_min", s.config.Translation.Rows.Decide.ScheduleInfoMin),
	)

	var wg sync.WaitGroup

	wg.Go(func() { s.runCollectLoop(ctx) })
	wg.Go(func() { s.runLoop(ctx, "rows", initialTranslate, translateInterval, s.extractPendingRows) })

	if s.detectsChanges() {
		wg.Go(func() { s.runLoop(ctx, "changes", initialCollectWait, s.config.ChangePollInterval(), s.detectChanges) })
	}

	wg.Wait()
	s.logger.Info("Schedule poster service stopped")
}

func (s *Service) runLoop(ctx context.Context, task string, wait, interval time.Duration, fn func(context.Context) error) {
	if !retry.Sleep(ctx, wait) {
		return
	}

	s.runTask(ctx, task, fn)

	tick := func(ctx context.Context) error {
		s.runTask(ctx, task, fn)

		return nil
	}

	if err := lifecycle.RunTickerLoop(ctx, interval, tick); err != nil && ctx.Err() == nil {
		s.logger.Error("Schedule poster loop stopped", slog.String("task", task), slog.Any("error", err))
	}
}

func (s *Service) runTask(ctx context.Context, task string, fn func(context.Context) error) {
	if err := fn(ctx); err != nil && ctx.Err() == nil {
		s.logger.Error("Schedule poster task failed", slog.String("task", task), slog.Any("error", err))
	}
}

func (s *Service) collect(ctx context.Context) error {
	now := s.now()
	autoReview := s.judge != nil

	var errs []error

	var communityChannels []string

	for _, target := range s.config.Targets {
		if target.YouTubeCommunity {
			communityChannels = append(communityChannels, target.ChannelID)
		}

		if target.XUserID == "" {
			continue
		}

		candidates, err := s.collectTarget(ctx, target, now)
		if err != nil {
			errs = append(errs, fmt.Errorf("collect target %s: %w", target.XUserID, err))
		}

		inserted, err := s.candidates.InsertCandidates(ctx, candidates, autoReview)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		if inserted > 0 {
			s.logger.Info("Schedule poster candidates found",
				slog.String("channel_id", target.ChannelID), slog.Int64("inserted", inserted))
		}
	}

	if err := s.collectCommunity(ctx, communityChannels, now, autoReview); err != nil {
		errs = append(errs, err)
	}

	if err := s.collectThumbnails(ctx, now, autoReview); err != nil {
		errs = append(errs, err)
	}

	if err := s.handOverUnreviewed(ctx, now, autoReview); err != nil {
		errs = append(errs, err)
	}

	if autoReview {
		if err := s.autoReview(ctx); err != nil {
			errs = append(errs, err)
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

func (s *Service) collectCommunity(ctx context.Context, channelIDs []string, now time.Time, autoReview bool) error {
	pattern := posterTextExpr

	if autoReview {
		pattern = ""
	}

	inserted, err := s.candidates.InsertCommunityCandidates(ctx, channelIDs, now.Add(-lookback), pattern, autoReview)
	if err != nil {
		return fmt.Errorf("collect community schedule posters: %w", err)
	}

	if inserted > 0 {
		s.logger.Info("Schedule poster community candidates found", slog.Int64("inserted", inserted))
	}

	return nil
}

func (s *Service) handOverUnreviewed(ctx context.Context, now time.Time, autoReview bool) error {
	before, reason := now, posterstore.ReviewReasonAutoReviewOff

	if autoReview {
		before, reason = now.Add(-handOverAfter), posterstore.ReviewReasonStale
	}

	handed, err := s.reviews.HandOverUnreviewed(ctx, before, reason)
	if err != nil {
		return fmt.Errorf("hand over unreviewed schedule posters: %w", err)
	}

	if handed > 0 {
		s.logger.Warn("Schedule poster unreviewed candidates handed over", slog.Int64("handed_over", handed), slog.Bool("auto_review", autoReview))
	}

	return nil
}

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

	if host, ok := httpsHost(photo); !ok || host != imageHost {
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

func (s *Service) collectThumbnails(ctx context.Context, now time.Time, autoReview bool) error {
	var (
		thumbnails []posterstore.ThumbnailCandidate
		errs       []error
	)

	for _, target := range s.config.Targets {
		for _, videoID := range target.YouTubeThumbnailVideoIDs {
			thumbnail, err := s.sources.image(ctx, "https://"+thumbnailHost+"/vi/"+videoID+"/maxresdefault.jpg")
			if err != nil {
				errs = append(errs, fmt.Errorf("collect thumbnail %s: %w", videoID, err))

				continue
			}

			thumbnails = append(thumbnails, posterstore.ThumbnailCandidate{
				ChannelID: target.ChannelID, VideoID: videoID, Image: posterstore.Image(thumbnail), PostedAt: now,
			})
		}
	}

	inserted, err := s.candidates.InsertThumbnailCandidates(ctx, thumbnails, autoReview)
	if err != nil {
		errs = append(errs, err)
	} else if inserted > 0 {
		s.logger.Info("Schedule poster thumbnail candidates found", slog.Int64("inserted", inserted))
	}

	return errors.Join(errs...)
}

func imageSHA256(data []byte) string {
	sum := sha256.Sum256(data)

	return hex.EncodeToString(sum[:])
}

var (
	errImageUnavailable = errors.New("poster image unavailable")
	errImageChanged     = fmt.Errorf("%w: it changed after it was collected", errImageUnavailable)
)

func (s *Service) posterImage(ctx context.Context, id int64, imageURL, collectedSHA256 string) (source, error) {
	stored, found, err := s.images.Image(ctx, id)
	if err != nil {
		return source{}, fmt.Errorf("load stored poster image: %w", err)
	}

	if found {
		return source(stored), nil
	}

	fetched, err := s.sources.image(ctx, imageURL)
	if err != nil {
		return source{}, fmt.Errorf("%w: %w", errImageUnavailable, err)
	}

	if collectedSHA256 != "" && imageSHA256(fetched.Data) != collectedSHA256 {
		return source{}, errImageChanged
	}

	if err := s.images.SaveImage(ctx, id, posterstore.Image(fetched)); err != nil {
		return source{}, fmt.Errorf("save poster image: %w", err)
	}

	return fetched, nil
}

func (s *Service) logUsage(posterID int64, stage string, spent usage) {
	if spent == (usage{}) {
		return
	}

	s.logger.Info("Schedule poster model usage",
		slog.Int64("poster_id", posterID),
		slog.String("stage", stage),
		slog.String("model", spent.model),
		slog.Int64("prompt_tokens", int64(spent.prompt)),
		slog.Int64("tool_use_prompt_tokens", int64(spent.toolUsePrompt)),
		slog.Int64("thoughts_tokens", int64(spent.thoughts)),
		slog.Int64("output_tokens", int64(spent.output)))
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
