package xspaces

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"

	"github.com/kapu/hololive-shared/pkg/pgxutil"
)

// LoginStatus는 비밀번호나 쿠키를 포함하지 않는 자동 로그인 시도 상태입니다.
type LoginStatus struct {
	State         string     `json:"state"`
	LastError     string     `json:"lastError"`
	LastAttemptAt *time.Time `json:"lastAttemptAt"`
}

type loginAttempt struct {
	id            int64
	configuration int64
	revision      int64
	submitted     *int64
	state         string
	errorCode     string
	started       time.Time
}

func scanLogin(row pgx.Row) (loginAttempt, error) {
	var attempt loginAttempt

	err := row.Scan(&attempt.id, &attempt.configuration, &attempt.revision, &attempt.submitted, &attempt.state, &attempt.errorCode, &attempt.started)

	return attempt, err
}

// LoginStatus는 마지막 시도만 읽습니다. 끊긴 실행은 읽기 시에도 결과 불명으로 표시합니다.
func (s *Store) LoginStatus(ctx context.Context) (LoginStatus, error) {
	if s == nil {
		return LoginStatus{State: "disabled"}, nil
	}

	a, err := scanLogin(s.pool.QueryRow(ctx, mustSQL("login_latest.sql")))
	if errors.Is(err, pgx.ErrNoRows) {
		return LoginStatus{State: "disabled"}, nil
	}

	if err != nil {
		return LoginStatus{}, fmt.Errorf("read X login status: %w", err)
	}

	if a.state == "running" && time.Since(a.started) > 5*time.Minute {
		a.state, a.errorCode = "outcome_unknown", "interrupted"
	}

	return LoginStatus{State: a.state, LastError: a.errorCode, LastAttemptAt: &a.started}, nil
}

// ReconcileLogin은 검증 결과와 종료된 실행의 결과 불명을 원장에 반영합니다. 재로그인하지 않습니다.
func (s *Store) ReconcileLogin(ctx context.Context) error {
	for _, query := range []string{"login_interrupted.sql", "login_reconcile.sql"} {
		if _, err := s.pool.Exec(ctx, mustSQL(query)); err != nil {
			return fmt.Errorf("reconcile X login: %w", err)
		}
	}

	return nil
}

// ClaimLogin은 로그인 전에 소유권을 지속 기록합니다. 0은 후보 검증·수동 변경·시도 제한으로 미실행입니다.
// 설정 세대 변경은 운영자가 새 계정/비밀번호 또는 중단된 시도를 명시적으로 다시 설정한 경우만 허용합니다.
func (s *Store) ClaimLogin(ctx context.Context, configuration int64) (_ int64, retErr error) {
	if configuration <= 0 {
		return 0, errors.New("invalid X login configuration revision")
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return 0, fmt.Errorf("begin X login claim: %w", err)
	}

	defer func() {
		if rollbackErr := pgxutil.Rollback(ctx, tx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			retErr = errors.Join(retErr, rollbackErr)
		}
	}()

	if _, err = tx.Exec(ctx, mustSQL("login_session_init.sql")); err != nil {
		return 0, fmt.Errorf("initialize X login session: %w", err)
	}

	var (
		revision, activeRevision int64
		state                    string
		candidate                bool
	)

	if err = tx.QueryRow(ctx, mustSQL("login_session_lock.sql")).Scan(&revision, &activeRevision, &state, &candidate); err != nil {
		return 0, fmt.Errorf("lock X login session: %w", err)
	}

	if candidate {
		return 0, nil
	}

	last, err := scanLogin(tx.QueryRow(ctx, mustSQL("login_latest.sql")))
	if err != nil && !errors.Is(err, pgx.ErrNoRows) {
		return 0, fmt.Errorf("read X login attempt: %w", err)
	}

	// 이전 시도가 없으면 설정 세대 0이므로 최초의 명시적 계정 설정으로 처리합니다.
	if !last.allows(configuration, revision, activeRevision, state) {
		return 0, nil
	}

	var hourly, daily int

	if err = tx.QueryRow(ctx, mustSQL("login_budget.sql")).Scan(&hourly, &daily); err != nil {
		return 0, fmt.Errorf("read X login budget: %w", err)
	}

	if hourly > 0 || daily >= 3 {
		return 0, nil
	}

	var id int64

	if err = tx.QueryRow(ctx, mustSQL("login_claim.sql"), configuration, revision).Scan(&id); err != nil {
		return 0, fmt.Errorf("claim X login: %w", err)
	}

	if err = tx.Commit(ctx); err != nil {
		return 0, fmt.Errorf("commit X login claim unconfirmed: %w", err)
	}

	return id, nil
}

func (a loginAttempt) allows(configuration, revision, active int64, state string) bool {
	if configuration < a.configuration || a.state == "running" || a.state == "submitted" {
		return false
	}

	if configuration > a.configuration {
		return true
	}

	return a.state == "connected" && state == "auth_required" && a.submitted != nil && active == *a.submitted && revision == active
}

// FinishLogin은 후보 저장과 시도 완료를 한 트랜잭션으로 반영합니다. 실제 활성화는 수집기의 검증만 수행합니다.
// 원격 로그인 성공 후 저장 결과가 불명확하면 호출자는 다시 로그인하지 말고 원장을 조회해야 합니다.
func (s *Store) FinishLogin(ctx context.Context, id int64, cookies *Cookies, code string) (retErr error) {
	sealed, err := s.encodeLoginResult(cookies, code)
	if err != nil {
		return err
	}

	tx, err := s.pool.Begin(ctx)
	if err != nil {
		return fmt.Errorf("begin X login result: %w", err)
	}

	defer func() {
		if rollbackErr := pgxutil.Rollback(ctx, tx); rollbackErr != nil && !errors.Is(rollbackErr, pgx.ErrTxClosed) {
			retErr = errors.Join(retErr, rollbackErr)
		}
	}()

	if finishErr := finishLoginTx(ctx, tx, id, sealed, code); finishErr != nil {
		return finishErr
	}

	if err = tx.Commit(ctx); err != nil {
		return fmt.Errorf("commit X login result unconfirmed: %w", err)
	}

	return nil
}

func finishLoginTx(ctx context.Context, tx pgx.Tx, id int64, sealed []byte, code string) error {
	var err error

	// ClaimLogin과 같은 세션→시도 순서로 잠가 교착을 방지합니다.
	var (
		current, active int64
		currentState    string
		pending         bool
	)

	if err = tx.QueryRow(ctx, mustSQL("login_session_lock.sql")).Scan(&current, &active, &currentState, &pending); err != nil {
		return fmt.Errorf("lock X login result session: %w", err)
	}

	var expected int64

	if err = tx.QueryRow(ctx, mustSQL("login_attempt_lock.sql"), id).Scan(&expected); errors.Is(err, pgx.ErrNoRows) {
		return nil
	} else if err != nil {
		return fmt.Errorf("lock X login result: %w", err)
	}

	state := "login_required"

	var submitted *int64

	if code == "outcome_unknown" {
		state = "outcome_unknown"
	}

	if sealed != nil {
		state, code = "manual_override", "manual_override"
	}

	if sealed != nil && current == expected && !pending {
		result, submitErr := tx.Exec(ctx, mustSQL("submit.sql"), sealed, expected)
		if submitErr != nil {
			return fmt.Errorf("submit recovered X session: %w", submitErr)
		}

		if result.RowsAffected() == 1 {
			state, code, submitted = "submitted", "", new(expected+1)
		}
	}

	if _, err = tx.Exec(ctx, mustSQL("login_finish.sql"), id, state, code, submitted); err != nil {
		return fmt.Errorf("finish X login: %w", err)
	}

	return nil
}

func (s *Store) encodeLoginResult(cookies *Cookies, code string) ([]byte, error) {
	if !validLoginError(code) || (cookies == nil) == (code == "") {
		return nil, errors.New("invalid X login result")
	}

	if cookies == nil {
		return nil, nil
	}

	return s.seal(*cookies)
}

func validLoginError(code string) bool {
	switch code {
	case "", "additional_authentication", "login_rejected", "browser_failed", "outcome_unknown":
		return true
	default:
		return false
	}
}
