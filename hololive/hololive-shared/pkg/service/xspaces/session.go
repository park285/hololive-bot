package xspaces

import (
	"context"
	"crypto/aes"
	"crypto/cipher"
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
)

var cookiePattern = regexp.MustCompile(`^[a-zA-Z0-9_-]{16,512}$`)

var ErrDisabled = errors.New("x session store disabled")

type Cookies struct {
	AuthToken string `json:"auth_token"`
	CSRFToken string `json:"ct0"`
}

func (c Cookies) Validate() error {
	if !cookiePattern.MatchString(c.AuthToken) || !cookiePattern.MatchString(c.CSRFToken) {
		return errors.New("invalid X session cookies")
	}

	return nil
}

type Status struct {
	Available      bool       `json:"available"`
	Revision       string     `json:"revision"`
	State          string     `json:"state"`
	CandidateState string     `json:"candidateState"`
	LastError      string     `json:"lastError"`
	CandidateError string     `json:"candidateError"`
	LastCheckedAt  *time.Time `json:"lastCheckedAt"`
	LastSuccessAt  *time.Time `json:"lastSuccessAt"`
	NextCheckAt    *time.Time `json:"nextCheckAt"`
}

type Snapshot struct {
	Revision       int64
	ActiveRevision int64
	Active         *Cookies
	Candidate      *Cookies
	NextCheckAt    *time.Time
	State          string
	LastError      string
}

type Store struct {
	pool *pgxpool.Pool
	aead cipher.AEAD
}

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

func LoadStore(pool *pgxpool.Pool, path string) (store *Store, retErr error) {
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

	// #nosec G407 -- NewGCMWithRandomNonce는 빈 nonce를 요구하고 Seal마다 무작위 nonce를 붙인다.
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

func (s *Store) Status(ctx context.Context) (Status, error) {
	status := Status{Available: s != nil, Revision: "0", State: "unconfigured", CandidateState: "idle"}
	if s == nil {
		status.State = "disabled"
		return status, nil
	}

	err := s.pool.QueryRow(ctx, statusSQL).Scan(&status.Revision, &status.State,
		&status.CandidateState, &status.LastError, &status.CandidateError, &status.LastCheckedAt, &status.LastSuccessAt, &status.NextCheckAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return status, nil
	}

	if err != nil {
		return Status{}, fmt.Errorf("read X session status: %w", err)
	}

	return status, nil
}

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

	result, err := s.pool.Exec(ctx, submitSQL, sealed, revision)
	if err != nil {
		return false, fmt.Errorf("submit X session candidate: %w", err)
	}

	return result.RowsAffected() == 1, nil
}

func (s *Store) Snapshot(ctx context.Context) (Snapshot, error) {
	var (
		snapshot          Snapshot
		active, candidate []byte
	)

	err := s.pool.QueryRow(ctx, snapshotSQL).Scan(&snapshot.Revision, &snapshot.ActiveRevision, &active, &candidate, &snapshot.NextCheckAt, &snapshot.State, &snapshot.LastError)

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

func (s *Store) ResolveCandidate(ctx context.Context, revision int64, rejection string) (bool, error) {
	query := promoteSQL

	if rejection != "" {
		if rejection != "authentication" {
			return false, errors.New("invalid X session rejection")
		}

		query = rejectSQL
	}

	result, err := s.pool.Exec(ctx, query, revision, rejection)
	if err != nil {
		return false, fmt.Errorf("resolve X session candidate: %w", err)
	}

	return result.RowsAffected() == 1, nil
}

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

	result, err := s.pool.Exec(ctx, observeSQL, activeRevision, state, code, next, code == "")
	if err != nil {
		return false, fmt.Errorf("record X session observation: %w", err)
	}

	return result.RowsAffected() == 1, nil
}

func ValidErrorCode(code string) bool {
	switch code {
	case "", "authentication", "authentication_pending", "api_error", "rate_limited", "upstream", "invalid_response", "unexpected_user", "invalid_targets", "invalid_cookies", "forbidden_endpoint", "forbidden_credentials", "redirect", "response_too_large", "collector_failed", "timeout":
		return true
	default:
		return false
	}
}

func (s *Store) CandidateFailure(ctx context.Context, revision int64, code string, next time.Time) error {
	if code == "" || !ValidErrorCode(code) {
		return errors.New("invalid candidate failure code")
	}

	_, err := s.pool.Exec(ctx, candidateFailureSQL, revision, code, next)
	if err != nil {
		return fmt.Errorf("record X candidate failure: %w", err)
	}

	return nil
}
