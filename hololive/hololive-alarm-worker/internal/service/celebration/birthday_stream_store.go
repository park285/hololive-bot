package celebration

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type birthdayStreamSessionStore interface {
	FindBirthdaySessions(ctx context.Context, channelIDs []string, windowStartUTC, windowEndUTC, seenSince time.Time) ([]BirthdayStreamSession, error)
	ListPublishedEventKeys(ctx context.Context, keyPrefix string) ([]string, error)
	FindPublishedBirthdayStreamEvents(ctx context.Context, eventKeys []string) (map[string]domain.AlarmQueueEnvelope, error)
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

func (s *PgxStore) FindPublishedBirthdayStreamEvents(
	ctx context.Context,
	eventKeys []string,
) (map[string]domain.AlarmQueueEnvelope, error) {
	events := make(map[string]domain.AlarmQueueEnvelope, len(eventKeys))
	if len(eventKeys) == 0 {
		return events, nil
	}

	rows, err := s.db.Query(ctx, mustSQL("birthday_stream_runner_0108_04.sql"), eventKeys)
	if err != nil {
		return nil, fmt.Errorf("birthday stream runner: query published events: %w", err)
	}
	defer rows.Close()

	for rows.Next() {
		var (
			eventKey             string
			payloadSchemaVersion int16
			payload              []byte
		)

		if err := rows.Scan(&eventKey, &payloadSchemaVersion, &payload); err != nil {
			return nil, fmt.Errorf("birthday stream runner: scan published event: %w", err)
		}

		if payloadSchemaVersion != 1 {
			return nil, fmt.Errorf("birthday stream runner: event %q has unsupported payload schema version %d", eventKey, payloadSchemaVersion)
		}

		var envelope domain.AlarmQueueEnvelope

		if err := jsonv2.Unmarshal(payload, &envelope); err != nil {
			return nil, fmt.Errorf("birthday stream runner: decode published event %q: %w", eventKey, err)
		}

		if err := validatePublishedBirthdayStreamEvent(eventKey, &envelope); err != nil {
			return nil, fmt.Errorf("birthday stream runner: validate published event: %w", err)
		}

		events[eventKey] = envelope
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("birthday stream runner: iterate published events: %w", err)
	}

	return events, nil
}

func validatePublishedBirthdayStreamEvent(eventKey string, envelope *domain.AlarmQueueEnvelope) error {
	if envelope.SourceKind != domain.AlarmDispatchSourceKindCelebration {
		return fmt.Errorf("birthday stream runner: event %q has source kind %q", eventKey, envelope.SourceKind)
	}

	payload := envelope.Celebration
	if payload == nil || payload.Kind != domain.CelebrationKindBirthdayStream {
		return fmt.Errorf("birthday stream runner: event %q is not a birthday stream", eventKey)
	}

	expectedKey := birthdayStreamEventKey(payload.ChannelID, payload.Date, payload.VideoID)
	if expectedKey != eventKey {
		return fmt.Errorf("birthday stream runner: event key mismatch: got %q, payload resolves to %q", eventKey, expectedKey)
	}

	return nil
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
