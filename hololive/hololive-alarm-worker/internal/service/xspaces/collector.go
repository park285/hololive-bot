package xspaces

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"time"

	sessions "github.com/kapu/hololive-shared/pkg/service/xspaces"
)

const invalidResponseCode = "invalid_response"

// Observation은 helper가 확인한 직접 개설 스페이스다. 방·채널 정보는 worker가 결정한다.
type Observation struct {
	SpaceID   string    `json:"space_id"`
	CreatorID string    `json:"creator_id"`
	Title     string    `json:"title"`
	StartedAt time.Time `json:"started_at"`
}

// CollectionError는 secret이 없는 고정 오류 코드, 재조회 간격과 숫자 HTTP 진단만 전달한다.
type CollectionError struct {
	Code       string
	Cooldown   time.Duration
	HTTPStatus int
	APICodes   []int
}

// Error는 upstream 원문 대신 검증된 숫자 진단만 포함한 로그 메시지를 반환한다.
func (e *CollectionError) Error() string {
	return fmt.Sprintf("X spaces collection: %s (http_status=%d api_codes=%v)", e.Code, e.HTTPStatus, e.APICodes)
}

// ProcessCollector는 단발 Node helper의 stdin으로만 인증 값을 전달한다.
// 취소·timeout 시 프로세스를 종료하고 기다리며 stderr 원문을 노출하지 않는다.
type ProcessCollector struct{}

type boundedOutput struct{ bytes.Buffer }

type collectionResult struct {
	Spaces          *[]Observation `json:"spaces"`
	Error           string         `json:"error"`
	CooldownSeconds int            `json:"cooldown_seconds"`
	HTTPStatus      int            `json:"http_status"`
	APICodes        []int          `json:"api_codes"`
}

func (b *boundedOutput) Write(p []byte) (int, error) {
	if b.Len()+len(p) > 256*1024 {
		return 0, errors.New("x collector output exceeds limit")
	}

	n, err := b.Buffer.Write(p)
	if err != nil {
		return n, fmt.Errorf("buffer collector output: %w", err)
	}

	return n, nil
}

// Collect는 최대 90초 동안 조회한다. 실패한 응답을 빈 결과로 해석하지 않는다.
func (p ProcessCollector) Collect(ctx context.Context, cookies sessions.Cookies, userIDs []string) ([]Observation, error) {
	if err := cookies.Validate(); err != nil {
		return nil, &CollectionError{Code: "invalid_cookies"}
	}

	raw, err := jsonv2.Marshal(struct {
		UserIDs []string         `json:"user_ids"`
		Cookies sessions.Cookies `json:"cookies"`
	}{userIDs, cookies})
	if err != nil {
		return nil, errors.New("encode X collector input failed")
	}
	defer clear(raw)

	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)

	defer cancel()

	command := exec.CommandContext(ctx, "/usr/local/bin/node", "/app/xspaces/src/collect.mjs")

	command.Env = []string{"LANG=C.UTF-8"}
	command.Stdin = bytes.NewReader(raw)
	command.Stderr = io.Discard

	output := &boundedOutput{}

	command.Stdout = output
	command.WaitDelay = time.Second

	runErr := command.Run()

	if ctx.Err() != nil {
		return nil, &CollectionError{Code: "timeout"}
	}

	return decodeCollectionOutput(output.Bytes(), runErr)
}

func decodeCollectionOutput(output []byte, runErr error) ([]Observation, error) {
	var result collectionResult

	if err := jsonv2.Unmarshal(output, &result, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, &CollectionError{Code: "collector_failed"}
	}

	if result.Error != "" {
		return nil, result.failure()
	}

	if runErr != nil || result.Spaces == nil || len(*result.Spaces) > 100 {
		return nil, &CollectionError{Code: "collector_failed"}
	}

	return *result.Spaces, nil
}

func (r collectionResult) failure() error {
	if !sessions.ValidErrorCode(r.Error) || r.Error == "authentication_pending" {
		return &CollectionError{Code: invalidResponseCode}
	}

	if r.CooldownSeconds < 0 || r.CooldownSeconds > 86400 {
		return &CollectionError{Code: invalidResponseCode}
	}

	if r.HTTPStatus != 0 && (r.HTTPStatus < 100 || r.HTTPStatus > 599) {
		return &CollectionError{Code: invalidResponseCode}
	}

	if len(r.APICodes) > 8 {
		return &CollectionError{Code: invalidResponseCode}
	}

	for _, code := range r.APICodes {
		if code < 0 || code > 65535 {
			return &CollectionError{Code: invalidResponseCode}
		}
	}

	return &CollectionError{Code: r.Error, Cooldown: time.Duration(r.CooldownSeconds) * time.Second, HTTPStatus: r.HTTPStatus, APICodes: r.APICodes}
}

func collectionFailure(err error) (string, time.Duration) {
	if failure, ok := errors.AsType[*CollectionError](err); ok && sessions.ValidErrorCode(failure.Code) {
		return failure.Code, failure.Cooldown
	}

	return "collector_failed", 0
}

// Collector는 모의 응답과 실제 helper가 공유하는 읽기 전용 수집 경계다.
type Collector interface {
	Collect(context.Context, sessions.Cookies, []string) ([]Observation, error)
}

var _ Collector = ProcessCollector{}
