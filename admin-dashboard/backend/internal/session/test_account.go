package session

import (
	"context"
	"crypto/rand"
	"encoding/base64"
	"encoding/hex"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/crypto/bcrypt"

	"github.com/kapu/admin-dashboard/internal/auth"
	"github.com/kapu/hololive-shared/pkg/util"
)

const testAccountKey = keyPrefix + "test-account"

// TestAccountPrefix는 CLI 발급 계정의 이름 영역이며 일반 관리자와 limiter를 분리할 때 사용합니다.
const TestAccountPrefix = "test-"

// DefaultTestAccountTTL은 운영자가 명시하지 않은 임시 계정의 유효 기간입니다.
const DefaultTestAccountTTL = 15 * time.Minute

// MaxTestAccountTTL은 발급과 저장 시 적용하는 임시 계정의 최대 유효 기간입니다.
const MaxTestAccountTTL = time.Hour

var (
	// ErrTestAccountExists는 기존 발급을 변경하지 않고 중복 발급을 거부한 결과입니다.
	ErrTestAccountExists = errors.New("a test account is already active")
	// ErrTestAccountMismatch는 요청한 식별자가 현재 계정과 달라 폐기하지 않은 결과입니다.
	ErrTestAccountMismatch = errors.New("the current test account has a different username")
)

// TestAccount는 Valkey에 저장되는 조회 전용 계정입니다. 해시는 로그나 API로 내보내지 않습니다.
type TestAccount struct {
	Username      string `json:"username"`
	PasswordHash  string `json:"password_hash"`
	ExpiresAtUnix int64  `json:"expires_at_unix"`
}

// TestCredentials는 발급 직후 비공개 파일로 전달할 자격증명이며 Valkey에는 저장하지 않습니다.
type TestCredentials struct {
	Username      string `json:"username"`
	Password      string `json:"password"`
	ExpiresAtUnix int64  `json:"expires_at_unix"`
}

// NewTestAccount는 외부 상태 변경 없이 무작위 이름·암호와 bcrypt 레코드를 준비합니다.
// TTL은 초 단위 1~60분이며 cost는 기존 관리자와 같은 강도로 지정합니다.
func NewTestAccount(ttl time.Duration, cost int) (TestAccount, TestCredentials, error) {
	if ttl < time.Minute || ttl > MaxTestAccountTTL || ttl%time.Second != 0 || cost < 10 || cost > bcrypt.MaxCost {
		return TestAccount{}, TestCredentials{}, errors.New("invalid test account TTL or bcrypt cost")
	}

	var (
		name     [16]byte
		password [32]byte
	)

	if _, err := rand.Read(name[:]); err != nil {
		return TestAccount{}, TestCredentials{}, fmt.Errorf("generate test account name: %w", err)
	}

	if _, err := rand.Read(password[:]); err != nil {
		return TestAccount{}, TestCredentials{}, fmt.Errorf("generate test account password: %w", err)
	}

	plain := base64.RawURLEncoding.EncodeToString(password[:])

	hash, err := bcrypt.GenerateFromPassword([]byte(plain), cost)
	if err != nil {
		return TestAccount{}, TestCredentials{}, fmt.Errorf("hash test account password: %w", err)
	}

	account := TestAccount{Username: TestAccountPrefix + hex.EncodeToString(name[:]), PasswordHash: string(hash), ExpiresAtUnix: time.Now().Add(ttl).Unix()}

	return account, TestCredentials{Username: account.Username, Password: plain, ExpiresAtUnix: account.ExpiresAtUnix}, nil
}

func validTestAccountName(username string) bool {
	suffix, ok := strings.CutPrefix(username, TestAccountPrefix)
	if !ok || len(suffix) != 32 {
		return false
	}

	_, err := hex.DecodeString(suffix)

	return err == nil && suffix == strings.ToLower(suffix)
}

func validateTestAccount(account TestAccount) error {
	if !validTestAccountName(account.Username) || account.ExpiresAtUnix <= 0 {
		return errors.New("invalid test account record")
	}

	cost, err := bcrypt.Cost([]byte(account.PasswordHash))
	if err != nil || cost < 10 {
		return errors.New("invalid test account password hash")
	}

	return nil
}

// IssueTestAccount는 기존 계정을 덮지 않고 한 개의 만료형 계정을 생성합니다.
// 호출자는 I/O 전 자격증명을 비공개 파일에 보존해야 하며 불명 결과를 자동 재발급하면 안 됩니다.
func (s *Store) IssueTestAccount(ctx context.Context, account TestAccount) error {
	if err := validateTestAccount(account); err != nil {
		return err
	}

	seconds := account.ExpiresAtUnix - time.Now().Unix()
	if seconds < 1 || seconds > int64(MaxTestAccountTTL/time.Second) {
		return errors.New("test account expiry is outside the allowed window")
	}

	data, err := jsonv2.Marshal(account)
	if err != nil {
		return fmt.Errorf("marshal test account: %w", err)
	}

	result, err := s.evalInt(ctx, issueTestAccountScript, []string{testAccountKey}, []string{string(data), fmt.Sprint(seconds)})
	if err != nil {
		return fmt.Errorf("issue test account outcome unknown: %w", err)
	}

	if result == 0 {
		return ErrTestAccountExists
	}

	if result != 1 {
		return fmt.Errorf("unexpected test account issuance result: %d", result)
	}

	return nil
}

// CurrentTestAccount는 현재 레코드를 검증합니다. 부재·만료와 저장소 오류를 구분합니다.
func (s *Store) CurrentTestAccount(ctx context.Context) (TestAccount, bool, error) {
	account, _, found, err := s.readTestAccount(ctx)
	return account, found, err
}

func (s *Store) readTestAccount(ctx context.Context) (TestAccount, string, bool, error) {
	data, err := s.client.Do(ctx, s.client.B().Get().Key(testAccountKey).Build()).ToString()
	if err != nil {
		if util.IsValkeyNil(err) {
			return TestAccount{}, "", false, nil
		}

		return TestAccount{}, "", false, fmt.Errorf("read test account: %w", err)
	}

	var account TestAccount

	if err := jsonv2.Unmarshal([]byte(data), &account); err != nil {
		return TestAccount{}, "", false, errors.New("invalid stored test account JSON")
	}

	if err := validateTestAccount(account); err != nil {
		return TestAccount{}, "", false, err
	}

	if account.ExpiresAtUnix > time.Now().Add(MaxTestAccountTTL).Unix() {
		return TestAccount{}, "", false, errors.New("stored test account exceeds the maximum lifetime")
	}

	if time.Now().Unix() >= account.ExpiresAtUnix {
		return TestAccount{}, "", false, nil
	}

	return account, data, true, nil
}

// RevokeTestAccount는 식별자가 일치하는 현재 계정만 원자적으로 폐기합니다.
// 부재는 changed=false이며, 폐기 이후 모든 해당 세션과 WS family 조회는 실패합니다.
func (s *Store) RevokeTestAccount(ctx context.Context, username string) (changed bool, err error) {
	if !validTestAccountName(username) {
		return false, errors.New("invalid test account username")
	}

	result, err := s.evalInt(ctx, revokeTestAccountScript, []string{testAccountKey}, []string{username})
	if err != nil {
		return false, fmt.Errorf("revoke test account outcome unknown: %w", err)
	}

	switch result {
	case 0:
		return false, nil
	case 1:
		return true, nil
	case -1:
		return false, ErrTestAccountMismatch
	default:
		return false, fmt.Errorf("unexpected test account revocation result: %d", result)
	}
}

// CreateTestSession는 현재 계정과 같은 레코드일 때만 제한된 세션을 생성합니다.
// 암호 검사와 생성 사이의 폐기·재발급은 found=false로 거부합니다.
func (s *Store) CreateTestSession(ctx context.Context, expected TestAccount) (sess Session, found bool, err error) {
	account, raw, found, err := s.readTestAccount(ctx)
	if err != nil || !found {
		return Session{}, false, err
	}

	if account != expected {
		return Session{}, false, nil
	}

	id, err := auth.GenerateSessionID()
	if err != nil {
		return Session{}, false, fmt.Errorf("generate test session ID: %w", err)
	}

	now := time.Now().UTC()

	sess = s.buildSession(auth.TestSessionPrefix+id, now)
	sess.TestAccount = account.Username

	if expiry := time.Unix(account.ExpiresAtUnix, 0); expiry.Before(sess.AbsoluteExpiresAt) {
		sess.AbsoluteExpiresAt = expiry
	}

	sess.ExpiresAt = cappedExpiresAt(now, s.cfg.ExpiryDuration, sess.AbsoluteExpiresAt)

	data, err := jsonv2.Marshal(sess)
	if err != nil {
		return Session{}, false, fmt.Errorf("marshal test session: %w", err)
	}

	result, err := s.evalInt(ctx, createTestSessionScript,
		[]string{sessionKey(sess.ID), familyKey(sess.FamilyID), testAccountKey},
		[]string{string(data), sess.ID, fmt.Sprint(ttlSeconds(sess.ExpiresAt, now)), raw})
	if err != nil {
		return Session{}, false, fmt.Errorf("create test session: %w", err)
	}

	if result == 0 {
		return Session{}, false, nil
	}

	if result != 1 {
		return Session{}, false, fmt.Errorf("unexpected test session creation result: %d", result)
	}

	return sess, true, nil
}

const issueTestAccountScript = `
if redis.call('EXISTS', KEYS[1]) == 1 then return 0 end
redis.call('SET', KEYS[1], ARGV[1], 'EX', ARGV[2])
return 1
`

// 읽기·갱신·회전과 같은 Lua 실행에서 확인하여 폐기와 경합해도 권한을 연장하지 않습니다.
const testAccountAccessScript = `
local function testAccountActive(session, account_key, max_lifetime_seconds)
  if not session.test_account then return true end
  if type(session.test_account) ~= 'string' or session.test_account == '' then
    error('invalid session test account')
  end
  local data = redis.call('GET', account_key)
  if not data then return false end
  local account = cjson.decode(data)
  if type(account.username) ~= 'string' or #account.username ~= 37 or
      not string.match(account.username, '^test%-[0-9a-f]+$') or
      type(account.password_hash) ~= 'string' or #account.password_hash ~= 60 or
      not string.match(account.password_hash, '^%$2[aby]%$[0-3][0-9]%$[./A-Za-z0-9]+$') or
      tonumber(string.sub(account.password_hash, 5, 6)) < 10 or
      tonumber(string.sub(account.password_hash, 5, 6)) > 31 or
      type(account.expires_at_unix) ~= 'number' or account.expires_at_unix <= 0 or
      account.expires_at_unix % 1 ~= 0 then
    error('invalid test account record')
  end
  local now = tonumber(redis.call('TIME')[1])
  if account.expires_at_unix > now + max_lifetime_seconds then error('test account lifetime exceeds maximum') end
  return account.username == session.test_account and account.expires_at_unix > now
end
`

const revokeTestAccountScript = `
local data = redis.call('GET', KEYS[1])
if not data then return 0 end
local account = cjson.decode(data)
if type(account.username) ~= 'string' then return redis.error_reply('invalid test account record') end
if account.username ~= ARGV[1] then return -1 end
return redis.call('DEL', KEYS[1])
`

const createTestSessionScript = `
local account_data = redis.call('GET', KEYS[3])
if not account_data or account_data ~= ARGV[4] then return 0 end
local account = cjson.decode(account_data)
if account.expires_at_unix <= tonumber(redis.call('TIME')[1]) then return 0 end
` + createSessionScript
