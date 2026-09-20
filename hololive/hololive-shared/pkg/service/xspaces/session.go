// Package xspaces는 스페이스 인증 후보와 활성 세션을 암호화하여 보관한다.
package xspaces

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
	"embed"
	"encoding/hex"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/kapu/hololive-shared/pkg/sqlassets"
)

//go:embed queries/*.sql
var sqlFiles embed.FS

var (
	mustSQL       = sqlassets.MustReader(sqlFiles, "queries")
	cookiePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,512}$`)
)

// ErrDisabled는 세션 저장용 키 파일이 설정되지 않았음을 나타낸다.
var ErrDisabled = errors.New("x session store disabled")

// Cookies는 X 웹 세션의 두 인증 값이다. 응답·로그에 넣지 않는다.
type Cookies struct {
	AuthToken string `json:"auth_token"`
	CSRFToken string `json:"ct0"`
}

// Validate는 헤더 주입과 잘못된 인증 값의 저장을 거부한다.
func (c Cookies) Validate() error {
	if !cookiePattern.MatchString(c.AuthToken) || !cookiePattern.MatchString(c.CSRFToken) {
		return errors.New("invalid X session cookies")
	}

	return nil
}

// Status는 secret을 제외한 관리자용 인증 상태다. 시각 부재는 확인되지 않았음을 뜻한다.
type Status struct {
	Available      bool         `json:"available"`
	Revision       string       `json:"revision"`
	State          string       `json:"state"`
	CandidateState string       `json:"candidateState"`
	LastError      string       `json:"lastError"`
	CandidateError string       `json:"candidateError"`
	LastCheckedAt  *time.Time   `json:"lastCheckedAt"`
	LastSuccessAt  *time.Time   `json:"lastSuccessAt"`
	NextCheckAt    *time.Time   `json:"nextCheckAt"`
	Recovery       *LoginStatus `json:"recovery,omitempty"`
}

// Snapshot은 worker 한 번의 관측에 사용한다. Revision 조건으로 오래된 결과의 반영을 막는다.
type Snapshot struct {
	Revision       int64
	ActiveRevision int64
	Active         *Cookies
	Candidate      *Cookies
	NextCheckAt    *time.Time
	State          string
}

// Store는 API의 후보 제출과 worker의 검증·승격을 공유한다.
type Store struct {
	pool *pgxpool.Pool
	aead cipher.AEAD
}

// NewStore는 256-bit 전용 키를 사용한다. DB에는 nonce와 인증 암호문만 기록한다.
func NewStore(pool *pgxpool.Pool, key []byte) (*Store, error) {
	if pool == nil || len(key) != 32 {
		return nil, errors.New("x session store requires database and 32-byte key")
	}

	block, err := aes.NewCipher(key)
	if err != nil {
		return nil, fmt.Errorf("create X session cipher: %w", err)
	}

	aead, err := cipher.NewGCMWithRandomNonce(block)
	if err != nil {
		return nil, fmt.Errorf("create X session AEAD: %w", err)
	}

	return &Store{pool: pool, aead: aead}, nil
}

// LoadStore는 X_SPACES_KEY_FILE이 없으면 비활성화한다. 설정된 잘못된 키는 오류다.
// 키 파일은 static secret master에서 배포한 private regular file이어야 한다.
func LoadStore(pool *pgxpool.Pool) (store *Store, retErr error) {
	path := os.Getenv("X_SPACES_KEY_FILE")
	if path == "" {
		return nil, ErrDisabled
	}

	if !filepath.IsAbs(path) {
		return nil, errors.New("x session key path must be absolute")
	}

	root, err := os.OpenRoot(filepath.Dir(path))
	if err != nil {
		return nil, errors.New("open x session key directory failed")
	}

	defer func() {
		if closeErr := root.Close(); closeErr != nil {
			retErr = errors.Join(retErr, fmt.Errorf("close X session key directory: %w", closeErr))
		}
	}()

	before, err := root.Lstat(filepath.Base(path))
	if err != nil || !before.Mode().IsRegular() {
		return nil, errors.New("x session key must be a regular file")
	}

	f, err := root.Open(filepath.Base(path))
	if err != nil {
		return nil, errors.New("open X session key failed")
	}
	defer f.Close()

	st, err := f.Stat()
	if err != nil || !os.SameFile(before, st) || !st.Mode().IsRegular() || st.Mode().Perm()&0o077 != 0 || st.Size() > 65 {
		return nil, errors.New("x session key must be a private regular hex key file")
	}

	buffer, err := io.ReadAll(io.LimitReader(f, 66))
	if err != nil {
		return nil, errors.New("read X session key failed")
	}

	key, err := hex.DecodeString(strings.TrimSpace(string(buffer)))
	clear(buffer)

	if err != nil {
		return nil, errors.New("invalid X session key encoding")
	}

	defer clear(key)

	return NewStore(pool, key)
}

func (s *Store) seal(c Cookies) ([]byte, error) {
	if err := c.Validate(); err != nil {
		return nil, err
	}

	raw, err := jsonv2.Marshal(c)
	if err != nil {
		return nil, errors.New("encode X session failed")
	}
	defer clear(raw)

	return s.aead.Seal([]byte{1}, nil, raw, []byte("hololive:x-space-session:v1")), nil
}

func (s *Store) open(raw []byte) (*Cookies, error) {
	if len(raw) == 0 || raw[0] != 1 {
		return nil, errors.New("unsupported X session encoding")
	}

	plain, err := s.aead.Open(nil, nil, raw[1:], []byte("hololive:x-space-session:v1"))
	if err != nil {
		return nil, errors.New("decrypt X session failed")
	}
	defer clear(plain)

	var c Cookies

	if err := jsonv2.Unmarshal(plain, &c, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, errors.New("decode X session failed")
	}

	if err := c.Validate(); err != nil {
		return nil, err
	}

	return &c, nil
}

// Status는 인증 값을 읽지 않고 공개할 수 있는 상태만 조회한다.
func (s *Store) Status(ctx context.Context) (Status, error) {
	status := Status{Available: s != nil, Revision: "0", State: "unconfigured", CandidateState: "idle"}
	if s == nil {
		status.State = "disabled"
		return status, nil
	}

	err := s.pool.QueryRow(ctx, mustSQL("status.sql")).Scan(&status.Revision, &status.State,
		&status.CandidateState, &status.LastError, &status.CandidateError, &status.LastCheckedAt, &status.LastSuccessAt, &status.NextCheckAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return status, nil
	}

	if err != nil {
		return Status{}, fmt.Errorf("read X session status: %w", err)
	}

	recovery, err := s.LoginStatus(ctx)
	if err != nil {
		return Status{}, err
	}

	status.Recovery = &recovery

	return status, nil
}

// Submit은 기대한 세대가 현재와 일치할 때만 암호화한 후보를 저장한다.
// False는 충돌을 뜻하며 기존 인증을 바꾸지 않는다. 저장 성공은 X 인증 검증 성공과 다르다.
func (s *Store) Submit(ctx context.Context, c Cookies, expected string) (bool, error) {
	revision, err := strconv.ParseInt(expected, 10, 64)
	if err != nil || revision < 0 || strconv.FormatInt(revision, 10) != expected {
		return false, errors.New("invalid X session revision")
	}

	if s == nil {
		return false, errors.New("x session store disabled")
	}

	sealed, err := s.seal(c)
	if err != nil {
		return false, err
	}

	result, err := s.pool.Exec(ctx, mustSQL("submit.sql"), sealed, revision)
	if err != nil {
		return false, fmt.Errorf("submit X session candidate: %w", err)
	}

	return result.RowsAffected() == 1, nil
}

// Snapshot은 worker 내부에서 사용할 인증 값을 복호화한다. 호출자는 이를 로그·응답에 넣지 않는다.
func (s *Store) Snapshot(ctx context.Context) (Snapshot, error) {
	var (
		snapshot          Snapshot
		active, candidate []byte
	)

	err := s.pool.QueryRow(ctx, mustSQL("snapshot.sql")).Scan(&snapshot.Revision, &snapshot.ActiveRevision, &active, &candidate, &snapshot.NextCheckAt, &snapshot.State)

	if errors.Is(err, pgx.ErrNoRows) {
		return snapshot, nil
	}

	if err != nil {
		return Snapshot{}, fmt.Errorf("read X session snapshot: %w", err)
	}

	if len(active) > 0 {
		snapshot.Active, err = s.open(active)
		if err != nil {
			return Snapshot{}, err
		}
	}

	if len(candidate) > 0 {
		snapshot.Candidate, err = s.open(candidate)
		if err != nil {
			return Snapshot{}, err
		}
	}

	return snapshot, nil
}

// ResolveCandidate는 검증 성공 시에만 후보를 승격한다. 세대가 다른 결과는 반영하지 않는다.
// Rejection은 authentication만 허용하며 기존 활성 인증을 보존한다.
func (s *Store) ResolveCandidate(ctx context.Context, revision int64, rejection string) (bool, error) {
	query := "promote.sql"

	if rejection != "" {
		if rejection != "authentication" {
			return false, errors.New("invalid X session rejection")
		}

		query = "reject.sql"
	}

	result, err := s.pool.Exec(ctx, mustSQL(query), revision, rejection)
	if err != nil {
		return false, fmt.Errorf("resolve X session candidate: %w", err)
	}

	return result.RowsAffected() == 1, nil
}

// Observe는 현재 활성 세대의 관측만 기록한다. 후보 검증 실패는 활성 세대를 무효화하지 않는다.
// False이면 세대가 달라졌으므로 호출자는 해당 관측으로 발송하지 않는다.
func (s *Store) Observe(ctx context.Context, activeRevision int64, code string, next time.Time) (bool, error) {
	state := "connected"

	if code != "" {
		state = "error"
	}

	if code == "authentication" {
		state = "auth_required"
	}

	if code == "rate_limited" {
		state = "rate_limited"
	}

	if !ValidErrorCode(code) {
		return false, errors.New("invalid X observation error code")
	}

	result, err := s.pool.Exec(ctx, mustSQL("observe.sql"), activeRevision, state, code, next)
	if err != nil {
		return false, fmt.Errorf("record X session observation: %w", err)
	}

	return result.RowsAffected() == 1, nil
}

// ValidErrorCode는 비밀 값이 없는 고정된 helper 오류 코드만 허용한다.
func ValidErrorCode(code string) bool {
	switch code {
	case "", "authentication", "api_error", "rate_limited", "upstream", "invalid_response", "unexpected_user", "invalid_targets", "invalid_cookies", "forbidden_endpoint", "forbidden_credentials", "redirect", "response_too_large", "collector_failed", "timeout":
		return true
	default:
		return false
	}
}

// CandidateFailure는 일시 오류를 기록하고 같은 후보의 다음 검증을 제한한다.
// 네트워크 장애는 인증 거부가 아니므로 후보와 활성 인증 모두 보존한다.
func (s *Store) CandidateFailure(ctx context.Context, revision int64, code string, next time.Time) error {
	if code == "" || !ValidErrorCode(code) {
		return errors.New("invalid candidate failure code")
	}

	_, err := s.pool.Exec(ctx, mustSQL("candidate_failure.sql"), revision, code, next)
	if err != nil {
		return fmt.Errorf("record X candidate failure: %w", err)
	}

	return nil
}
