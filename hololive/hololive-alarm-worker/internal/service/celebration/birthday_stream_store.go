package celebration

import (
	"context"
	jsonv2 "encoding/json/v2"
	"fmt"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type birthdayStreamSessionStore interface {
	FindBirthdaySessions(ctx context.Context, channelIDs []string, windowStartUTC, windowEndUTC, observedSince, observedUntil time.Time) ([]BirthdayStreamSession, error)
	ListPublishedBirthdayStreamEvents(ctx context.Context, keyPrefix string) (map[string]dispatchoutbox.AlarmQueueEnvelope, error)
	FindPublishedCelebrationEvents(ctx context.Context, eventKeys []string) (map[string]dispatchoutbox.AlarmQueueEnvelope, error)
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
	windowStartUTC, windowEndUTC, observedSince, observedUntil time.Time,
) ([]BirthdayStreamSession, error) {
	rows, err := s.db.Query(ctx, findBirthdaySessionsSQL, channelIDs, windowStartUTC, windowEndUTC, observedSince, observedUntil)
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

func (s *PgxStore) ListPublishedBirthdayStreamEvents(
	ctx context.Context,
	keyPrefix string,
) (map[string]dispatchoutbox.AlarmQueueEnvelope, error) {
	pattern := birthdayStreamLikeEscaper.Replace(keyPrefix) + "%"

	events, err := s.findPublishedCelebrationEvents(ctx, listPublishedBirthdayStreamEventsSQL, pattern, validatePublishedBirthdayStreamEvent)
	if err != nil {
		return nil, fmt.Errorf("list published birthday stream events: %w", err)
	}

	return events, nil
}

func (s *PgxStore) FindPublishedCelebrationEvents(
	ctx context.Context,
	eventKeys []string,
) (map[string]dispatchoutbox.AlarmQueueEnvelope, error) {
	if len(eventKeys) == 0 {
		return map[string]dispatchoutbox.AlarmQueueEnvelope{}, nil
	}

	events, err := s.findPublishedCelebrationEvents(ctx, findPublishedCelebrationEventsSQL, eventKeys, validatePublishedCelebrationEvent)
	if err != nil {
		return nil, fmt.Errorf("find published celebration events: %w", err)
	}

	return events, nil
}

func (s *PgxStore) findPublishedCelebrationEvents(
	ctx context.Context,
	query string,
	queryArg any,
	validate func(string, *dispatchoutbox.AlarmQueueEnvelope) error,
) (map[string]dispatchoutbox.AlarmQueueEnvelope, error) {
	events := make(map[string]dispatchoutbox.AlarmQueueEnvelope)

	rows, err := s.db.Query(ctx, query, queryArg)
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

		var envelope dispatchoutbox.AlarmQueueEnvelope

		if err := jsonv2.Unmarshal(payload, &envelope); err != nil {
			return nil, fmt.Errorf("birthday stream runner: decode published event %q: %w", eventKey, err)
		}

		if err := validate(eventKey, &envelope); err != nil {
			return nil, fmt.Errorf("birthday stream runner: validate published event: %w", err)
		}

		events[eventKey] = envelope
	}

	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("birthday stream runner: iterate published events: %w", err)
	}

	return events, nil
}

func validatePublishedCelebrationEvent(eventKey string, envelope *dispatchoutbox.AlarmQueueEnvelope) error {
	if envelope.SourceKind != domain.AlarmDispatchSourceKindCelebration || envelope.Celebration == nil {
		return fmt.Errorf("celebration runner: event %q is not a celebration", eventKey)
	}

	expectedKey := celebrationEventKey(envelope.Celebration)
	if expectedKey != eventKey {
		return fmt.Errorf("celebration runner: event key mismatch: got %q, payload resolves to %q", eventKey, expectedKey)
	}

	return nil
}

func validatePublishedBirthdayStreamEvent(eventKey string, envelope *dispatchoutbox.AlarmQueueEnvelope) error {
	if envelope.SourceKind != domain.AlarmDispatchSourceKindCelebration {
		return fmt.Errorf("birthday stream runner: event %q has source kind %q", eventKey, envelope.SourceKind)
	}

	payload := envelope.Celebration
	if payload == nil || payload.Kind != domain.CelebrationKindBirthdayStream {
		return fmt.Errorf("birthday stream runner: event %q is not a birthday stream", eventKey)
	}

	expectedKey := birthdayStreamEventKey(payload.MemberID, payload.Date, payload.VideoID)
	if expectedKey != eventKey {
		return fmt.Errorf("birthday stream runner: event key mismatch: got %q, payload resolves to %q", eventKey, expectedKey)
	}

	return nil
}

func (s *PgxStore) FindSentRoomsByEventKeys(ctx context.Context, eventKeys []string) (map[string][]string, error) {
	if len(eventKeys) == 0 {
		return map[string][]string{}, nil
	}

	rows, err := s.db.Query(ctx, findSentRoomsByEventKeysSQL, eventKeys)
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
