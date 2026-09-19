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

// Observation은 helper가 확인한 직접 개설 스페이스다. 방·채널 정보는 worker가 결정한다.
type Observation struct {
	SpaceID   string    `json:"space_id"`
	CreatorID string    `json:"creator_id"`
	Title     string    `json:"title"`
	StartedAt time.Time `json:"started_at"`
}

// CollectionError는 secret이 없는 고정 오류 코드와 재조회 가능 시각만 전달한다.
type CollectionError struct {
	Code     string
	Cooldown time.Duration
}

func (e *CollectionError) Error() string { return "X spaces collection: " + e.Code }

// ProcessCollector는 단발 Node helper의 stdin으로만 인증 값을 전달한다.
// 취소·timeout 시 프로세스를 종료하고 기다리며 stderr 원문을 노출하지 않는다.
type ProcessCollector struct{}

type boundedOutput struct{ bytes.Buffer }

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

	var result struct {
		Spaces          *[]Observation `json:"spaces"`
		Error           string         `json:"error"`
		CooldownSeconds int            `json:"cooldown_seconds"`
	}

	if err := jsonv2.Unmarshal(output.Bytes(), &result, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, &CollectionError{Code: "collector_failed"}
	}

	if result.Error != "" {
		if !sessions.ValidErrorCode(result.Error) || result.CooldownSeconds < 0 || result.CooldownSeconds > 86400 {
			return nil, &CollectionError{Code: "invalid_response"}
		}

		return nil, &CollectionError{Code: result.Error, Cooldown: time.Duration(result.CooldownSeconds) * time.Second}
	}

	if runErr != nil || result.Spaces == nil || len(*result.Spaces) > 100 {
		return nil, &CollectionError{Code: "collector_failed"}
	}

	return *result.Spaces, nil
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
