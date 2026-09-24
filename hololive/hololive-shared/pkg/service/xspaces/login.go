package xspaces

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
)

// LoginStatus는 과거 자동 로그인 시도의 읽기 전용 상태입니다. 비밀번호와 쿠키는 포함하지 않습니다.
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
