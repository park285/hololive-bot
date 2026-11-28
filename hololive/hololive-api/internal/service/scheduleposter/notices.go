package scheduleposter

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/park285/shared-go/postgres/pgxdb"
)

type NoticeKind string

const (
	NoticePublished NoticeKind = "published"
	NoticeChanged   NoticeKind = "changed"
)

type NoticeStatus string

const (
	NoticeSent    NoticeStatus = "sent"
	NoticeSkipped NoticeStatus = "skipped"
)

type Notice struct {
	ID        int64
	Kind      NoticeKind
	PosterID  int64
	ChannelID string
	PostID    int64
	CreatedAt time.Time
	ClaimedAt time.Time
}

func insertNotice(ctx context.Context, tx pgx.Tx, kind NoticeKind, posterID int64, postID *int64) error {
	if _, err := pgxdb.Exec(ctx, tx, insertNoticeSQL, string(kind), posterID, postID); err != nil {
		return fmt.Errorf("insert %s notice: %w", kind, err)
	}

	return nil
}

func (s *Store) ClaimNotice(ctx context.Context, lease time.Duration) (Notice, bool, error) {
	var (
		notice Notice
		kind   string
	)

	err := pgxdb.QueryRow(ctx, s.pool, claimNoticeSQL, lease.Seconds()).
		Scan(&notice.ID, &kind, &notice.PosterID, &notice.ChannelID, &notice.PostID, &notice.CreatedAt, &notice.ClaimedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return Notice{}, false, nil
	}

	if err != nil {
		return Notice{}, false, fmt.Errorf("claim schedule poster notice: %w", err)
	}

	notice.Kind = NoticeKind(kind)

	return notice, true, nil
}

func (s *Store) NoticeChangePosts(ctx context.Context, notice Notice) ([]int64, error) {
	var posts []int64

	if err := pgxdb.QueryRow(ctx, s.pool, noticeChangePostsSQL, notice.ID, notice.ClaimedAt).Scan(&posts); err != nil {
		return nil, fmt.Errorf("list schedule poster notice change posts: %w", err)
	}

	return posts, nil
}

func (s *Store) SettleNotice(ctx context.Context, notice Notice, status NoticeStatus, rooms int) error {
	var sentRooms *int

	if status == NoticeSent {
		sentRooms = &rooms
	}

	if _, err := pgxdb.Exec(ctx, s.pool, settleNoticeSQL, notice.ID, string(status), sentRooms,
		notice.ChannelID, notice.PosterID, notice.ClaimedAt); err != nil {
		return fmt.Errorf("settle schedule poster notice: %w", err)
	}

	return nil
}
