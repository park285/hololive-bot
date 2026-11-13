package scheduleposter

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"regexp"
	"slices"
	"strconv"
	"strings"
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
	lookback          = 8 * 24 * time.Hour
	retention         = 60 * 24 * time.Hour
	purgeBatch        = 100
	excerptRunes      = 500
	translateInterval = time.Minute
	translateBatch    = 5
	// 행 하나의 상한은 판독·번역표·그리기 재시도를 모두 합친 시간이다. 넘으면 그 행은 실패로 기록되고 다음 행으로 넘어간다.
	// 행은 차례로 처리하므로 한 행이 이 시간만큼 같은 tick의 뒤 행을 기다리게 할 수 있다.
	translationRowBudget = 3 * time.Hour
	initialCollectWait   = 30 * time.Second
	initialTranslate     = 15 * time.Second
)

// 실패 사유는 관리자 화면에 그대로 보이는 분류다.
const (
	reasonSourceUnavailable = "source_unavailable"
	reasonTranslationFailed = "translation_failed"
)

// posterTextExpr는 X 글(Go regexp)과 커뮤니티 글(PostgreSQL ~*)에 같이 쓰므로 두 문법이 같게 읽는 형태만 쓴다.
// 予定만으로는 매일 올리는 「今日の配信予定」 공지까지 후보가 되므로 주 단위 문구만 받는다.
const posterTextExpr = `週間|今週|来週|スケジュール|schedule|weekly`

var posterTextPattern = regexp.MustCompile(`(?i)` + posterTextExpr)

type candidateStore interface {
	InsertCandidates(ctx context.Context, candidates []posterstore.Candidate, autoReview bool) (int64, error)
	InsertCommunityCandidates(ctx context.Context, channelIDs []string, since time.Time, textPattern string, autoReview bool) (int64, error)
	InsertThumbnailCandidates(ctx context.Context, candidates []posterstore.ThumbnailCandidate, autoReview bool) (int64, error)
	ExpirePending(ctx context.Context, before time.Time) (int64, error)
	Purge(ctx context.Context, before time.Time, limit int) (int64, error)
}

type translationStore interface {
	PendingTranslations(ctx context.Context, limit int) ([]posterstore.PendingTranslation, error)
	RecordTranslation(ctx context.Context, item posterstore.PendingTranslation, image posterstore.Image) (bool, error)
	RecordTranslationFailure(ctx context.Context, item posterstore.PendingTranslation, reason string, inputs posterstore.TranslationInputs) (bool, error)
}

type sourceClient interface {
	statuses(ctx context.Context, userID, cursor string) (statusPage, error)
	image(ctx context.Context, rawURL string) (source, error)
}

type posterTranslator interface {
	translateText(ctx context.Context, src source, prompt string) (textTranslation, usage, error)
	renderImage(ctx context.Context, src source, prompt string) (source, usage, error)
	transcribe(ctx context.Context, src source) ([]string, usage, error)
}

// 단계 이름은 재시도 로그와 사용량 로그의 stage 값이다.
const (
	stageOriginal = "original"
	stageText     = "text"
	stageImage    = "image"
	stageCheck    = "check"
)

// 검사에 걸린 결과가 maxRenderStalls번 연달아 가장 나은 번역본보다 문제가 적지 않으면, 이어 고쳐도 같은 문제를 되풀이하므로 그리기를 멈춘다.
const maxRenderStalls = 2

// errCheckUnavailable은 번역본을 판독하지 못한 경우다. 검사 없이 다시 그려도 통과를 판단할 수 없어 그리기를 재시도하지 않는다.
var errCheckUnavailable = errors.New("translated poster check unavailable")

var errRenderStalled = fmt.Errorf("render check found no fewer problems than the best draft %d times in a row", maxRenderStalls)

type Service struct {
	config       Config
	candidates   candidateStore
	translations translationStore
	sources      sourceClient
	translator   posterTranslator
	reviews      reviewStore
	judge        posterJudge
	members      memberSource
	logger       *slog.Logger
	now          func() time.Time
	retry        retry.RetryOptions
	rowBudget    time.Duration
}

// New의 apiKey가 비어 있으면 후보 자동 검토를 끈다(LoadAPIKey).
func New(ctx context.Context, config *Config, apiKey string, store *posterstore.Store, members memberSource, logger *slog.Logger) (*Service, error) {
	if config == nil || store == nil || members == nil || logger == nil {
		return nil, errors.New("schedule poster service requires config, store, members and logger")
	}

	translator, err := newVertexTranslator(ctx, config.Translation)
	if err != nil {
		return nil, err
	}

	service := newService(*config, store, store, store, newFXClient(), translator, members, logger)

	if apiKey != "" {
		judge, err := newGeminiJudge(ctx, apiKey)
		if err != nil {
			return nil, err
		}

		service.judge = judge
	}

	return service, nil
}

func newService(
	config Config,
	candidates candidateStore,
	translations translationStore,
	reviews reviewStore,
	sources sourceClient,
	translator posterTranslator,
	members memberSource,
	logger *slog.Logger,
) *Service {
	return &Service{
		config:       config,
		candidates:   candidates,
		translations: translations,
		reviews:      reviews,
		sources:      sources,
		translator:   translator,
		members:      members,
		logger:       logger.With(slog.String("service", "schedule_poster")),
		now:          time.Now,
		retry:        translationRetryOptions(),
		rowBudget:    translationRowBudget,
	}
}

// translationRetryOptions는 번역 재시도 예외 계약(docs/current/services/hololive-api.md)의 한도다.
func translationRetryOptions() retry.RetryOptions {
	return retry.RetryOptions{MaxAttempts: 10, BaseDelay: 5 * time.Second, MaxDelay: time.Minute, Jitter: 2 * time.Second}
}

// Start는 ctx가 끝날 때까지 돌아오지 않는다.
func (s *Service) Start(ctx context.Context) {
	s.logger.Info("Starting schedule poster service",
		slog.Int("targets", len(s.config.Targets)),
		slog.Duration("poll_interval", s.config.PollInterval()),
		slog.String("model", s.config.Translation.Model),
		slog.Bool("auto_review", s.judge != nil),
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

// collectCommunity는 자동 검토가 켜져 있으면 문구 패턴이 놓친 일정표(영어 글 등)까지 판정하도록 커뮤니티의 이미지 글을 모두 후보로 넣는다.
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

// handOverUnreviewed는 자동 검토가 꺼졌으면 켜져 있던 동안 들어와 기다리던 후보가 목록·알림 없이 만료되지 않도록 바로 넘긴다.
func (s *Service) handOverUnreviewed(ctx context.Context, now time.Time, autoReview bool) error {
	before := now

	if autoReview {
		before = now.Add(-handOverAfter)
	}

	handed, err := s.reviews.HandOverUnreviewed(ctx, before)
	if err != nil {
		return fmt.Errorf("hand over unreviewed schedule posters: %w", err)
	}

	if handed > 0 {
		s.logger.Warn("Schedule poster unreviewed candidates handed over", slog.Int64("handed_over", handed), slog.Bool("auto_review", autoReview))
	}

	return nil
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

	// Iris Console과 DB 제약은 X 출처 행의 원본을 이 호스트로만 받으므로, 다른 사진 하나가 수집 묶음 전체를 실패시키지 않게 미리 뺀다.
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

// collectThumbnails는 받지 못한 영상만 건너뛰고 나머지 썸네일은 후보로 넣는다. 번역은 여기서 남긴 해시와 같은 그림만 쓴다.
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
				ChannelID: target.ChannelID, VideoID: videoID, ImageSHA256: imageSHA256(thumbnail.Data), PostedAt: now,
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

	for i := range pending {
		item := &pending[i]
		prompt := buildTextPrompt(item.ChannelID, members, s.config.Translation.Glossary)

		rowCtx, cancel := context.WithTimeout(ctx, s.rowBudget)
		image, inputs, reason, err := s.translateOne(rowCtx, *item, prompt)

		cancel()

		if ctx.Err() != nil {
			return errors.Join(append(errs, ctx.Err())...)
		}

		if err != nil {
			s.logger.Warn("Schedule poster translation failed",
				slog.Int64("poster_id", item.ID), slog.String("reason", reason), slog.Any("error", err))

			if _, recordErr := s.translations.RecordTranslationFailure(ctx, *item, reason, inputs); recordErr != nil {
				errs = append(errs, recordErr)
			}

			continue
		}

		recorded, err := s.translations.RecordTranslation(ctx, *item, image)
		if err != nil {
			errs = append(errs, err)

			continue
		}

		s.logger.Info("Schedule poster translated",
			slog.Int64("poster_id", item.ID), slog.Bool("recorded", recorded), slog.Int("bytes", len(image.Data)))
	}

	return errors.Join(errs...)
}

// translateOne이 돌려주는 TranslationInputs는 실패 기록에 그대로 남길 값이다. 판독 오독이 번역표 대조나 그리기 검사를 막았을 수 있으므로,
// 다음 재번역 한 번만 다시 쓰도록 새로 만든 판독·번역표로 그리기에서 실패한 시도만 남기고, 원본을 받지 못한 시도는 이전 값을 그대로 둔다.
func (s *Service) translateOne(
	ctx context.Context, item posterstore.PendingTranslation, textPrompt string,
) (posterstore.Image, posterstore.TranslationInputs, string, error) {
	original, sourceErr := s.sources.image(ctx, item.ImageURL)
	if sourceErr != nil {
		return posterstore.Image{}, item.Inputs, reasonSourceUnavailable, sourceErr
	}

	if item.ImageSHA256 != "" && imageSHA256(original.Data) != item.ImageSHA256 {
		return posterstore.Image{}, item.Inputs, reasonSourceUnavailable, errors.New("poster image changed after it was collected")
	}

	key := translationInputsKey(s.config.Translation, original, textPrompt)

	transcript, table, reused := s.savedInputs(item, key)
	if !reused {
		var err error

		if transcript, err = s.transcribe(ctx, item.ID, stageOriginal, original); err != nil {
			return posterstore.Image{}, posterstore.TranslationInputs{}, reasonTranslationFailed, fmt.Errorf("transcribe original poster: %w", err)
		}

		s.logger.Info("Schedule poster original transcribed",
			slog.Int64("poster_id", item.ID),
			slog.String("transcript", strings.Join(transcript, " / ")))

		if table, err = s.checkedTable(ctx, item.ID, original, transcript, textPrompt); err != nil {
			return posterstore.Image{}, posterstore.TranslationInputs{}, reasonTranslationFailed, fmt.Errorf("translate poster text: %w", err)
		}
	}

	image, err := s.checkedRender(ctx, item.ID, original, transcript, table)
	if err != nil {
		var inputs posterstore.TranslationInputs

		if !reused {
			inputs = posterstore.TranslationInputs{SHA256: key, Transcript: transcript, Table: table.String()}
		}

		return posterstore.Image{}, inputs, reasonTranslationFailed, fmt.Errorf("render translated poster: %w", err)
	}

	return image, posterstore.TranslationInputs{}, "", nil
}

func (s *Service) savedInputs(item posterstore.PendingTranslation, key []byte) ([]string, translationTable, bool) {
	saved := item.Inputs
	if !bytes.Equal(saved.SHA256, key) {
		return nil, nil, false
	}

	table, err := parseTranslationTable(saved.Table)
	if err == nil {
		if problems := checkTable(table, saved.Transcript); len(problems) > 0 {
			err = fmt.Errorf("translation table check failed: %s", strings.Join(problems, "; "))
		}
	}

	if err != nil {
		s.logger.Warn("Schedule poster saved translation inputs rejected", slog.Int64("poster_id", item.ID), slog.Any("error", err))

		return nil, nil, false
	}

	s.logger.Info("Schedule poster saved translation inputs reused", slog.Int64("poster_id", item.ID))

	return saved.Transcript, table, true
}

func (s *Service) transcribe(ctx context.Context, posterID int64, stage string, src source) ([]string, error) {
	var lines []string

	err := retry.WithRetry(ctx, s.retryOptions(posterID, stage), func(ctx context.Context) error {
		var (
			spent usage
			err   error
		)

		lines, spent, err = s.translator.transcribe(ctx, src)
		s.logUsage(posterID, stage, spent)

		return err
	})
	if err != nil {
		return nil, fmt.Errorf("%s transcript: %w", stage, err)
	}

	return lines, nil
}

// checkedTable은 원본 판독과 어긋난 번역표를 받지 않고, 찾은 문제를 붙여 다시 만든다.
func (s *Service) checkedTable(ctx context.Context, posterID int64, original source, originalText []string, prompt string) (translationTable, error) {
	var (
		translation textTranslation
		problems    []string
	)

	err := retry.WithRetry(ctx, s.retryOptions(posterID, stageText), func(ctx context.Context) error {
		var (
			spent usage
			err   error
		)

		translation, spent, err = s.translator.translateText(ctx, original, withProblems(prompt, problems))
		s.logUsage(posterID, stageText, spent)

		if err != nil {
			return err
		}

		if problems = checkTable(translation.table, originalText); len(problems) > 0 {
			s.logger.Warn("Schedule poster table check failed",
				slog.Int64("poster_id", posterID),
				slog.Any("problems", problems),
				slog.String("table", translation.table.String()))

			return fmt.Errorf("translation table check failed: %s", strings.Join(problems, "; "))
		}

		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("build checked translation table: %w", err)
	}

	s.logger.Info("Schedule poster text translated",
		slog.Int64("poster_id", posterID),
		slog.Int("web_search_queries", translation.webSearchQueries),
		slog.String("table", translation.table.String()))

	return translation.table, nil
}

// renderAttempts는 검사에 걸린 번역본 가운데 문제가 가장 적은 것(draft)과 그 문제, 지금까지 찾은 모든 문제,
// draft를 바꾸지 못하고 연달아 검사에 걸린 횟수(stalls)를 기억한다. 검사까지 가지 못한 그리기 실패는 stalls에 넣지 않는다.
type renderAttempts struct {
	draft    source
	problems []renderProblem
	seen     []renderProblem
	stalls   int
}

// next는 첫 그리기면 원본을, 아니면 draft와 그 문제만 고칠 지시문을 고른다.
func (a *renderAttempts) next(original source, table translationTable) (source, string) {
	if len(a.problems) == 0 {
		return original, buildImagePrompt(table)
	}

	keep := slices.DeleteFunc(slices.Clone(a.seen), func(problem renderProblem) bool { return slices.Contains(a.problems, problem) })

	return a.draft, buildFixPrompt(table, a.problems, keep)
}

// reject에는 판독한 JPEG를 그대로 넘긴다. 다시 보내는 이미지가 문제 목록을 만든 이미지와 같아야 한다.
func (a *renderAttempts) reject(rendered source, found []renderProblem) {
	for _, problem := range found {
		if !slices.Contains(a.seen, problem) {
			a.seen = append(a.seen, problem)
		}
	}

	if len(a.problems) == 0 || len(found) < len(a.problems) {
		a.draft, a.problems, a.stalls = rendered, found, 0

		return
	}

	a.stalls++
}

// checkedRender는 저장할 JPEG를 판독해 검사를 통과한 번역본만 돌려준다. 검사에 걸리면 가장 나은 번역본을 이전 대화 없이 다시 보내 그 문제만 고치게 하고,
// 앞선 시도에서만 나온 문제는 다시 생기지 않게 함께 알린다.
func (s *Service) checkedRender(ctx context.Context, posterID int64, original source, originalText []string, table translationTable) (posterstore.Image, error) {
	var (
		image    posterstore.Image
		attempts renderAttempts
	)

	options := s.retryOptions(posterID, stageImage)

	options.ShouldRetry = func(err error) bool {
		return !errors.Is(err, errCheckUnavailable) && !errors.Is(err, errRenderStalled) && retryableTranslationError(err)
	}

	err := retry.WithRetry(ctx, options, func(ctx context.Context) error {
		input, prompt := attempts.next(original, table)

		translated, spent, err := s.translator.renderImage(ctx, input, prompt)
		s.logUsage(posterID, stageImage, spent)

		if err != nil {
			return err
		}

		data, err := toJPEG(translated)
		if err != nil {
			return err
		}

		rendered := source{MIME: "image/jpeg", Data: data}

		renderedText, err := s.transcribe(ctx, posterID, stageCheck, rendered)
		if err != nil {
			return fmt.Errorf("%w: %w", errCheckUnavailable, err)
		}

		if found := checkRender(table, originalText, renderedText); len(found) > 0 {
			attempts.reject(rendered, found)

			texts := problemTexts(found)

			s.logger.Warn("Schedule poster render check failed",
				slog.Int64("poster_id", posterID),
				slog.Any("problems", texts),
				slog.String("transcript", strings.Join(renderedText, " / ")))

			if attempts.stalls >= maxRenderStalls {
				return fmt.Errorf("%w: %s", errRenderStalled, strings.Join(texts, "; "))
			}

			return fmt.Errorf("render check failed: %s", strings.Join(texts, "; "))
		}

		image = posterstore.Image{MIME: rendered.MIME, Data: rendered.Data}

		return nil
	})
	if err != nil {
		return posterstore.Image{}, fmt.Errorf("draw checked poster: %w", err)
	}

	return image, nil
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
