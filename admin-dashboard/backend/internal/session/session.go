package session

import (
	"context"
	"fmt"
	"math"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/admin-dashboard/internal/auth"
	"github.com/kapu/admin-dashboard/internal/config"
	"github.com/park285/shared-go/pkg/json"
)

const keyPrefix = "session:admin:"

type Session struct {
	ID                string    `json:"id"`
	CreatedAt         time.Time `json:"created_at"`
	ExpiresAt         time.Time `json:"expires_at"`
	AbsoluteExpiresAt time.Time `json:"absolute_expires_at"`
	LastRotatedAt     time.Time `json:"last_rotated_at"`
	RotatedTo         *string   `json:"rotated_to,omitempty"`
}

type RefreshKind string

const (
	RefreshRefreshed       RefreshKind = "refreshed"
	RefreshIdleShortened   RefreshKind = "idle_shortened"
	RefreshRotated         RefreshKind = "rotated"
	RefreshMissing         RefreshKind = "missing"
	RefreshNotRefreshable  RefreshKind = "not_refreshable"
	RefreshAbsoluteExpired RefreshKind = "absolute_expired"
)

type RefreshResult struct {
	Kind    RefreshKind
	Session *Session
}

type Store struct {
	client valkey.Client
	cfg    config.SessionConfig
}

// DisableCache/ForceSingleClient: miniredis 등 RESP2·비클러스터 환경 호환 (hololive-shared cache 컨벤션)
type Options struct {
	DisableCache      bool
	ForceSingleClient bool
}

func NewStore(ctx context.Context, valkeyURL string, cfg config.SessionConfig) (*Store, error) {
	return NewStoreWithOptions(ctx, valkeyURL, cfg, Options{})
}

func NewStoreWithOptions(ctx context.Context, valkeyURL string, cfg config.SessionConfig, opts Options) (*Store, error) {
	addr, password, err := parseValkeyAddress(valkeyURL)
	if err != nil {
		return nil, err
	}
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:       []string{addr},
		Password:          password,
		PipelineMultiplex: 4,
		BlockingPoolSize:  64,
		Dialer:            net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second},
		ConnWriteTimeout:  3 * time.Second,
		DisableCache:      opts.DisableCache,
		ForceSingleClient: opts.ForceSingleClient,
	})
	if err != nil {
		return nil, fmt.Errorf("create valkey client: %w", err)
	}
	pingCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	if err := client.Do(pingCtx, client.B().Ping().Build()).Error(); err != nil {
		client.Close()
		return nil, fmt.Errorf("valkey ping failed: %w", err)
	}
	return &Store{client: client, cfg: cfg}, nil
}

func (s *Store) Close() {
	if s != nil && s.client != nil {
		s.client.Close()
	}
}

func (s *Store) Create(ctx context.Context) (Session, error) {
	id, err := auth.GenerateSessionID()
	if err != nil {
		return Session{}, err
	}
	now := time.Now().UTC()
	session := s.buildSession(id, now)
	data, err := json.Marshal(session)
	if err != nil {
		return Session{}, err
	}
	if err := s.client.Do(ctx, s.client.B().Set().Key(sessionKey(id)).Value(string(data)).ExSeconds(ttlSeconds(session.ExpiresAt, now)).Build()).Error(); err != nil {
		return Session{}, err
	}
	return session, nil
}

func (s *Store) Get(ctx context.Context, id string) (*Session, error) {
	data, ok, err := s.getRaw(ctx, id)
	if err != nil || !ok {
		return nil, err
	}
	var sess Session
	if err := json.Unmarshal([]byte(data), &sess); err != nil {
		return nil, err
	}
	if sess.LastRotatedAt.IsZero() {
		sess.LastRotatedAt = sess.CreatedAt
	}
	if isAbsolutelyExpiredAt(sess, time.Now().UTC()) {
		_ = s.Delete(ctx, id)
		return nil, nil
	}
	return &sess, nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	deleteCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	return s.client.Do(deleteCtx, s.client.B().Del().Key(sessionKey(id)).Build()).Error()
}

func (s *Store) buildSession(id string, now time.Time) Session {
	absolute := now.Add(s.cfg.AbsoluteTimeout)
	return Session{
		ID:                id,
		CreatedAt:         now,
		ExpiresAt:         cappedExpiresAt(now, s.cfg.ExpiryDuration, absolute),
		AbsoluteExpiresAt: absolute,
		LastRotatedAt:     now,
	}
}

func (s *Store) getRaw(ctx context.Context, id string) (string, bool, error) {
	resp := s.client.Do(ctx, s.client.B().Get().Key(sessionKey(id)).Build())
	if err := resp.Error(); err != nil {
		if isValkeyNil(err) {
			return "", false, nil
		}
		return "", false, err
	}
	value, err := resp.ToString()
	if err != nil {
		if isValkeyNil(err) {
			return "", false, nil
		}
		return "", false, err
	}
	return value, true, nil
}

func (s *Store) evalInt(ctx context.Context, script string, keys []string, args []string) (int64, error) {
	cmd := s.client.B().Eval().Script(script).Numkeys(int64(len(keys))).Key(keys...).Arg(args...).Build()
	resp := s.client.Do(ctx, cmd)
	if err := resp.Error(); err != nil {
		return 0, err
	}
	return resp.AsInt64()
}

func sessionKey(id string) string { return keyPrefix + id }

func cappedExpiresAt(now time.Time, ttl time.Duration, absolute time.Time) time.Time {
	candidate := now.Add(ttl)
	if candidate.After(absolute) {
		return absolute
	}
	return candidate
}

func ttlSeconds(expiresAt, now time.Time) int64 {
	seconds := int64(math.Ceil(expiresAt.Sub(now).Seconds()))
	if seconds < 1 {
		return 1
	}
	return seconds
}

func isAbsolutelyExpiredAt(sess Session, now time.Time) bool {
	return !now.Before(sess.AbsoluteExpiresAt)
}

func parseValkeyAddress(value string) (addr, password string, err error) {
	userinfo, host, ok := strings.Cut(value, "@")
	if !ok {
		return value, "", nil
	}
	password = strings.TrimPrefix(userinfo, ":")
	if decoded, decodeErr := url.QueryUnescape(password); decodeErr == nil {
		password = decoded
	}
	if host == "" {
		return "", "", fmt.Errorf("VALKEY_URL host is empty")
	}
	return host, password, nil
}

func isValkeyNil(err error) bool {
	for err != nil {
		if valkey.IsValkeyNil(err) {
			return true
		}
		err = unwrap(err)
	}
	return false
}

type unwrapper interface{ Unwrap() error }

func unwrap(err error) error {
	if u, ok := err.(unwrapper); ok {
		return u.Unwrap()
	}
	return nil
}
