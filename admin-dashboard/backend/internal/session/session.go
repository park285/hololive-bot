package session

import (
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"math"
	"net"
	"net/url"
	"strings"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/admin-dashboard/internal/auth"
	"github.com/kapu/admin-dashboard/internal/config"
	"github.com/kapu/hololive-shared/pkg/util"
)

const (
	keyPrefix       = "session:admin:"
	familyKeyPrefix = "session:admin:family:"

	valkeyConnectionBufferSize = 16 << 10
	// 단일 서버의 valkey-go 기본값과 같이 client당 2^2개 연결을 사용합니다.
	valkeyPipelineMultiplex = 2
)

type Session struct {
	ID                string    `json:"id"`
	FamilyID          string    `json:"family_id"`
	CreatedAt         time.Time `json:"created_at"`
	ExpiresAt         time.Time `json:"expires_at"`
	AbsoluteExpiresAt time.Time `json:"absolute_expires_at"`
	LastRotatedAt     time.Time `json:"last_rotated_at"`
	RotatedTo         *string   `json:"rotated_to,omitempty"`
}

// validateStoredSession은 새 세대의 필수 필드를 검증하며 구형 레코드를 보정하지 않습니다.
func validateStoredSession(sess *Session, id string) error {
	if sess.ID == "" || sess.ID != id || sess.FamilyID == "" || sess.CreatedAt.IsZero() ||
		sess.ExpiresAt.IsZero() || sess.AbsoluteExpiresAt.IsZero() || sess.LastRotatedAt.IsZero() ||
		(sess.RotatedTo != nil && *sess.RotatedTo == "") {
		return errors.New("invalid stored session record")
	}

	return nil
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

// DisableCache/ForceSingleClient: miniredis 등 RESP2·비클러스터 환경 호환 (hololive-shared cache 컨벤션).
type Options struct {
	DisableCache      bool
	ForceSingleClient bool
}

func NewStore(ctx context.Context, valkeyURL string, cfg *config.SessionConfig) (*Store, error) {
	store, err := NewStoreWithOptions(ctx, valkeyURL, cfg, Options{})
	if err != nil {
		return nil, fmt.Errorf("store with options: %w", err)
	}

	return store, nil
}

func NewStoreWithOptions(ctx context.Context, valkeyURL string, cfg *config.SessionConfig, opts Options) (*Store, error) {
	addr, password, err := parseValkeyAddress(valkeyURL)
	if err != nil {
		return nil, fmt.Errorf("parse valkey address: %w", err)
	}

	// 세션 명령의 연결마다 기본 1 MiB 버퍼를 보관하지 않습니다. 메시지 크기 상한은 바꾸지 않습니다.
	client, err := valkey.NewClient(valkey.ClientOption{
		InitAddress:         []string{addr},
		Password:            password,
		PipelineMultiplex:   valkeyPipelineMultiplex,
		BlockingPoolSize:    64,
		ReadBufferEachConn:  valkeyConnectionBufferSize,
		WriteBufferEachConn: valkeyConnectionBufferSize,
		Dialer:              net.Dialer{Timeout: 5 * time.Second, KeepAlive: 30 * time.Second},
		ConnWriteTimeout:    3 * time.Second,
		DisableCache:        opts.DisableCache,
		ForceSingleClient:   opts.ForceSingleClient,
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

	return &Store{client: client, cfg: *cfg}, nil
}

func (s *Store) Close() {
	if s != nil && s.client != nil {
		s.client.Close()
	}
}

func (s *Store) Create(ctx context.Context) (Session, error) {
	id, err := auth.GenerateSessionID()
	if err != nil {
		return Session{}, fmt.Errorf("generate session ID: %w", err)
	}

	now := time.Now().UTC()
	sess := s.buildSession(id, now)

	data, err := jsonv2.Marshal(sess)
	if err != nil {
		return Session{}, fmt.Errorf("marshal: %w", err)
	}

	ttl := ttlSeconds(sess.ExpiresAt, now)

	result, err := s.evalInt(ctx, createSessionScript,
		[]string{sessionKey(id), familyKey(sess.FamilyID)},
		[]string{string(data), id, fmt.Sprint(ttl)})
	if err != nil {
		return Session{}, fmt.Errorf("eval int: %w", err)
	}

	if result != 1 {
		return Session{}, fmt.Errorf("unexpected session create result: %d", result)
	}

	return sess, nil
}

func (s *Store) Get(ctx context.Context, id string) (Session, bool, error) {
	data, ok, err := s.getRaw(ctx, id)
	if err != nil {
		return Session{}, false, fmt.Errorf("get raw: %w", err)
	}

	if !ok {
		return Session{}, false, nil
	}

	var sess Session

	if err := jsonv2.Unmarshal([]byte(data), &sess); err != nil {
		return Session{}, false, fmt.Errorf("unmarshal: %w", err)
	}

	if err := validateStoredSession(&sess, id); err != nil {
		return Session{}, false, fmt.Errorf("validate stored session: %w", err)
	}

	if isAbsolutelyExpiredAt(&sess, time.Now().UTC()) {
		if err := s.deleteLoadedSession(ctx, &sess); err != nil {
			return Session{}, false, fmt.Errorf("delete loaded session: %w", err)
		}

		return Session{}, false, nil
	}

	return sess, true, nil
}

func (s *Store) Delete(ctx context.Context, id string) error {
	data, ok, err := s.getRaw(ctx, id)
	if err != nil {
		return fmt.Errorf("get raw: %w", err)
	}

	if !ok {
		return nil
	}

	var sess Session

	if err := jsonv2.Unmarshal([]byte(data), &sess); err != nil {
		return fmt.Errorf("unmarshal: %w", err)
	}

	if err := validateStoredSession(&sess, id); err != nil {
		return fmt.Errorf("validate stored session: %w", err)
	}

	if err := s.deleteLoadedSession(ctx, &sess); err != nil {
		return fmt.Errorf("delete loaded session: %w", err)
	}

	return nil
}

// RevokeFamily는 rotation과 원자적으로 직렬화하여 현재 토큰과 family 권한을 폐기한다.
// 이전 회전 marker는 TTL까지 남을 수 있지만 폐기된 교체 토큰으로 인증할 수 없다.
func (s *Store) RevokeFamily(ctx context.Context, familyID string) error {
	if familyID == "" {
		return errors.New("session family ID is required")
	}

	result, err := s.evalInt(ctx, revokeFamilyScript,
		[]string{familyKey(familyID), sessionKey(familyID)}, []string{keyPrefix})
	if err != nil {
		return fmt.Errorf("revoke session family: %w", err)
	}

	if result != 1 {
		return fmt.Errorf("unexpected family revocation result: %d", result)
	}

	return nil
}

func (s *Store) deleteLoadedSession(ctx context.Context, sess *Session) error {
	deleteCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()

	result, err := s.evalInt(deleteCtx, deleteSessionScript,
		[]string{sessionKey(sess.ID), familyKey(sess.FamilyID)},
		[]string{sess.ID})
	if err != nil {
		return fmt.Errorf("eval int: %w", err)
	}

	if result != 0 && result != 1 {
		return fmt.Errorf("unexpected session delete result: %d", result)
	}

	return nil
}

// FamilyActive checks the stable session-family lease. A family lease always
// points at the currently authoritative token ID, so logout, expiry and
// rotation are visible to long-lived WebSocket connections across processes.
func (s *Store) FamilyActive(ctx context.Context, familyID string) (bool, error) {
	if familyID == "" {
		return false, nil
	}

	currentID, err := s.client.Do(ctx, s.client.B().Hget().Key(familyKey(familyID)).Field("token").Build()).ToString()
	if err != nil {
		if util.IsValkeyNil(err) {
			return false, nil
		}

		return false, fmt.Errorf("get string: %w", err)
	}

	out, err := s.tokenHoldsFamily(ctx, currentID, familyID)
	if err != nil {
		return out, fmt.Errorf("token holds family: %w", err)
	}

	return out, nil
}

func (s *Store) tokenHoldsFamily(ctx context.Context, tokenID, familyID string) (bool, error) {
	sess, ok, err := s.Get(ctx, tokenID)
	if err != nil {
		return false, fmt.Errorf("get: %w", err)
	}

	if !ok {
		return false, nil
	}

	return sess.RotatedTo == nil && sess.FamilyID == familyID, nil
}

func (s *Store) buildSession(id string, now time.Time) Session {
	absolute := now.Add(s.cfg.AbsoluteTimeout)

	return Session{
		ID:                id,
		FamilyID:          id,
		CreatedAt:         now,
		ExpiresAt:         cappedExpiresAt(now, s.cfg.ExpiryDuration, absolute),
		AbsoluteExpiresAt: absolute,
		LastRotatedAt:     now,
	}
}

func (s *Store) getRaw(ctx context.Context, id string) (data string, ok bool, err error) {
	// family hash 유실은 인증도 닫아야 사용한 mutation ID가 없는 family로 복구되지 않습니다.
	value, err := s.client.Do(ctx, s.client.B().Eval().Script(readSessionScript).Numkeys(1).Key(sessionKey(id)).Arg(familyKeyPrefix).Build()).ToString()
	if err != nil {
		if util.IsValkeyNil(err) {
			return "", false, nil
		}

		return "", false, fmt.Errorf("to string: %w", err)
	}

	return value, true, nil
}

func (s *Store) evalInt(ctx context.Context, script string, keys, args []string) (int64, error) {
	cmd := s.client.B().Eval().Script(script).Numkeys(int64(len(keys))).Key(keys...).Arg(args...).Build()
	resp := s.client.Do(ctx, cmd)

	if err := resp.Error(); err != nil {
		return 0, fmt.Errorf("error: %w", err)
	}

	out, err := resp.AsInt64()
	if err != nil {
		return out, fmt.Errorf("as int64: %w", err)
	}

	return out, nil
}

func sessionKey(id string) string { return keyPrefix + id }
func familyKey(id string) string  { return familyKeyPrefix + id }

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

func isAbsolutelyExpiredAt(sess *Session, now time.Time) bool {
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
		return "", "", errors.New("VALKEY_URL host is empty")
	}

	return host, password, nil
}

const createSessionScript = `
local session_key = KEYS[1]
local family_key = KEYS[2]
local data = ARGV[1]
local id = ARGV[2]
local ttl = tonumber(ARGV[3])
redis.call('SET', session_key, data, 'EX', ttl)
redis.call('HSET', family_key, 'token', id)
redis.call('EXPIRE', family_key, ttl)
return 1
`

const readSessionScript = `
local data = redis.call('GET', KEYS[1])
if not data then return nil end
local session = cjson.decode(data)
if not session.family_id or session.family_id == '' then return nil end
local token = redis.call('HGET', ARGV[1] .. session.family_id, 'token')
if not token or token ~= (session.rotated_to or session.id) then return nil end
return data
`

const deleteSessionScript = `
local session_key = KEYS[1]
local family_key = KEYS[2]
local id = ARGV[1]
local deleted = redis.call('DEL', session_key)
if redis.call('HGET', family_key, 'token') == id then
  redis.call('DEL', family_key)
end
return deleted
`

// lease의 현재 토큰을 같은 Lua 실행에서 읽고 제거해야 auth snapshot 이후의 rotation도 폐기된다.
const revokeFamilyScript = `
local current_id = redis.call('HGET', KEYS[1], 'token')
if current_id then
  redis.call('DEL', ARGV[1] .. current_id)
end
redis.call('DEL', KEYS[2], KEYS[1])
return 1
`
