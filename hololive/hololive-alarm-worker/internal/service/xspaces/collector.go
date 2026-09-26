package xspaces

import (
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"time"

	sessions "github.com/kapu/hololive-shared/pkg/service/xspaces"
)

const (
	invalidResponseCode = "invalid_response"
	collectorFailedCode = "collector_failed"
	// Go가 helper 결과 문서를 받지 못하고 종료 상태만 관측한 실패 단계다.
	helperOutputStage = "helper_output"
)

var (
	// 실패 위치로 helper가 보고할 수 있는 단계다. 예외 원문 대신 단계로 원인 범위를 좁힌다.
	helperStages = []string{"input", "library", "app_shell", "transaction", "collect"}
	// JavaScript 내장 오류 종류이며 collector_failed에만 붙는다. 그 밖의 이름은 helper가 other로 보낸다.
	helperErrorNames = []string{"Error", "TypeError", "SyntaxError", "RangeError", "ReferenceError", "AbortError", "TimeoutError", "other"}
	nodeErrorCode    = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
)

// Observation은 helper가 확인한 직접 개설 스페이스다. 방·채널 정보는 worker가 결정한다.
type Observation struct {
	SpaceID   string    `json:"space_id"`
	CreatorID string    `json:"creator_id"`
	Title     string    `json:"title"`
	StartedAt time.Time `json:"started_at"`
}

// CollectionError는 secret이 없는 고정 오류 코드, 재조회 간격, 실패 위치와 숫자 HTTP 진단만 전달한다.
// 진단 필드는 로그 전용이며 세션 상태에는 Code만 기록한다.
type CollectionError struct {
	Code       string
	Cooldown   time.Duration
	HTTPStatus int
	APICodes   []int
	// Stage는 helper가 실패한 단계이며, helper 출력을 해석하지 못하면 helper_output이다.
	Stage string
	// ErrorName·ErrorCode는 collector_failed의 내장 오류 종류와 Node 오류 코드다.
	ErrorName string
	ErrorCode string
	// Process는 helper_output에서 Go가 관측한 실행 결과다(예: exit status 1, signal: killed).
	Process string
}

// Error는 upstream·예외 원문 대신 검증된 진단만 포함한 로그 메시지를 반환한다.
func (e *CollectionError) Error() string {
	var diagnostics strings.Builder

	if e.Stage != "" {
		fmt.Fprintf(&diagnostics, "stage=%s ", e.Stage)
	}

	if e.ErrorName != "" {
		fmt.Fprintf(&diagnostics, "error_name=%s ", e.ErrorName)
	}

	if e.ErrorCode != "" {
		fmt.Fprintf(&diagnostics, "error_code=%s ", e.ErrorCode)
	}

	if e.Process != "" {
		fmt.Fprintf(&diagnostics, "process=%q ", e.Process)
	}

	return fmt.Sprintf("X spaces collection: %s (%shttp_status=%d api_codes=%v)", e.Code, diagnostics.String(), e.HTTPStatus, e.APICodes)
}

// ProcessCollector는 단발 Node helper의 stdin으로만 인증 값을 전달한다.
// 취소·timeout 시 프로세스를 종료하고 기다리며 stderr 원문을 노출하지 않는다.
type ProcessCollector struct{}

type boundedOutput struct{ bytes.Buffer }

type collectionResult struct {
	Spaces          *[]Observation `json:"spaces"`
	Error           string         `json:"error"`
	Stage           string         `json:"stage"`
	ErrorName       string         `json:"error_name"`
	ErrorCode       string         `json:"error_code"`
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
		return nil, helperOutputFailure(runErr)
	}

	if result.Error != "" {
		return nil, result.failure()
	}

	if runErr != nil || result.Spaces == nil || len(*result.Spaces) > 100 {
		return nil, helperOutputFailure(runErr)
	}

	return *result.Spaces, nil
}

// 결과 문서가 없거나 해석되지 않으면 Go가 관측한 실행 결과만 남긴다. 표준 오류 원문은 인증 정보를 포함할 수 있어 읽지 않는다.
// Go exec 오류 문자열은 종료 상태·실행 실패만 담고 stdin 내용을 담지 않으므로 그대로 남긴다.
func helperOutputFailure(runErr error) *CollectionError {
	process := "exit status 0"

	if runErr != nil {
		process = runErr.Error()
	}

	return &CollectionError{Code: collectorFailedCode, Stage: helperOutputStage, Process: process}
}

func (r collectionResult) failure() error {
	if !sessions.ValidErrorCode(r.Error) || r.Error == "authentication_pending" {
		return &CollectionError{Code: invalidResponseCode}
	}

	if !slices.Contains(helperStages, r.Stage) || !r.validErrorKind() {
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

	return &CollectionError{
		Code: r.Error, Cooldown: time.Duration(r.CooldownSeconds) * time.Second, HTTPStatus: r.HTTPStatus, APICodes: r.APICodes,
		Stage: r.Stage, ErrorName: r.ErrorName, ErrorCode: r.ErrorCode,
	}
}

// validErrorKind는 분류되지 않은 예외(collector_failed)에만 오류 종류를 허용한다.
func (r collectionResult) validErrorKind() bool {
	if r.Error != collectorFailedCode {
		return r.ErrorName == "" && r.ErrorCode == ""
	}

	return slices.Contains(helperErrorNames, r.ErrorName) && (r.ErrorCode == "" || nodeErrorCode.MatchString(r.ErrorCode))
}

func collectionFailure(err error) (string, time.Duration) {
	if failure, ok := errors.AsType[*CollectionError](err); ok && sessions.ValidErrorCode(failure.Code) {
		return failure.Code, failure.Cooldown
	}

	return collectorFailedCode, 0
}

// Collector는 모의 응답과 실제 helper가 공유하는 읽기 전용 수집 경계다.
type Collector interface {
	Collect(context.Context, sessions.Cookies, []string) ([]Observation, error)
}

var _ Collector = ProcessCollector{}
