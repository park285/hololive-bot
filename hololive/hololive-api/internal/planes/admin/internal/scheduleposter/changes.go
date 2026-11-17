package scheduleposter

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"net"
	"net/http"
	"reflect"
	"slices"
	"time"

	"cloud.google.com/go/auth"
	"github.com/park285/shared-go/v2/pkg/retry"
	"google.golang.org/genai"

	posterstore "github.com/kapu/hololive-api/internal/service/scheduleposter"
	"github.com/kapu/hololive-shared/pkg/domain"
)

const (
	stageChangeDecision  = "change_decision"
	stageChangeMatch     = "change_match"
	stageChangeTranslate = "change_translate"
	changeBatch          = 20
	changePostBudget     = 10 * time.Minute
	changeGiveUpAfter    = 6 * time.Hour
)

const (
	pauseCollect  = "collect"
	pauseDecision = "decision"
	pauseMatch    = "match"
)

var changeServiceCodes = []int{
	http.StatusBadRequest, http.StatusUnauthorized, http.StatusForbidden, http.StatusNotFound, http.StatusTooManyRequests, http.StatusServiceUnavailable,
}

type postOutcome int

const (
	postNext postOutcome = iota
	postMatch
	postStop
)

// changeLoopState는 detectChanges goroutine만 만지므로 잠그지 않는다. 프로세스가 다시 뜨면 비어 있고 종단 기준을 처음부터 다시 센다.
// 두 맵(judging, matches)의 항목은 기록에 성공하거나 대기열에서 사라질 때만 지운다. 종단 기준이 첫 시도부터라 먼저 지우면 기준이 늦춰진다.
type changeLoopState struct {
	collected map[string]collectMark
	judging   map[judgeKey]time.Time
	matches   map[matchKey]matchEntry
	// mentions는 다른 멤버의 글에서 찾은, 그 채널 멤버가 출연하는 방송의 글이다. 그 멤버의 확정본이 나중에 게시돼도 넣을 수 있게 lookback 동안 둔다.
	mentions map[string]map[int64]posterstore.Post
}

type judgeKey struct {
	channelID string
	postID    int64
}

type matchKey struct {
	channelID string
	postID    int64
	posterID  int64
	rowsAt    int64
}

type matchEntry struct {
	firstTried time.Time
	done       matchDone
}

type matchProgress int

const (
	matchNone matchProgress = iota
	matchJudged
	matchReady
)

// 계산에 쓴 앞선 변경(prior)이 지금과 내용이 다르면 입력이 바뀐 것이라 다시 계산한다. 진행이 matchJudged면 result.Changes는 재번역 전의 판정이다.
type matchDone struct {
	progress matchProgress
	prior    []posterstore.Change
	result   posterstore.MatchResult
	report   translationReport
}

type changeRoster struct {
	members []*domain.Member
	err     error
	loaded  bool
}

func (r *changeRoster) load(ctx context.Context, source memberSource) ([]*domain.Member, error) {
	if !r.loaded {
		r.members, r.err = source.GetAllMembers(ctx)
		r.loaded = true
	}

	return r.members, r.err
}

func (s *Service) detectsChanges() bool {
	return s.classifier != nil && s.config.ChangePollSeconds > 0
}

// detectChanges는 goroutine 하나에서만 돌린다. 같은 멤버의 글을 게시 순서로 하나씩 대응해야 뒤 글의 입력이 앞 글의 변경을 담는다.
func (s *Service) detectChanges(ctx context.Context) error {
	now := s.now()
	roster := &changeRoster{}

	if s.changeLoop.collected == nil {
		s.changeLoop = changeLoopState{
			collected: map[string]collectMark{}, judging: map[judgeKey]time.Time{}, matches: map[matchKey]matchEntry{},
			mentions: map[string]map[int64]posterstore.Post{},
		}
	}

	s.changeLoop.dropMentionsBefore(now.Add(-lookback))

	var (
		errs   []error
		paused bool
	)

	for _, target := range s.config.Targets {
		stage, err := s.detectTarget(ctx, target, postedDate(now), roster)
		if ctx.Err() != nil {
			return errors.Join(append(errs, ctx.Err())...)
		}

		if err == nil {
			continue
		}

		if stage == "" {
			errs = append(errs, err)

			continue
		}

		s.logger.Warn("Schedule poster change detection paused",
			slog.String("stage", stage), slog.String("channel_id", target.ChannelID), slog.Any("error", err))

		if errors.Is(err, errChangeServiceFailed) {
			paused = true

			break
		}
	}

	if _, err := s.ledger.PurgePosts(ctx, now.Add(-retention), purgeBatch); err != nil {
		errs = append(errs, err)
	}

	switch {
	case ctx.Err() != nil:
	case paused:
		changeCyclesTotal.WithLabelValues(changeCyclePaused).Inc()
	case len(errs) == 0:
		changeCyclesTotal.WithLabelValues(changeCycleCompleted).Inc()
		changeLastCompleted.Set(float64(s.now().Unix()))
	default:
		changeCyclesTotal.WithLabelValues(changeCycleStoreError).Inc()
	}

	return errors.Join(errs...)
}

// detectTarget이 단계와 함께 돌려준 오류는 모델 실패이고, 단계 없이 돌려준 오류는 저장소 오류다.
func (s *Service) detectTarget(ctx context.Context, target Target, today time.Time, roster *changeRoster) (string, error) {
	schedule, ok, err := s.changes.PublishedRows(ctx, target.ChannelID, today)
	if err != nil {
		return "", fmt.Errorf("load published schedule rows for changes: %w", err)
	}

	if !ok {
		return "", nil
	}

	// JST로 바꾼 행은 글이 말하는 현지 시각과 대응할 수 없다.
	if slices.ContainsFunc(schedule.Rows, func(row posterstore.Row) bool { return row.SourceZone != "" }) {
		s.logger.Info("Schedule poster change detection skipped",
			slog.String("channel_id", target.ChannelID), slog.String("reason", "unsupported_zone"))

		return "", nil
	}

	err = s.storePosts(ctx, target, &schedule, roster)
	if err != nil {
		return "", err
	}

	pending, err := s.changes.PendingPosts(ctx, schedule, changeBatch)
	if err != nil {
		return "", fmt.Errorf("load pending schedule poster posts: %w", err)
	}

	maps.DeleteFunc(s.changeLoop.judging, func(key judgeKey, _ time.Time) bool {
		return key.channelID == target.ChannelID && !slices.ContainsFunc(pending, func(post posterstore.Post) bool {
			return post.ID == key.postID && post.Status == posterstore.PostUnjudged
		})
	})
	maps.DeleteFunc(s.changeLoop.matches, func(key matchKey, _ matchEntry) bool {
		return key.channelID == target.ChannelID &&
			!slices.ContainsFunc(pending, func(post posterstore.Post) bool {
				return post.Status != posterstore.PostUnjudged && pendingMatchKey(target, &schedule, &post) == key
			})
	})

	return s.processPosts(ctx, target, today, schedule, pending, roster)
}

func (s *Service) processPosts(
	ctx context.Context, target Target, today time.Time, schedule posterstore.PublishedSchedule, pending []posterstore.Post, roster *changeRoster,
) (string, error) {
	for handled := 0; len(pending) > 0 && handled < changeBatch; handled++ {
		post := &pending[0]

		pending = pending[1:]

		if post.Status == posterstore.PostUnjudged {
			outcome, err := s.decidePost(ctx, target, &schedule, post)
			if err != nil {
				return modelStage(pauseDecision, err), err
			}

			if outcome == postStop {
				return "", nil
			}

			if outcome == postNext {
				continue
			}
		}

		if schedule.RowsPending {
			continue
		}

		// 앞 글의 기록을 담아야 뒤 글의 대응 입력이 그 변경을 반영한다.
		current, ok, err := s.changes.PublishedRows(ctx, target.ChannelID, today)
		if err != nil {
			return "", fmt.Errorf("reload published schedule rows for changes: %w", err)
		}

		if !ok || current.PosterID != schedule.PosterID || !current.ExtractedAt.Equal(schedule.ExtractedAt) || current.RowsPending {
			return "", nil
		}

		schedule = current

		recorded, applied, err := s.matchOne(ctx, target, &schedule, post, roster)
		if err != nil {
			return modelStage(pauseMatch, err), err
		}

		if !recorded {
			return "", nil
		}

		// 반영 변경을 기록하면 저장소가 같은 묶음의 뒤 글을 대기열로 돌려보낸다. 다시 읽어야 그 글이 더 뒤의 글보다 먼저 대응된다.
		if applied {
			pending, err = s.changes.PendingPosts(ctx, schedule, changeBatch)
			if err != nil {
				return "", fmt.Errorf("reload pending schedule poster posts: %w", err)
			}

			pending = slices.DeleteFunc(pending, func(next posterstore.Post) bool { return !postedAfter(&next, post) })
		}
	}

	return "", nil
}

func modelStage(stage string, err error) string {
	if errors.Is(err, errChangeServiceFailed) || errors.Is(err, errChangeOutcomeUnknown) {
		return stage
	}

	return ""
}

func postedAfter(post, than *posterstore.Post) bool {
	return post.PostedAt.After(than.PostedAt) || post.PostedAt.Equal(than.PostedAt) && post.ID > than.ID
}

func (s *Service) storePosts(ctx context.Context, target Target, schedule *posterstore.PublishedSchedule, roster *changeRoster) error {
	posts := s.changeLoop.mentionsFor(target.ChannelID)

	var (
		collected collectedPosts
		fetched   bool
	)

	if target.XUserID != "" {
		var err error

		collected, fetched, err = s.fetchPosts(ctx, target, schedule, roster)
		if err != nil {
			return err
		}

		posts = append(collected.posts, posts...)
	}

	var stored int64

	if fetched || len(posts) > 0 {
		var err error

		stored, err = s.ledger.InsertPosts(ctx, schedule.PosterID, posts)
		if err != nil {
			return fmt.Errorf("store schedule poster posts: %w", err)
		}
	}

	switch {
	case !fetched:
	case collected.reached:
		newest := collected.newest
		if mark := s.changeLoop.collected[target.ChannelID]; mark.posterID == schedule.PosterID {
			newest = later(newest, mark.newest)
		}

		s.changeLoop.collected[target.ChannelID] = collectMark{posterID: schedule.PosterID, newest: newest}
	default:
		s.logger.Warn("Schedule poster posts collection stopped at the page limit",
			slog.String("channel_id", target.ChannelID), slog.Int64("poster_id", schedule.PosterID), slog.Int("pages", maxChangePages))
	}

	if stored > 0 {
		s.logger.Info("Schedule poster posts collected",
			slog.String("channel_id", target.ChannelID), slog.Int64("poster_id", schedule.PosterID), slog.Int64("inserted", stored))
	}

	return nil
}

// fetchPosts가 false를 돌려주면 X 글을 읽지 못한 것이고 이번 주기에는 본인 글을 넣지 않는다. 오류는 저장소 오류이거나 ctx 종료다.
func (s *Service) fetchPosts(
	ctx context.Context, target Target, schedule *posterstore.PublishedSchedule, roster *changeRoster,
) (collectedPosts, bool, error) {
	collected, err := s.collectPosts(ctx, target, schedule)
	if err != nil {
		if ctx.Err() != nil {
			return collectedPosts{}, false, ctx.Err()
		}

		s.logger.Warn("Schedule poster change detection paused",
			slog.String("stage", pauseCollect), slog.String("channel_id", target.ChannelID), slog.Any("error", err))

		return collectedPosts{}, false, nil
	}

	mentions, err := s.attachStreams(ctx, target, &collected, roster)
	if err != nil {
		return collectedPosts{}, false, err
	}

	s.changeLoop.keepMentions(mentions)

	return collected, true, nil
}

func (s *Service) decidePost(
	ctx context.Context, target Target, schedule *posterstore.PublishedSchedule, post *posterstore.Post,
) (postOutcome, error) {
	key := judgeKey{channelID: target.ChannelID, postID: post.ID}

	firstTried, ok := s.changeLoop.judging[key]
	if !ok {
		firstTried = s.now()
		s.changeLoop.judging[key] = firstTried
	}

	answer, spent, err := s.classifier.classifyPost(ctx, post.Body)
	s.logUsage(schedule.PosterID, stageChangeDecision, spent)

	if ctx.Err() != nil {
		return postStop, ctx.Err()
	}

	var decision posterstore.PostDecision

	if err != nil {
		reason := s.closingReason(err, later(post.CreatedAt, firstTried).Add(changeGiveUpAfter))
		if reason == "" {
			return postStop, err
		}

		s.logPostFailure(post, schedule, posterstore.PostJudgeFailed, reason, err)

		decision = posterstore.PostDecision{Status: posterstore.PostJudgeFailed, Reason: reason}
	} else {
		decision = answer.decision(s.config.Translation.Rows.Decide.ScheduleInfoMin)
	}

	recorded, err := s.changes.RecordDecision(ctx, target.ChannelID, post.ID, decision)
	if err != nil {
		return postStop, fmt.Errorf("record schedule poster post decision: %w", err)
	}

	delete(s.changeLoop.judging, key)

	if !recorded {
		return postStop, nil
	}

	post.Status = decision.Status
	postsTotal.WithLabelValues(string(decision.Status)).Inc()

	s.logger.Info("Schedule poster post judged",
		slog.Int64("post_id", post.ID), slog.Int64("poster_id", schedule.PosterID), slog.String("status", string(decision.Status)),
		slog.Float64("schedule_info", decision.ScheduleInfo), slog.String("kind", decision.Kind), slog.String("reason", decision.Reason))

	if decision.Status == posterstore.PostRelevant {
		return postMatch, nil
	}

	return postNext, nil
}

func pendingMatchKey(target Target, schedule *posterstore.PublishedSchedule, post *posterstore.Post) matchKey {
	return matchKey{channelID: target.ChannelID, postID: post.ID, posterID: schedule.PosterID, rowsAt: schedule.ExtractedAt.UnixNano()}
}

func (s *Service) matchOne(
	ctx context.Context, target Target, schedule *posterstore.PublishedSchedule, post *posterstore.Post, roster *changeRoster,
) (recorded, applied bool, err error) {
	prior := priorChanges(schedule.Changes, post)
	key := pendingMatchKey(target, schedule, post)

	entry := s.changeLoop.matches[key]
	if entry.firstTried.IsZero() {
		entry.firstTried = s.now()
	}

	deadline := later(later(post.CreatedAt, schedule.ExtractedAt), entry.firstTried).Add(changeGiveUpAfter)

	if entry.done.progress != matchNone && !samePrior(entry.done.prior, prior) {
		entry.done = matchDone{}
	}

	if entry.done.progress != matchReady {
		entry.done, err = s.computeMatch(ctx, target, schedule, post, prior, roster, deadline, entry.done)
		if err != nil {
			s.changeLoop.matches[key] = entry

			return false, false, err
		}
	}

	done := entry.done
	result := done.result

	recorded, err = s.changes.RecordMatch(ctx, *schedule, *post, result)
	if err != nil && result.Status == posterstore.PostMatched && !s.now().Before(deadline) {
		s.logPostFailure(post, schedule, posterstore.PostMatchFailed, reasonUnavailable, err)

		result = posterstore.MatchResult{Status: posterstore.PostMatchFailed, Reason: reasonUnavailable}
		recorded, err = s.changes.RecordMatch(ctx, *schedule, *post, result)
	}

	if err != nil {
		s.changeLoop.matches[key] = entry

		return false, false, fmt.Errorf("record schedule poster post match: %w", err)
	}

	delete(s.changeLoop.matches, key)

	if recorded {
		postsTotal.WithLabelValues(string(result.Status)).Inc()
	}

	if result.Status == posterstore.PostMatched {
		s.logMatched(post, schedule, recorded, result.Changes, done.report)
	}

	applied = recorded && slices.ContainsFunc(result.Changes, func(change posterstore.Change) bool { return change.Kind.Applied() })

	return recorded, applied, nil
}

func samePrior(kept, prior []posterstore.Change) bool {
	return slices.EqualFunc(kept, prior, func(a, b posterstore.Change) bool {
		a.RecordedAt, b.RecordedAt = time.Time{}, time.Time{}

		return reflect.DeepEqual(a, b)
	})
}

// computeMatch가 오류와 함께 돌려준 값은 다음 주기에 이어 쓸 진행이고, 오류는 기록하지 않고 멈출 실패다.
// 글 탓 실패와 종단 기준이 지난 불명 실패는 match_failed 결과로 돌려준다.
func (s *Service) computeMatch(
	ctx context.Context, target Target, schedule *posterstore.PublishedSchedule, post *posterstore.Post, prior []posterstore.Change,
	roster *changeRoster, deadline time.Time, done matchDone,
) (matchDone, error) {
	members, err := roster.load(ctx, s.members)
	if err != nil {
		return done, fmt.Errorf("load members for schedule poster changes: %w", err)
	}

	postCtx, cancel := context.WithTimeout(ctx, changePostBudget)
	defer cancel()

	if done.progress == matchNone {
		var changes []posterstore.Change

		changes, err = s.judgeChanges(postCtx, schedule, post, prior)
		if err == nil {
			done = matchDone{progress: matchJudged, prior: prior, result: posterstore.MatchResult{Status: posterstore.PostMatched, Changes: changes}}
		}
	}

	if err == nil {
		var finished matchDone

		finished, err = s.finishChanges(postCtx, target, schedule, post, &done, members)
		if err == nil && ctx.Err() == nil {
			return finished, nil
		}
	}

	if ctx.Err() != nil {
		return done, ctx.Err()
	}

	reason := s.closingReason(err, deadline)
	if reason == "" {
		return done, err
	}

	s.logPostFailure(post, schedule, posterstore.PostMatchFailed, reason, err)

	return matchDone{progress: matchReady, prior: prior, result: posterstore.MatchResult{Status: posterstore.PostMatchFailed, Reason: reason}}, nil
}

func (s *Service) judgeChanges(
	ctx context.Context, schedule *posterstore.PublishedSchedule, post *posterstore.Post, prior []posterstore.Change,
) ([]posterstore.Change, error) {
	prompt := buildMatchPrompt(*post, schedule.WeekStart, schedule.WeekEnd, matchRows(posterstore.ApplyChanges(schedule.Rows, prior)))

	items, err := s.matchItems(ctx, schedule.PosterID, prompt)
	if err != nil {
		return nil, err
	}

	return judgeItems(*post, schedule.WeekStart, schedule.WeekEnd, schedule.Rows, prior, items), nil
}

func (s *Service) finishChanges(
	ctx context.Context, target Target, schedule *posterstore.PublishedSchedule, post *posterstore.Post, judged *matchDone, members []*domain.Member,
) (matchDone, error) {
	changes, report, err := s.translateChanges(ctx, target, schedule, post, judged.prior, judged.result.Changes, members)
	if err != nil {
		return matchDone{}, err
	}

	if err := posterstore.ValidateChanges(changes); err != nil {
		return matchDone{}, &postFaultError{reason: reasonInvalidChanges, err: err}
	}

	return matchDone{progress: matchReady, prior: judged.prior, result: posterstore.MatchResult{Status: posterstore.PostMatched, Changes: changes}, report: report}, nil
}

func (s *Service) matchItems(ctx context.Context, posterID int64, prompt string) ([]matchItem, error) {
	var (
		items               []matchItem
		attempts, malformed int
	)

	options := s.retryOptions(posterID, stageChangeMatch)

	options.ShouldRetry = func(err error) bool {
		return !errors.Is(err, errMatchBlocked) && !vertexUnreachable(err) && retryableTranslationError(err)
	}

	err := retry.WithRetry(ctx, options, func(ctx context.Context) error {
		var (
			spent usage
			err   error
		)

		items, spent, err = s.matcher.matchPost(ctx, prompt)
		s.logUsage(posterID, stageChangeMatch, spent)

		attempts++

		if errors.Is(err, errBadAnswer) {
			malformed++
		}

		return err
	})

	switch {
	case err == nil:
		return items, nil
	case ctx.Err() != nil:
		return nil, fmt.Errorf("%w: match post: %w", errChangeOutcomeUnknown, err)
	case errors.Is(err, errMatchBlocked):
		return nil, &postFaultError{reason: reasonBlocked, err: err}
	case errors.Is(err, errBadAnswer) && malformed == attempts:
		return nil, &postFaultError{reason: reasonBadAnswer, err: err}
	case vertexServiceFailure(err):
		return nil, fmt.Errorf("%w: match post: %w", errChangeServiceFailed, err)
	}

	return nil, fmt.Errorf("%w: match post: %w", errChangeOutcomeUnknown, err)
}

func vertexServiceFailure(err error) bool {
	apiErr, ok := errors.AsType[genai.APIError](err)

	return ok && slices.Contains(changeServiceCodes, apiErr.Code) || vertexUnreachable(err)
}

// vertexUnreachable은 요청이 Vertex에 닿기 전의 실패(이름 확인·연결·TLS 인증서·토큰 발급)다. 응답을 기다리다 난 실패는 그 글 때문일 수 있어 넣지 않는다.
func vertexUnreachable(err error) bool {
	if _, ok := errors.AsType[genai.APIError](err); ok {
		return false
	}

	_, lookup := errors.AsType[*net.DNSError](err)
	opErr, isOp := errors.AsType[*net.OpError](err)
	_, certificate := errors.AsType[*tls.CertificateVerificationError](err)
	_, record := errors.AsType[tls.RecordHeaderError](err)
	_, token := errors.AsType[*auth.Error](err)

	return lookup || isOp && opErr.Op == "dial" || certificate || record || token
}

// closingReason이 빈 값이면 기록하지 않고 멈출 실패다.
func (s *Service) closingReason(err error, deadline time.Time) string {
	if fault, ok := errors.AsType[*postFaultError](err); ok {
		return fault.reason
	}

	if errors.Is(err, errChangeServiceFailed) || s.now().Before(deadline) {
		return ""
	}

	return reasonUnavailable
}

func priorChanges(changes []posterstore.Change, post *posterstore.Post) []posterstore.Change {
	var prior []posterstore.Change

	for i := range changes {
		change := &changes[i]
		if change.PostedAt.Before(post.PostedAt) || change.PostedAt.Equal(post.PostedAt) && change.PostID < post.ID {
			prior = append(prior, *change)
		}
	}

	return prior
}

func (s *Service) logPostFailure(
	post *posterstore.Post, schedule *posterstore.PublishedSchedule, status posterstore.PostStatus, reason string, err error,
) {
	s.logger.Warn("Schedule poster post failed",
		slog.Int64("post_id", post.ID), slog.Int64("poster_id", schedule.PosterID), slog.String("status", string(status)),
		slog.String("reason", reason), slog.Any("error", err))
}

func (s *Service) logMatched(
	post *posterstore.Post, schedule *posterstore.PublishedSchedule, recorded bool, changes []posterstore.Change, report translationReport,
) {
	reasons := 0
	warnings, doubts := 0, 0

	for i := range changes {
		if changes[i].Reason != "" {
			reasons++
		}

		warnings += len(changes[i].Warnings)

		if changes[i].DoubtKo != "" {
			doubts++
		}
	}

	s.logger.Info("Schedule poster post matched",
		slog.Int64("post_id", post.ID), slog.Int64("poster_id", schedule.PosterID), slog.Bool("recorded", recorded),
		slog.Group("kinds", slog.Int("count", len(changes))), slog.Group("reasons", slog.Int("count", reasons)), slog.Int("translated", report.translated),
		slog.Bool("translation_failed", report.failed), slog.Int("warnings", warnings), slog.Int("doubts", doubts))
}
