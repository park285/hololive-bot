package celebration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type birthdayStreamSessionStore interface {
	FindBirthdaySessions(ctx context.Context, channelIDs []string, windowStartUTC, windowEndUTC, seenSince time.Time) ([]birthdayStreamSession, error)
	ListPublishedEventKeys(ctx context.Context, keyPrefix string) ([]string, error)
}

type BirthdayStreamQuerier interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type PgxStore struct {
	db BirthdayStreamQuerier
}

func NewPgxStore(db BirthdayStreamQuerier) *PgxStore {
	return &PgxStore{db: db}
}

func (s *PgxStore) FindBirthdaySessions(
	ctx context.Context,
	channelIDs []string,
	windowStartUTC, windowEndUTC, seenSince time.Time,
) ([]birthdayStreamSession, error) {
	rows, err := s.db.Query(ctx, mustSQL("birthday_stream_runner_0050_01.sql"), channelIDs, windowStartUTC, windowEndUTC, seenSince)
	if err != nil {
		return nil, fmt.Errorf("birthday stream runner: query sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]birthdayStreamSession, 0, birthdayStreamMaxPublishedPerMemberDay*len(channelIDs))
	for rows.Next() {
		var session birthdayStreamSession
		if err := rows.Scan(
			&session.VideoID,
			&session.ChannelID,
			&session.Title,
			&session.Status,
			&session.ScheduledStart,
			&session.StartedAt,
		); err != nil {
			return nil, fmt.Errorf("birthday stream runner: scan session: %w", err)
		}
		sessions = append(sessions, session)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("birthday stream runner: iterate sessions: %w", err)
	}
	return sessions, nil
}

var birthdayStreamLikeEscaper = strings.NewReplacer(`\`, `\\`, `%`, `\%`, `_`, `\_`)

func (s *PgxStore) ListPublishedEventKeys(ctx context.Context, keyPrefix string) ([]string, error) {
	pattern := birthdayStreamLikeEscaper.Replace(keyPrefix) + "%"
	rows, err := s.db.Query(ctx, mustSQL("birthday_stream_runner_0081_02.sql"), pattern)
	if err != nil {
		return nil, fmt.Errorf("birthday stream runner: query published event keys: %w", err)
	}
	defer rows.Close()

	var keys []string
	for rows.Next() {
		var key string
		if err := rows.Scan(&key); err != nil {
			return nil, fmt.Errorf("birthday stream runner: scan published event key: %w", err)
		}
		keys = append(keys, key)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("birthday stream runner: iterate published event keys: %w", err)
	}
	return keys, nil
}
