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
	helperOutputStage   = "helper_output"
)

var (
	helperStages     = []string{"input", "library", "app_shell", "transaction", "collect"}
	helperErrorNames = []string{"Error", "TypeError", "SyntaxError", "RangeError", "ReferenceError", "AbortError", "TimeoutError", "other"}
	nodeErrorCode    = regexp.MustCompile(`^[A-Z][A-Z0-9_]{0,63}$`)
)

type Observation struct {
	SpaceID   string    `json:"space_id"`
	CreatorID string    `json:"creator_id"`
	Title     string    `json:"title"`
	StartedAt time.Time `json:"started_at"`
}

type CollectionError struct {
	Code       string
	Cooldown   time.Duration
	HTTPStatus int
	APICodes   []int
	Stage      string
	ErrorName  string
	ErrorCode  string
	Process    string
}

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

type Collector interface {
	Collect(context.Context, sessions.Cookies, []string) ([]Observation, error)
}

var _ Collector = ProcessCollector{}
