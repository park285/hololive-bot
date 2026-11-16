package scheduleposter

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/jackc/pgx/v5"
)

// InsertPosts는 확정본 posterID를 게시한 뒤이고 그 주 범위(JST 날짜) 안에 올린 글만 넣고, 넣은 글 수를 돌려준다.
// 이미 기록한 글은 건너뛴다. 글 하나라도 저장할 수 없으면 아무것도 넣지 않고 오류다.
func (s *Store) InsertPosts(ctx context.Context, posterID int64, posts []Post) (int64, error) {
	if err := validatePosts(posts); err != nil {
		return 0, fmt.Errorf("insert schedule poster posts: %w", err)
	}

	if len(posts) == 0 {
		return 0, nil
	}

	ids := make([]int64, len(posts))
	postedAt := make([]time.Time, len(posts))
	bodies := make([]string, len(posts))

	for i := range posts {
		ids[i], postedAt[i], bodies[i] = posts[i].ID, posts[i].PostedAt, posts[i].Body
	}

	tag, err := s.pool.Exec(ctx, insertPostsSQL, posterID, ids, postedAt, bodies)
	if err != nil {
		return 0, fmt.Errorf("insert schedule poster posts: %w", err)
	}

	return tag.RowsAffected(), nil
}

func validatePosts(posts []Post) error {
	seen := make(map[int64]bool, len(posts))

	for i := range posts {
		post := &posts[i]

		switch n := utf8.RuneCountInString(post.Body); {
		case post.ID <= 0:
			return fmt.Errorf("post %d: id must be positive, got %d", i+1, post.ID)
		case seen[post.ID]:
			return fmt.Errorf("post %d: duplicate id %d", i+1, post.ID)
		case n < 1 || n > MaxPostChars:
			return fmt.Errorf("post %d: body must be 1..%d characters, got %d", i+1, MaxPostChars, n)
		case !utf8.ValidString(post.Body) || strings.ContainsRune(post.Body, 0):
			return fmt.Errorf("post %d: body must be valid UTF-8 without NUL", i+1)
		}

		seen[post.ID] = true
	}

	return nil
}

// PendingPosts는 schedule의 행 묶음이 지금도 게시 중일 때 판별할 글과 그 묶음에 아직 대응하지 않은 글을 게시 순서로 돌려준다.
// 묶음이 바뀌었으면 빈 목록이고, 재추출을 기다리는 동안(RowsPending)은 판별할 글만 돌려준다.
func (s *Store) PendingPosts(ctx context.Context, schedule PublishedSchedule, limit int) ([]Post, error) {
	rows, err := s.pool.Query(ctx, pendingPostsSQL, schedule.PosterID, schedule.ExtractedAt, min(max(limit, 1), maxListLimit))
	if err != nil {
		return nil, fmt.Errorf("list pending schedule poster posts: %w", err)
	}

	posts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Post, error) {
		var (
			post     Post
			posterID *int64
			rowsAt   *time.Time
		)

		scanErr := row.Scan(&post.ID, &post.PostedAt, &post.Body, &post.Status, &post.CreatedAt, &posterID, &rowsAt)
		if posterID != nil && rowsAt != nil {
			post.MatchedPosterID, post.MatchedRowsAt = *posterID, *rowsAt
		}

		return post, scanErr
	})
	if err != nil {
		return nil, fmt.Errorf("list pending schedule poster posts: %w", err)
	}

	return posts, nil
}

// RecordDecision은 아직 판별하지 않은 글에만 판별 결과를 기록한다. 이미 판별한 글이면 false다.
func (s *Store) RecordDecision(ctx context.Context, postID int64, decision PostDecision) (bool, error) {
	switch decision.Status {
	case PostNotRelevant, PostJudgeFailed, PostRelevant:
	case PostUnjudged, PostMatched, PostMatchFailed, PostRevoked:
		return false, fmt.Errorf("record schedule poster decision: status %q is not a decision", decision.Status)
	default:
		return false, fmt.Errorf("record schedule poster decision: invalid status %q", decision.Status)
	}

	var (
		info *float64
		kind *string
	)

	if decision.Kind != "" {
		info, kind = new(decision.ScheduleInfo), new(decision.Kind)
	}

	tag, err := s.pool.Exec(ctx, recordDecisionSQL, postID, decision.Status, decision.Reason, info, kind)
	if err != nil {
		return false, fmt.Errorf("record schedule poster decision: %w", err)
	}

	return tag.RowsAffected() == 1, nil
}

// RecordMatch는 PendingPosts가 읽은 post를 schedule의 행 묶음에 대응한 결과로 기록하고 그 글의 옛 변경을 새 변경으로 바꾼다.
// 반영 변경(ChangeKind.Applied)이 하나라도 있으면 같은 묶음에 이미 대응한 뒤 글(게시 순서)을 relevant로 돌려 다시 대응하게 한다.
// 그 사이 묶음이 바뀌었거나, 글의 상태나 대응 도장이 post와 달라졌거나(다른 작업의 기록, 철회), 이 글이 이미 그 묶음에 대응했으면
// 아무것도 쓰지 않고 false다. 변경이 ValidateChanges를 통과하지 못하면 오류다.
// 대응 작업은 모든 인스턴스를 통틀어 하나만 돈다는 전제다. 둘이 겹치면 뒤 글이 앞선 글을 반영하지 않은 판정으로 matched에 남을 수 있다.
func (s *Store) RecordMatch(ctx context.Context, schedule PublishedSchedule, post Post, result MatchResult) (bool, error) {
	payload, err := encodeChanges(post.ID, result)
	if err != nil {
		return false, fmt.Errorf("record schedule poster match: %w", err)
	}

	var (
		readPosterID *int64
		readRowsAt   *time.Time
	)

	if post.MatchedPosterID != 0 {
		readPosterID, readRowsAt = new(post.MatchedPosterID), new(post.MatchedRowsAt)
	}

	recorded := false

	err = pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		tag, markErr := tx.Exec(ctx, recordMatchSQL, post.ID, schedule.PosterID, schedule.ExtractedAt, result.Status, result.Reason,
			post.Status, readPosterID, readRowsAt)
		if markErr != nil {
			return fmt.Errorf("mark post matched: %w", markErr)
		}

		if tag.RowsAffected() != 1 {
			return nil
		}

		if _, deleteErr := tx.Exec(ctx, deleteChangesSQL, post.ID); deleteErr != nil {
			return fmt.Errorf("delete changes: %w", deleteErr)
		}

		if payload != "" {
			if _, insertErr := tx.Exec(ctx, insertChangesSQL, post.ID, payload); insertErr != nil {
				return fmt.Errorf("insert changes: %w", insertErr)
			}
		}

		if slices.ContainsFunc(result.Changes, func(change Change) bool { return change.Kind.Applied() }) {
			if _, requeueErr := tx.Exec(ctx, requeueLaterPostsSQL, post.ID, schedule.PosterID, schedule.ExtractedAt); requeueErr != nil {
				return fmt.Errorf("requeue later posts: %w", requeueErr)
			}
		}

		recorded = true

		return nil
	})
	if err != nil {
		return false, fmt.Errorf("record schedule poster match: %w", err)
	}

	return recorded, nil
}

type changeRecord struct {
	Item         int        `json:"item"`
	Kind         ChangeKind `json:"kind"`
	Reason       string     `json:"reason"`
	Slot         int        `json:"slot"`
	TargetPostID int64      `json:"target_post_id"`
	TargetItem   int        `json:"target_item"`
	Day          *string    `json:"day"`
	Start        *string    `json:"start_time"`
	Status       RowStatus  `json:"status"`
	Text         string     `json:"source_text"`
	TitleKo      string     `json:"title_ko"`
	WithKo       []string   `json:"with_ko"`
	PlaceKo      string     `json:"place_ko"`
	TagsKo       []string   `json:"tags_ko"`
	NoteKo       string     `json:"note_ko"`
	Terms        []Term     `json:"terms"`
	DoubtKo      string     `json:"doubt_ko"`
	Warnings     []string   `json:"warnings"`
}

func encodeChanges(postID int64, result MatchResult) (string, error) {
	switch {
	case result.Status != PostMatched && result.Status != PostMatchFailed:
		return "", fmt.Errorf("status %q is not a match result", result.Status)
	case result.Status == PostMatchFailed && len(result.Changes) > 0:
		return "", errors.New("failed match cannot carry changes")
	}

	if err := ValidateChanges(result.Changes); err != nil {
		return "", err
	}

	if len(result.Changes) == 0 {
		return "", nil
	}

	records := make([]changeRecord, len(result.Changes))

	for i := range result.Changes {
		change := &result.Changes[i]
		if change.PostID != postID {
			return "", fmt.Errorf("change %d belongs to post %d, not %d", i+1, change.PostID, postID)
		}

		records[i] = changeRecord{
			Item: change.Item, Kind: change.Kind, Reason: change.Reason, Slot: change.Slot,
			TargetPostID: change.TargetPostID, TargetItem: change.TargetItem, Start: change.Start, Status: change.Status,
			Text: change.Text, TitleKo: change.TitleKo, WithKo: change.WithKo, PlaceKo: change.PlaceKo, TagsKo: change.TagsKo,
			NoteKo: change.NoteKo, Terms: change.Terms, DoubtKo: change.DoubtKo, Warnings: change.Warnings,
		}

		if !change.Day.IsZero() {
			records[i].Day = new(change.Day.Format(time.DateOnly))
		}
	}

	payload, err := jsonv2.Marshal(records)
	if err != nil {
		return "", fmt.Errorf("encode changes: %w", err)
	}

	return string(payload), nil
}

// PurgePosts는 before보다 먼저 올린 글을 게시 순서로 limit건까지 지운다. 그 글의 변경도 함께 지워진다.
func (s *Store) PurgePosts(ctx context.Context, before time.Time, limit int) (int64, error) {
	tag, err := s.pool.Exec(ctx, purgePostsSQL, before, limit)
	if err != nil {
		return 0, fmt.Errorf("purge schedule poster posts: %w", err)
	}

	return tag.RowsAffected(), nil
}

// RevokePost는 확정본 posterID에 대응할 수 있는 글(같은 멤버, 포스터 게시 뒤, 주 범위 안)을 철회한다. 글은 RestorePost 전까지
// 다시 판별·대응하지 않고 그 변경은 지운다. 포스터나 글이 없거나 서로 이어지지 않으면 ErrNotFound, 포스터가 확정본이 아니거나 판별 결과가
// relevant·matched·match_failed가 아니면 ErrInvalidState다.
func (s *Store) RevokePost(ctx context.Context, posterID, postID int64) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		if err := setPostState(ctx, tx, revokePostSQL, posterID, postID); err != nil {
			return err
		}

		if _, err := tx.Exec(ctx, deleteChangesSQL, postID); err != nil {
			return fmt.Errorf("delete changes: %w", err)
		}

		return nil
	})
	if err != nil {
		return fmt.Errorf("revoke schedule poster post: %w", err)
	}

	return nil
}

// RestorePost는 RevokePost로 철회한 글을 relevant로 돌려 다음 주기에 다시 대응하게 한다. 조건과 오류는 RevokePost와 같고,
// 글이 철회 상태가 아니면 ErrInvalidState다.
func (s *Store) RestorePost(ctx context.Context, posterID, postID int64) error {
	err := pgx.BeginFunc(ctx, s.pool, func(tx pgx.Tx) error {
		return setPostState(ctx, tx, restorePostSQL, posterID, postID)
	})
	if err != nil {
		return fmt.Errorf("restore schedule poster post: %w", err)
	}

	return nil
}

func setPostState(ctx context.Context, tx pgx.Tx, query string, posterID, postID int64) error {
	var (
		review  *ReviewStatus
		status  *string
		changed bool
	)

	if err := tx.QueryRow(ctx, query, posterID, postID).Scan(&review, &status, &changed); err != nil {
		return fmt.Errorf("set post state: %w", err)
	}

	switch {
	case review == nil:
		return ErrNotFound
	case *review != ReviewConfirmed:
		return ErrInvalidState
	case status == nil:
		return ErrNotFound
	case !changed:
		return ErrInvalidState
	}

	return nil
}

// PosterPost는 포스터에 대응할 수 있는 글 하나와 그 처리 상태다.
type PosterPost struct {
	ID          int64
	PostedAt    time.Time
	TextExcerpt string
	Status      PostStatus
	Reason      string
	// Current는 대응 도장이 포스터의 지금 행 묶음과 같다는 뜻이다. Changes는 Current인 글만 채운다.
	Current bool
	// Revocable·Restorable은 RevokePost·RestorePost가 받는 포스터와 글의 상태라는 뜻이다.
	Revocable  bool
	Restorable bool
	// Changes는 거절과 같음을 포함한 그 글의 변경을 항목 순서로 담고, Change의 한국어 칸은 TitleKo만 채운다.
	Changes []Change
}

const (
	maxActionablePosterPosts = 200
	maxOtherPosterPosts      = 50
)

// PosterPosts는 posterID에 대응할 수 있는 글을 최근 글부터 돌려준다. 상태가 relevant·matched·match_failed·revoked인 글은
// 200건까지, 나머지 상태는 50건까지다. 포스터가 없으면 ErrNotFound다. TextExcerpt는 본문 앞 280자다.
func (s *Store) PosterPosts(ctx context.Context, posterID int64) ([]PosterPost, error) {
	var posts []PosterPost

	err := pgx.BeginTxFunc(ctx, s.pool, pgx.TxOptions{IsoLevel: pgx.RepeatableRead, AccessMode: pgx.ReadOnly}, func(tx pgx.Tx) error {
		rows, err := tx.Query(ctx, getSQL, posterID)
		if err != nil {
			return fmt.Errorf("load poster: %w", err)
		}

		if _, err = pgx.CollectExactlyOneRow(rows, scanPoster); errors.Is(err, pgx.ErrNoRows) {
			return ErrNotFound
		} else if err != nil {
			return fmt.Errorf("load poster: %w", err)
		}

		if posts, err = loadPosterPosts(ctx, tx, posterID); err != nil {
			return err
		}

		return loadPosterPostChanges(ctx, tx, posts)
	})
	if err != nil {
		return nil, fmt.Errorf("list schedule poster posts: %w", err)
	}

	return posts, nil
}

func loadPosterPosts(ctx context.Context, tx pgx.Tx, posterID int64) ([]PosterPost, error) {
	rows, err := tx.Query(ctx, posterPostsSQL, posterID, maxActionablePosterPosts, maxOtherPosterPosts)
	if err != nil {
		return nil, fmt.Errorf("load posts: %w", err)
	}

	posts, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (PosterPost, error) {
		var item PosterPost

		return item, row.Scan(&item.ID, &item.PostedAt, &item.TextExcerpt, &item.Status, &item.Reason, &item.Current, &item.Revocable,
			&item.Restorable)
	})
	if err != nil {
		return nil, fmt.Errorf("load posts: %w", err)
	}

	return posts, nil
}

func loadPosterPostChanges(ctx context.Context, tx pgx.Tx, posts []PosterPost) error {
	current := make(map[int64]int, len(posts))
	ids := make([]int64, 0, len(posts))

	for i := range posts {
		if posts[i].Current {
			current[posts[i].ID] = i
			ids = append(ids, posts[i].ID)
		}
	}

	if len(ids) == 0 {
		return nil
	}

	rows, err := tx.Query(ctx, posterPostChangesSQL, ids)
	if err != nil {
		return fmt.Errorf("load post changes: %w", err)
	}

	changes, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Change, error) {
		var (
			change Change
			day    *time.Time
		)

		scanErr := row.Scan(&change.PostID, &change.Item, &change.Kind, &change.Reason, &change.Slot, &change.TargetPostID,
			&change.TargetItem, &day, &change.Start, &change.Status, &change.Text, &change.TitleKo)
		if day != nil {
			change.Day = *day
		}

		return change, scanErr
	})
	if err != nil {
		return fmt.Errorf("load post changes: %w", err)
	}

	for i := range changes {
		j := current[changes[i].PostID]

		posts[j].Changes = append(posts[j].Changes, changes[i])
	}

	return nil
}

func publishedChanges(ctx context.Context, tx pgx.Tx, schedule *PublishedSchedule) ([]Change, error) {
	rows, err := tx.Query(ctx, publishedChangesSQL, schedule.PosterID, schedule.ExtractedAt)
	if err != nil {
		return nil, fmt.Errorf("load changes: %w", err)
	}

	changes, err := pgx.CollectRows(rows, func(row pgx.CollectableRow) (Change, error) {
		var (
			change   Change
			day      *time.Time
			editedAt *time.Time
		)

		scanErr := row.Scan(&change.PostID, &change.Item, &change.PostedAt, &change.RecordedAt, &change.Kind, &change.Slot,
			&change.TargetPostID, &change.TargetItem, &day, &change.Start, &change.Status, &change.Text, &change.TitleKo,
			&change.WithKo, &change.PlaceKo, &change.TagsKo, &change.NoteKo, &editedAt)
		if day != nil {
			change.Day = *day
		}

		if editedAt != nil {
			change.EditedAt = *editedAt
		}

		return change, scanErr
	})
	if err != nil {
		return nil, fmt.Errorf("load changes: %w", err)
	}

	return changes, nil
}
