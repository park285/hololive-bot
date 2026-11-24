package xspaces

import (
	"bufio"
	"bytes"
	"context"
	jsonv2 "encoding/json/v2"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"regexp"
	"slices"
	"strings"
	"sync"
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

const maxHelperOutput = 256 * 1024

var errHelperOutputLimit = errors.New("x collector output exceeds limit")

type ProcessCollector struct {
	mu      sync.Mutex
	helper  *helperProcess
	command func(context.Context) *exec.Cmd
}

type helperProcess struct {
	cmd     *exec.Cmd
	stdin   io.WriteCloser
	stdout  io.ReadCloser
	output  *bufio.Reader
	exited  chan struct{}
	state   *os.ProcessState
	waitErr error
}

type helperLine struct {
	line []byte
	err  error
}

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

func (p *ProcessCollector) Collect(ctx context.Context, cookies sessions.Cookies, userIDs []string) ([]Observation, error) {
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

	p.mu.Lock()
	defer p.mu.Unlock()

	if p.helper != nil && p.helper.hasExited() {
		_ = p.helper.stop() //nolint:errcheck // 유휴 중 끝난 helper의 종료 상태는 이번 요청의 결과가 아니며, 같은 호출에서 새 helper로 교체한다.

		p.helper = nil
	}

	if p.helper == nil {
		command := p.command
		if command == nil {
			command = nodeHelperCommand
		}

		helper, startErr := startHelper(command(context.WithoutCancel(ctx)))
		if startErr != nil {
			return nil, helperOutputFailure(startErr)
		}

		p.helper = helper
	}

	line, err := p.helper.exchange(ctx, raw)
	if err == nil {
		spaces, decodeErr := decodeCollectionOutput(line)
		if failure, ok := errors.AsType[*CollectionError](decodeErr); !ok || failure.Stage != helperOutputStage {
			return spaces, decodeErr
		}
	}

	exitErr := p.helper.stop()

	p.helper = nil

	if ctx.Err() != nil {
		return nil, &CollectionError{Code: "timeout"}
	}

	return nil, helperOutputFailure(exitErr)
}

func nodeHelperCommand(ctx context.Context) *exec.Cmd {
	return exec.CommandContext(ctx, "/usr/local/bin/node", "/app/xspaces/src/serve.mjs")
}

func startHelper(cmd *exec.Cmd) (*helperProcess, error) {
	cmd.Env = []string{"LANG=C.UTF-8"}
	cmd.Stderr = nil

	stdin, err := cmd.StdinPipe()
	if err != nil {
		return nil, fmt.Errorf("open X collector input: %w", err)
	}

	stdout, err := cmd.StdoutPipe()
	if err != nil {
		return nil, fmt.Errorf("open X collector output: %w", err)
	}

	if err := cmd.Start(); err != nil {
		return nil, fmt.Errorf("start X collector: %w", err)
	}

	helper := &helperProcess{cmd: cmd, stdin: stdin, stdout: stdout, output: bufio.NewReader(stdout), exited: make(chan struct{})}

	go func() {
		helper.state, helper.waitErr = cmd.Process.Wait()
		close(helper.exited)
	}()

	return helper, nil
}

func (h *helperProcess) hasExited() bool {
	select {
	case <-h.exited:
		return true
	default:
		return false
	}
}

func (h *helperProcess) exchange(ctx context.Context, request []byte) ([]byte, error) {
	if _, err := h.stdin.Write(request); err != nil {
		return nil, fmt.Errorf("send X collector request: %w", err)
	}

	if _, err := io.WriteString(h.stdin, "\n"); err != nil {
		return nil, fmt.Errorf("send X collector request: %w", err)
	}

	result := make(chan helperLine, 1)

	go func() {
		line, err := readHelperLine(h.output)
		result <- helperLine{line: line, err: err}
	}()

	select {
	case response := <-result:
		return response.line, response.err
	case <-ctx.Done():
		return nil, fmt.Errorf("await X collector response: %w", ctx.Err())
	}
}

func (h *helperProcess) stop() error {
	if err := h.cmd.Process.Kill(); err != nil && !errors.Is(err, os.ErrProcessDone) {
		return fmt.Errorf("stop X collector: %w", err)
	}

	<-h.exited

	closeErr := errors.Join(h.stdin.Close(), h.stdout.Close())

	switch {
	case h.waitErr != nil:
		return fmt.Errorf("wait X collector: %w", h.waitErr)
	case !h.state.Success():
		return fmt.Errorf("wait X collector: %w", &exec.ExitError{ProcessState: h.state})
	case closeErr != nil:
		return fmt.Errorf("close X collector pipes: %w", closeErr)
	default:
		return nil
	}
}

func readHelperLine(output *bufio.Reader) ([]byte, error) {
	var line []byte

	for {
		chunk, err := output.ReadSlice('\n')
		if len(line)+len(chunk) > maxHelperOutput {
			return nil, errHelperOutputLimit
		}

		line = append(line, chunk...)

		if err == nil {
			return bytes.TrimSuffix(line, []byte{'\n'}), nil
		}

		if !errors.Is(err, bufio.ErrBufferFull) {
			return nil, fmt.Errorf("read X collector response: %w", err)
		}
	}
}

func decodeCollectionOutput(output []byte) ([]Observation, error) {
	var result collectionResult

	if err := jsonv2.Unmarshal(output, &result, jsonv2.RejectUnknownMembers(true)); err != nil {
		return nil, helperOutputFailure(nil)
	}

	if result.Error != "" {
		return nil, result.failure()
	}

	if result.Spaces == nil || len(*result.Spaces) > 100 {
		return nil, helperOutputFailure(nil)
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

var _ Collector = (*ProcessCollector)(nil)
