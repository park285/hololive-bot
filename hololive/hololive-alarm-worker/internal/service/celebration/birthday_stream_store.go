package celebration

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
)

type birthdayStreamSessionStore interface {
	FindBirthdaySessions(ctx context.Context, channelIDs []string, windowStartUTC, windowEndUTC, seenSince time.Time) ([]BirthdayStreamSession, error)
	ListPublishedEventKeys(ctx context.Context, keyPrefix string) ([]string, error)
	FindSentRoomsByEventKeys(ctx context.Context, eventKeys []string) (map[string][]string, error)
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
) ([]BirthdayStreamSession, error) {
	rows, err := s.db.Query(ctx, mustSQL("birthday_stream_runner_0050_01.sql"), channelIDs, windowStartUTC, windowEndUTC, seenSince)
	if err != nil {
		return nil, fmt.Errorf("birthday stream runner: query sessions: %w", err)
	}
	defer rows.Close()

	sessions := make([]BirthdayStreamSession, 0, birthdayStreamMaxPublishedPerMemberDay*len(channelIDs))

	for rows.Next() {
		var session BirthdayStreamSession

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

func (s *PgxStore) FindSentRoomsByEventKeys(ctx context.Context, eventKeys []string) (map[string][]string, error) {
	if len(eventKeys) == 0 {
		return map[string][]string{}, nil
	}

	rows, err := s.db.Query(ctx, mustSQL("birthday_stream_runner_0104_03.sql"), eventKeys)
	if err != nil {
		return nil, fmt.Errorf("birthday stream runner: query sent birthday rooms: %w", err)
	}
	defer rows.Close()

	roomsByEventKey := make(map[string][]string, len(eventKeys))

	for rows.Next() {
		var (
			eventKey string
			roomID   string
		)

		if err := rows.Scan(&eventKey, &roomID); err != nil {
			return nil, fmt.Errorf("birthday stream runner: scan sent birthday room: %w", err)
		}

		roomsByEventKey[eventKey] = append(roomsByEventKey[eventKey], roomID)
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("birthday stream runner: iterate sent birthday rooms: %w", err)
	}

	return roomsByEventKey, nil
}
