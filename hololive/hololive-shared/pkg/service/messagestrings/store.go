package messagestrings

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

const (
	NamespaceOrg       = "org"
	NamespaceAlarmType = "alarmtype"
	NamespaceMisc      = "misc"
	NamespaceError     = "error"
	NamespaceNotify    = "notify"
	NamespaceCalendar  = "calendar"
	NamespaceTimeFmt   = "timefmt"
)

type queryRunner interface {
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
}

type Store struct {
	pool   queryRunner
	logger *slog.Logger
	mu     sync.RWMutex
	cache  map[string]map[string]string
	loaded bool
}

func NewStore(pool *pgxpool.Pool, logger *slog.Logger) *Store {
	initMetrics()

	return &Store{pool: pool, logger: logger}
}

func (s *Store) Load(ctx context.Context) error {
	if s == nil {
		return errors.New("message strings store is nil")
	}

	if err := s.load(ctx); err != nil {
		observeLoadFailure()

		return fmt.Errorf("load message strings: %w", err)
	}

	return nil
}

type Requirements struct {
	Keys       []Key
	Namespaces []string
}

func (s *Store) Validate(requirements Requirements) error {
	if s == nil {
		return errors.New("message strings store is nil")
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	if !s.loaded {
		return errors.New("message strings are not loaded")
	}

	var missing []string

	for _, key := range requirements.Keys {
		if strings.TrimSpace(s.cache[key.Namespace][key.Name]) == "" {
			missing = append(missing, key.String())
		}
	}

	for _, namespace := range requirements.Namespaces {
		if len(s.cache[namespace]) == 0 {
			missing = append(missing, namespace+"/*")
		}
	}

	if len(missing) > 0 {
		return fmt.Errorf("message strings missing required entries: %s", strings.Join(missing, ", "))
	}

	return nil
}

func (s *Store) Text(key Key) string {
	value, _ := s.Lookup(key.Namespace, key.Name)

	return value
}

func (s *Store) Lookup(namespace, key string) (string, bool) {
	if s == nil {
		observeLookupMiss(lookupMissReasonUnloaded, namespace)

		return "", false
	}

	s.mu.RLock()

	loaded := s.loaded
	value := s.cache[namespace][key]
	s.mu.RUnlock()

	if value != "" {
		return value, true
	}

	reason := lookupMissReasonMissing

	if !loaded {
		reason = lookupMissReasonUnloaded
	}

	observeLookupMiss(reason, namespace)

	return "", false
}

func (s *Store) GetMap(namespace string) map[string]string {
	if s == nil {
		return nil
	}

	s.mu.RLock()
	defer s.mu.RUnlock()

	src := s.cache[namespace]
	if src == nil {
		return nil
	}

	out := make(map[string]string, len(src))
	maps.Copy(out, src)

	return out
}

func (s *Store) load(ctx context.Context) error {
	rows, err := s.pool.Query(ctx, loadSQL)
	if err != nil {
		return fmt.Errorf("query message_strings: %w", err)
	}
	defer rows.Close()

	next := make(map[string]map[string]string)

	for rows.Next() {
		var namespace, key, value string

		if scanErr := rows.Scan(&namespace, &key, &value); scanErr != nil {
			return fmt.Errorf("scan message_strings: %w", scanErr)
		}

		values, ok := next[namespace]
		if !ok {
			values = make(map[string]string)
			next[namespace] = values
		}

		values[key] = value
	}

	if err := rows.Err(); err != nil {
		return fmt.Errorf("iterate message_strings: %w", err)
	}

	s.mu.Lock()

	s.cache = next
	s.loaded = true
	s.mu.Unlock()

	return nil
}
